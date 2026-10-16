# ADR 0007 — Packaging

## Statut

Accepté (phase 1)

## Contexte

La phase 1 doit être vérifiable sur un poste de développement : collecter depuis le simulateur,
historiser, servir l'API REST et afficher le dashboard React. Le frontend est un projet Vite avec
rechargement à chaud ; le backend est un binaire Go.

La cible finale est une installation sur NAS, mais l'unité d'installation et le mode de déploiement ne
sont pas encore arrêtés.

## Décision

En phase 1, trois binaires autonomes et un serveur de développement Vite :

- `froniusd` : collecteur + API REST + service statique optionnel de `FRONIUS_WEB_DIR` ;
- `fronius-sim` : simulateur ;
- `fronius-probe` : sonde lecture seule.

Ils sont compilés avec `CGO_ENABLED=0`, sans `go:embed` des assets React. Le conditionnement conteneur
(Dockerfile multi-stage, compose, healthcheck, documentation d'installation NAS) et l'intégration des
assets par `go:embed` sont reportés en phase 5.

## Conséquences

- Aucun `go:embed` ne doit être introduit avant la phase 5.
- Le service statique se contente de servir `FRONIUS_WEB_DIR` avec repli SPA sur `index.html` ; si la
  variable est vide, aucun statique n'est servi.
- `CGO_ENABLED=0` prouve déjà l'absence de dépendance native (vérifié en CI).
- L'exploitation sur NAS dépend de la phase 5 ; aucune unité systemd ni image n'est produite en phase 1.
- Le cycle de build du frontend reste indépendant de celui du backend.

## Alternatives écartées

- **Binaire unique avec `go:embed` dès la phase 1** : fige le cycle de build du frontend dans celui du
  backend, sans besoin identifié.
- **Docker dès la phase 1** : ajoute une dépendance à Docker pour une application locale déjà exécutable
  en binaire.
- **Service systemd natif en phase 1** : l'unité d'installation n'est pas encore connue.

## Sources

- `Makefile` (`build`, `dev-sim`, `dev-api`, `dev-web`)
- `cmd/froniusd/main.go` (`FRONIUS_WEB_DIR`, repli SPA)
- `.github/workflows/ci.yml` (`CGO_ENABLED=0 go build ./...`)
- Roadmap phase 5 (packaging, `go:embed`, Dockerfile multi-stage, compose, E2E)
