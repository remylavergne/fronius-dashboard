// Package storage assure la persistance SQLite de la supervision.
//
// Une seule connexion est ouverte en écriture (volume : un échantillon toutes
// les 5 s) ; le mode WAL reste utile pour permettre une lecture concurrente
// depuis un outil externe (sqlite3) sans bloquer le collecteur.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pilote pur Go : CGO_ENABLED=0 reste possible
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// tsLayout est le format de stockage des horodatages : UTC, milliseconde,
// suffixe Z. Toute écriture passe par formatTS, toute lecture par parseTS.
const tsLayout = "2006-01-02T15:04:05.000Z"

// Store est le point d'accès à la base.
type Store struct {
	db  *sql.DB
	loc *time.Location
}

// Open ouvre (ou crée) la base, applique les migrations manquantes et retourne
// le magasin. loc est le fuseau de l'installation, utilisé pour les
// regroupements par jour local.
func Open(path string, loc *time.Location) (*Store, error) {
	if loc == nil {
		loc = time.UTC
	}
	if path == "" {
		return nil, fmt.Errorf("chemin de base vide")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("création du répertoire %s: %w", dir, err)
		}
	}

	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("ouverture de %s: %w", path, err)
	}
	// Un seul écrivain : évite SQLITE_BUSY et garde un ordre d'écriture simple.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, loc: loc}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close ferme la base.
func (s *Store) Close() error { return s.db.Close() }

// Location retourne le fuseau de l'installation.
func (s *Store) Location() *time.Location { return s.loc }

// DB expose la connexion sous-jacente (tests et outils de diagnostic).
func (s *Store) DB() *sql.DB { return s.db }

// migrate applique les migrations non encore enregistrées.
func (s *Store) migrate(ctx context.Context) error {
	const createVersions = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := s.db.ExecContext(ctx, createVersions); err != nil {
		return fmt.Errorf("création de schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("lecture de la version de schéma: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("lecture des migrations embarquées: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // l'ordre lexicographique suit le préfixe numérique

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("lecture de %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, version, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int, name, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op après Commit

	for i, stmt := range splitStatements(body) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %s, instruction %d: %w", name, i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, formatTS(time.Now())); err != nil {
		return fmt.Errorf("enregistrement de la migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("validation de la migration %s: %w", name, err)
	}
	return nil
}

// splitStatements découpe un fichier de migration en instructions.
//
// Les commentaires sont retirés AVANT le découpage : un point-virgule présent
// dans un commentaire (par exemple « (W, Wh, %) ; aucune conversion »)
// découperait sinon la ligne de commentaire en deux et laisserait un fragment
// de commentaire non préfixé au milieu d'une instruction.
//
// Les fichiers de migration sont écrits et versionnés dans ce dépôt : ils ne
// contiennent ni déclencheur ni littéral comportant un point-virgule, donc ce
// découpage est sûr ici. Il évite de dépendre du traitement multi-instructions
// du pilote.
func splitStatements(body string) []string {
	stripped := stripSQLComments(body)
	var out []string
	for _, raw := range strings.Split(stripped, ";") {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func migrationVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, ".sql")
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("nom de migration invalide %q : préfixe numérique et « _ » attendus", name)
	}
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("nom de migration invalide %q : %w", name, err)
	}
	return v, nil
}

// formatTS sérialise un horodatage en UTC, à la milliseconde.
func formatTS(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format(tsLayout)
}

// parseTS lit un horodatage écrit par formatTS.
func parseTS(s string) (time.Time, error) {
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("horodatage invalide %q: %w", s, err)
	}
	return t.UTC(), nil
}
