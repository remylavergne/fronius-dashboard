# ADR 0004 — Séparation supervision / commandes

## Statut

Accepté (phase 1)

## Contexte

L'application interroge un onduleur réel. Une lecture erronée produit une donnée fausse et corrigeable ;
une écriture erronée peut modifier des réglages matériels ou réseau électrique, avec un coût difficilement
réversible. Aucune permission firmware d'écriture n'a été établie : une chaîne trouvée dans le bundle JS
n'est pas une autorisation accordée par le serveur.

La phase 1 vise la supervision (collecte, historisation, affichage) et n'a pas besoin d'écrire.

## Décision

La phase 1 est strictement en lecture seule, garantie par construction et non par convention :

- tous les clients HTTP sont construits derrière `internal/fronius/readonly.go` : le type `ReadOnly`
  refuse toute méthode autre que `GET` avant d'atteindre le transport, en retournant une erreur de type
  `KindReadOnly` ;
- aucun chemin de code n'utilise POST/PUT/PATCH/DELETE (vérifié par grep, étape 12 du plan) ;
- la sonde `fronius-probe` n'émet que des `GET` vers un endpoint unique en liste blanche et exige
  `--allow-non-loopback` pour toute adresse non locale.

Le contrôle de l'onduleur est hors MVP (phase 6, non planifiée). Il exigera une revue de sécurité dédiée,
une liste blanche de commandes nommées, un mode dry-run et un journal d'audit.

## Conséquences

- Les retries ne portent que sur des requêtes `GET` : aucun retry ne peut modifier l'état du matériel.
- Toute nouvelle fonctionnalité doit être classée READ / MUTATION / DANGEROUS dans un ADR avant d'être
  implémentée.
- Aucune commande POST/PUT/PATCH/DELETE ne peut être émise vers l'onduleur tant qu'une revue de sécurité
  dédiée n'a pas autorisé un endpoint nommé.
- La surface d'attaque est limitée à un seul verbe HTTP côté matériel.

## Alternatives écartées

- **Lecture et écriture dès la phase 1** : risque matériel sans preuve d'autorisation firmware.
- **« Dry-run par défaut » avec écriture possible derrière un flag** : le flag devient un contournement,
  et un dry-run ne prouve pas l'absence d'effet de bord côté matériel.
- **Commandes limitées à la lecture d'état immédiat** : aucun gain par rapport aux `GET` déjà autorisés.

## Sources

- `internal/fronius/readonly.go`, `internal/fronius/froniuserr/errors.go`
- `docs/PROBE-READONLY.md`, `cmd/fronius-probe`
- `docs/ARCHITECTURE.md` (invariant lecture seule)
- `06-SECURITE-ET-TESTS.md`
- Plan phase 1, étapes 4 et 12
