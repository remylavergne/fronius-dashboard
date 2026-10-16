# Changelog

Toutes les modifications notables de ce projet sont documentées dans ce fichier.

Le format suit [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et le
projet adhère au [versionnage sémantique](https://semver.org/lang/fr/).

## [Unreleased]

### Added

- 2026-10-16 — Socle du dépôt : `.gitignore`, licence MIT, `CHANGELOG.md`,
  `.env.example`, `Makefile`, arborescence `docs/` et correction du chemin
  `references/` dans `AGENTS.md`.
- 2026-10-16 — Simulateur Fronius (`cmd/fronius-sim`) : module Go
  `fronius-supervisor`, `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`
  déterministe (seed, courbe solaire, nébulosité lissée), extension `?at=`,
  injection de pannes (`--fault` / `X-Sim-Fault`), `GET /sim/health`, logs
  `log/slog`. Tests unitaires et HTTP déterministes.
- 2026-10-16 — Configuration (`internal/config`) : lecture exclusive par
  variables d'environnement `FRONIUS_*`, validation exhaustive (durées, entier,
  booléen, fuseau, URL, adresse d'écoute, niveau et format de log) avec
  accumulation de toutes les erreurs, et garde `CheckListenAddress` qui refuse
  une écoute non locale sans `FRONIUS_ALLOW_REMOTE=1`.
