// Command fronius-backup saves, verifies and restores the database of froniusd.
//
// It is the tool for the one operation the running application never performs:
// replacing the whole history. Three subcommands:
//
//	fronius-backup save    -out <file> [-db <path>]
//	fronius-backup verify  -file <file>
//	fronius-backup restore -from <file> -yes [-db <path>]
//
// The database path and the timezone default to the application's configuration
// (FRONIUS_DB_PATH, FRONIUS_TZ), so an operator who runs froniusd with an
// environment does not repeat it here.
//
// A save may run while the collector runs: the snapshot is taken through the
// application's own connection (VACUUM INTO), so it is consistent and never half
// written. A restore must not: stop froniusd first (docs/RUNBOOK.md). It refuses
// to run without -yes, and it first keeps the current database aside under a
// name derived from the database path, so a restore made for the wrong reason
// stays reversible.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/storage"
)

// options is one invocation of the tool.
type options struct {
	command string
	dbPath  string
	tz      string
	out     string
	from    string
	yes     bool
	stdout  io.Writer
}

func main() {
	if code := main1(os.Stdout, os.Stderr, os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}

// main1 wires the process's streams, parses the command line and runs it.
// Returning the exit code keeps the tests free of os.Exit.
func main1(stdout, stderr io.Writer, args []string) int {
	opt, err := parse(stdout, args)
	if err != nil {
		fmt.Fprintln(stderr, "fronius-backup:", err)
		return 2
	}
	if err := run(opt); err != nil {
		fmt.Fprintln(stderr, "fronius-backup:", err)
		return 1
	}
	return 0
}

// parse reads the subcommand and its flags. The database path and the timezone
// come from the application's configuration unless they are given.
func parse(stdout io.Writer, args []string) (options, error) {
	cfg, err := config.Load()
	if err != nil {
		return options{}, err
	}
	opt := options{tz: cfg.TZ, dbPath: cfg.DBPath, stdout: stdout}

	if len(args) == 0 {
		return options{}, errors.New(usage())
	}
	opt.command = args[0]
	rest := args[1:]

	dbPathFlag := func(fs *flag.FlagSet) {
		fs.StringVar(&opt.dbPath, "db", opt.dbPath, "path of the SQLite database")
		fs.StringVar(&opt.tz, "tz", opt.tz, "installation timezone (used to read the database)")
	}
	fs := flag.NewFlagSet(opt.command, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the caller reports the error, not the flag package

	switch opt.command {
	case "save":
		dbPathFlag(fs)
		fs.StringVar(&opt.out, "out", "", "file to write the backup to (required)")
	case "verify":
		fs.StringVar(&opt.out, "file", "", "backup file to verify (required)")
	case "restore":
		dbPathFlag(fs)
		fs.StringVar(&opt.from, "from", "", "backup file to restore from (required)")
		fs.BoolVar(&opt.yes, "yes", false, "confirms that the whole database is replaced")
	default:
		return options{}, fmt.Errorf("unknown command %q\n%s", opt.command, usage())
	}
	if err := fs.Parse(rest); err != nil {
		return options{}, fmt.Errorf("%s: %w", opt.command, err)
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("%s: unexpected argument %q", opt.command, fs.Arg(0))
	}
	if opt.command == "verify" {
		if opt.out == "" {
			return options{}, errors.New("verify: -file is required")
		}
	}
	if opt.command == "save" && opt.out == "" {
		return options{}, errors.New("save: -out is required")
	}
	if opt.command == "restore" && opt.from == "" {
		return options{}, errors.New("restore: -from is required")
	}
	return opt, nil
}

func usage() string {
	return `usage:
  fronius-backup save    -out <file> [-db <path>] [-tz <zone>]
  fronius-backup verify  -file <file>
  fronius-backup restore -from <file> -yes [-db <path>] [-tz <zone>]`
}

// run performs the requested operation.
func run(opt options) error {
	loc, err := time.LoadLocation(opt.tz)
	if err != nil {
		return fmt.Errorf("loading timezone %q: %w", opt.tz, err)
	}
	ctx := context.Background()

	switch opt.command {
	case "save":
		return runSave(ctx, opt, loc)
	case "verify":
		info, err := storage.Inspect(ctx, opt.out)
		if err != nil {
			return err
		}
		fmt.Fprintln(opt.stdout, describe(info))
		return nil
	case "restore":
		return runRestore(ctx, opt, loc)
	default:
		return fmt.Errorf("unknown command %q", opt.command)
	}
}

// runSave snapshots the database and reports what the snapshot holds.
func runSave(ctx context.Context, opt options, loc *time.Location) error {
	s, err := storage.Open(opt.dbPath, loc)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	if err := s.Backup(ctx, opt.out); err != nil {
		return err
	}
	// Read the result back: a backup nobody checked is a hope, not a copy.
	info, err := storage.Inspect(ctx, opt.out)
	if err != nil {
		return fmt.Errorf("checking the backup just written: %w", err)
	}
	fmt.Fprintln(opt.stdout, describe(info))
	return nil
}

// runRestore replaces the whole database, keeping the previous one aside.
func runRestore(ctx context.Context, opt options, loc *time.Location) error {
	if !opt.yes {
		return fmt.Errorf("restore replaces every row of %s: pass -yes to confirm", opt.dbPath)
	}

	// Keep the current database before touching it: a restore done for the
	// wrong reason (a backup of another installation, an older day) must stay
	// reversible. The name is derived from the database path and carries the
	// instant, so consecutive restores do not overwrite each other.
	var safety string
	if _, err := os.Stat(opt.dbPath); err == nil {
		safety = fmt.Sprintf("%s.pre-restore-%s.db", opt.dbPath, time.Now().UTC().Format("20060102T150405Z"))
		s, err := storage.Open(opt.dbPath, loc)
		if err != nil {
			return err
		}
		err = s.Backup(ctx, safety)
		_ = s.Close()
		if err != nil {
			return fmt.Errorf("keeping the current database aside: %w", err)
		}
		fmt.Fprintf(opt.stdout, "current database kept as %s\n", safety)
	}

	s, err := storage.Open(opt.dbPath, loc)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	if err := s.Restore(ctx, opt.from); err != nil {
		return err
	}
	info, err := storage.Inspect(ctx, opt.dbPath)
	if err != nil {
		return fmt.Errorf("checking the restored database: %w", err)
	}
	fmt.Fprintf(opt.stdout, "restored from %s\n", opt.from)
	fmt.Fprintln(opt.stdout, describe(info))
	return nil
}

// describe is the one-line summary an operator reads: what the file holds and
// the period it covers.
func describe(info storage.BackupInfo) string {
	period := "no sample"
	if info.HasFirst && info.HasLast {
		period = fmt.Sprintf("%s → %s", info.FirstSample.UTC().Format(time.RFC3339), info.LastSample.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("%s: %d bytes, %d migrations, %d samples (%s), %d events, %d aggregated hours, written %s",
		info.Path, info.Size, info.Migrations, info.Samples, period, info.Events, info.Hours,
		info.WrittenUTC.Format(time.RFC3339))
}
