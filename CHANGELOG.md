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
  accumulant toutes les erreurs, et garde `CheckListenAddress` qui refuse
  une écoute non locale sans `FRONIUS_ALLOW_REMOTE=1`.
- 2026-10-16 — Erreurs typées (`internal/fronius/froniuserr`) : natures
  `not_found`, `unauthorized`, `server`, `decode`, `timeout`, `transport`,
  `device_status`, `readonly_violation` ; `errors.Is`/`errors.As` et `KindOf`.
  Garde lecture seule (`internal/fronius/readonly.go`) : `ReadOnly` refuse
  toute méthode autre que GET avant d'atteindre le transport.
- 2026-10-16 — Client Solar API (`internal/fronius/solarapi`) : DTO explicites de
  `GetPowerFlowRealtimeData.fcgi` (forme [HYPOTHÈSE], décodage tolérant), borne
  de 1 MiB sur le corps, cartographie complète des erreurs (404, 401/403,
  429/5xx, JSON invalide ou tronqué, `Body.Data` absent, `Head.Status.Code != 0`,
  délai dépassé, serveur injoignable).
- 2026-10-16 — Modèle normalisé (`internal/model`) : `Sample` (unités SI de base
  W/Wh/%, horodatage UTC, `Quality` measured/estimated/missing), conversion
  `FromPowerFlow`, capacités avec prérequis, preuves et motif d'indisponibilité,
  événements locaux typés. Invariant testé : un champ absent reste `nil`, un `0`
  explicite de l'appareil reste `0`.
- 2026-10-16 — Documentation de référence : `docs/SOURCES.md` (registre de
  provenance, vérifications exécutées sur le bundle, schéma Solar API supposé),
  `docs/ENDPOINTS.md` (classification READ/MUTATION/DANGEROUS des 79 chemins
  inventoriés + liste blanche phase 1) et les ADR 0001 à 0007.
