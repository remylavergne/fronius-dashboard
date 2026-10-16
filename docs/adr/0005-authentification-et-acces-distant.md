# ADR 0005 — Authentification et accès distant

## Statut

Accepté (phase 1)

## Contexte

L'API REST de la phase 1 n'embarque aucun mécanisme d'authentification. Exposer une API sans
authentification sur un réseau local revient à publier les courbes de production et de consommation du
foyer. Le projet se veut local par défaut.

Le client officiel stocke par ailleurs les identifiants en clair dans le `localStorage` (clé `AuthHash`,
module `CNEH`) — pratique qui n'est jamais reproduite côté application.

## Décision

En phase 1, l'API REST n'a aucune authentification et écoute donc uniquement sur la boucle locale
(défaut `127.0.0.1:8080`). `froniusd` refuse au démarrage toute adresse non locale (`127.0.0.1`, `::1`,
`localhost`) tant que `FRONIUS_ALLOW_REMOTE=1` n'est pas défini explicitement, avec un message d'erreur
renvoyant au présent ADR :

```
refus: {addr} n'est pas une adresse locale ; définir FRONIUS_ALLOW_REMOTE=1 après avoir lu docs/adr/0005
```

## Conséquences

- L'accès à distance n'est possible qu'après lecture de cet ADR et un choix conscient.
- Aucune donnée d'authentification Fronius ne transite vers le frontend.
- Les identifiants éventuels de l'API interne (phase 2) resteront dans l'environnement du backend, jamais
  dans le bundle React ni dans le dépôt.
- Le besoin d'une authentification applicative (jeton ou mot de passe, éventuellement proxifié en HTTPS)
  devra être tranché par un ADR ultérieur avant tout accès distant permanent.
- La garde loopback est testée (`internal/config`, tests de `froniusd`).

## Alternatives écartées

- **Écoute sur `0.0.0.0` avec avertissement documentaire** : l'avertissement n'empêche pas l'exposition.
- **Authentification par mot de passe simple dès la phase 1** : ajoute un secret à gérer avant que la
  surface d'exposition n'existe.
- **Réutiliser l'authentification Digest de l'onduleur** : couple l'accès à l'UI à un compte matériel et
  ne protège pas l'API elle-même.

## Sources

- `internal/config/config.go` (garde loopback, `FRONIUS_ALLOW_REMOTE`)
- `cmd/froniusd/main.go`
- `docs/SOURCES.md` (module `CNEH`, identifiants en clair dans le `localStorage`)
- `06-SECURITE-ET-TESTS.md`
- Plan phase 1, étapes 3 et 9
