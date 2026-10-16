# Fronius GEN24 — dossier de transfert à un agent IA

**Objectif :** remplacer l'interface de consultation Angular du fabricant par une application locale **Go + React/TypeScript + shadcn/ui**, avec dashboard, graphiques, historique et, ultérieurement, contrôles explicitement autorisés.

## Ordre de lecture
1. `01-CONTEXTE-ET-ETAT.md` — matériel, faits observés, inconnues.
2. `02-REVERSE-ENGINEERING.md` — modules Webpack et mécanismes.
3. `03-API-INVENTAIRE.md` — endpoints repérés dans le bundle (inventaire statique, pas preuve d'accessibilité).
4. `04-ARCHITECTURE-CIBLE.md` — proposition technique Go/React.
5. `05-SPEC-PRODUIT-UI.md` — écrans, visualisations, UX.
6. `06-SECURITE-ET-TESTS.md` — règles impératives.
7. `TODO.md` — backlog exhaustif et ordonné.
8. `AGENTS.md` — consignes directement utilisables par un agent de développement.

## Sources locales
- `Pasted text(20261016-090329).txt` : bundle JavaScript Webpack fourni par l'utilisateur (environ 184 000 lignes) ; **non recopié** dans ce dossier.
- `fronius-information.pdf` : export système du 16 octobre 2026 ; **non recopié**.
- Observations HTTP et discussion utilisateur : certaines valeurs sont rapportées et non reproductibles à partir du JS seul.

**Confiance :** distinguer [SOURCE-JS], [SOURCE-PDF], [OBSERVÉ-CONVERSATION], [HYPOTHÈSE], [À-TESTER]. Ne jamais transformer une route trouvée dans le JS en permission serveur confirmée.
