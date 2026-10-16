# Consignes à l'agent IA de développement

Tu développes une application locale de supervision d'un Fronius Primo GEN24 3.6 Plus, backend Go, frontend React/TypeScript/shadcn/ui. Lis **tous** les fichiers de ce dossier avant de coder, en commençant par `README.md` et `TODO.md`.

## Workflow

1. Établis le plan de livraison et les ADR manquants ; propose les hypothèses plutôt que de les inventer.
2. Implémente **d'abord** un simulateur Fronius à partir de fixtures anonymisées ; aucun matériel requis pour les tests.
3. Implémente un client Solar API lecture seule et le modèle normalisé ; ajoute l'auth Digest interne seulement si une métrique le justifie.
4. Implémente la persistance, l'API Go, puis le frontend React et les graphiques.
5. Écris tests et documentation à chaque étape ; maintiens `TODO.md` et un journal `CHANGELOG.md`.
6. Avant toute interaction avec l'onduleur réel, explique précisément les requêtes prévues et demande confirmation. **GET uniquement** par défaut.

## Exigences

- Pas de dépendance à l'Angular du fabricant dans l'application produite.
- Ne jamais affirmer que des permissions frontend sont des permissions firmware.
- Ne jamais exposer secrets à React ni les inscrire dans le dépôt.
- Ne pas calculer consommation/import/export sans Smart Meter ou autre mesure validée.
- Les commandes modifiant l'onduleur sont hors périmètre MVP.
- Code idiomatique Go, TS strict, architecture simple et testable ; lint, format, tests, CI.
- Tout endpoint non testé est explicitement `unverified`.
- Toute nouvelle information doit être documentée avec sa source et sa version de firmware.

## Definition of Done

Fonctionnalité testée (unitaires + intégration), typée, documentée, erreurs et états vides gérés, accessibilité vérifiée, secrets absents des logs, compatible données hors ligne.

## Sources de référence

Le fichier `references/fronius-original.js` contient
le JavaScript original de l'interface Fronius GEN24.

Il constitue la source primaire pour :

- L'identification des endpoints HTTP
- L'analyse des mécanismes d'authentification
- La compréhension des permissions
- L'identification des structures de données
- La reconstitution des fonctionnalités existantes

L'agent doit consulter ce fichier avant d'implémenter
une fonctionnalité basée sur l'API propriétaire Fronius.

Ne jamais modifier ce fichier original.
