# ADR 0003 — Solar API vs API interne

## Statut

Accepté (phase 1)

## Contexte

L'application doit récupérer les mesures de production d'un Fronius Primo GEN24 3.6 Plus. Deux familles
d'interfaces sont identifiées :

- la **Solar API v1**, une interface de lecture documentée (`GET /solar_api/v1/...`) ;
- l'**API interne** utilisée par l'interface Angular du fabricant (`/status/*`, `/config/*`), protégée
  par un Digest MD5 avec un en-tête de défi non standard `x-www-authenticate` et un retry unique sur
  401 (module `xeXE` / `JWhm` du bundle, voir `docs/SOURCES.md`).

Une exigence du projet interdit toute dépendance à l'interface du fabricant. L'accès à la Solar API
sans authentification sur le firmware 1.19.7-1 est une [HYPOTHÈSE] à confirmer par la sonde
(`fronius-probe`) — statut **[À-TESTER]**.

L'acquisition doit être strictement en lecture seule (voir ADR 0004).

## Décision

La Solar API v1 en lecture seule est la source unique de la phase 1 :
`GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`, sans paramètre.

L'adaptateur d'API interne (sessions Digest, `/status/*`, `/config/*`) est reporté en phase 2 et
n'existe pas dans le code de la phase 1. Le schéma JSON de `GetPowerFlowRealtimeData` reste une
[HYPOTHÈSE] jusqu'à capture réelle par la sonde.

## Conséquences

- En phase 1, `GET /api/v1/system` renvoie `device: null` : aucune identité onduleur issue du matériel.
- La capacité `device.identity` est indisponible, avec le motif « Adaptateur API interne non implémenté
  (phase 2) ».
- Si la sonde révèle que la Solar API exige une authentification, la phase 2 (adaptateur Digest) est
  remontée en priorité ; les identifiants restent alors confinés à l'environnement du backend, jamais
  dans le frontend (voir ADR 0005).
- La phase 1 reste pleinement fonctionnelle sur le simulateur `fronius-sim` sans matériel.

## Alternatives écartées

- **Modbus TCP** : nécessite d'activer une fonction sur l'onduleur, donc une modification de
  configuration, interdite en phase 1.
- **API interne dès la phase 1** : impose des secrets et ouvre la voie à des endpoints d'écriture ;
  hors périmètre.
- **Lecture du bundle Angular** : interdit — aucune dépendance à l'interface du fabricant.

## Sources

- `docs/SOURCES.md` (modules `xeXE`, `JWhm`, provenance, niveau de confiance)
- `docs/UNKNOWNS.md` (schéma Solar API, authentification Solar API, identité onduleur)
- `03-API-INVENTAIRE.md` (inventaire des endpoints ; la Solar API est absente du bundle)
- `internal/fronius/solarapi/dto.go` (forme supposée, [HYPOTHÈSE])
- `cmd/fronius-probe`, `docs/PROBE-READONLY.md` (protocole de capture)
- Plan phase 1, étapes 5, 11
