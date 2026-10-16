# ADR 0008 — Périmètre des données exposées : modèle de domaine, pas de miroir d'API

## Statut

Accepté (phase 2)

## Contexte

L'extraction du bundle (`docs/ENDPOINTS.md`, section « Méthode, arguments et DTO
par chemin ») a remplacé les inconnues par des faits : 54 chemins `READ` sont
documentés avec leur méthode, leurs arguments, leur enveloppe et les champs que
le client officiel **lit réellement** — 43 signaux par onduleur, 23 par Smart
Meter, 10 par batterie, 11 champs d'événement, des configurations entières, plus
10 chemins dont la charge utile n'est jamais consommée.

La question qui suit est mécanique : « puisque l'appareil expose tout cela,
faut-il tout exposer à l'interface ? » Elle se heurte à quatre contraintes déjà
posées :

- `AGENTS.md` : ne jamais calculer consommation, import/export ou autoconsommation
  sans mesure validée ; n'ajouter l'authentification interne que si **une métrique
  le justifie** ; marquer `unverified` tout endpoint non testé ; toute nouvelle
  information documentée avec sa source et sa version de firmware ;
- l'ADR 0003 : la Solar API d'abord, l'API interne en phase 2 ;
- l'ADR 0004 : aucune commande, aucun endpoint `MUTATION` ou `DANGEROUS` ;
- la définition de « terminé » : erreurs et états vides gérés, compatibilité
  hors ligne.

Un miroir d'API (par exemple `GET /api/v1/raw/<chemin Fronius>`) satisferait la
question en apparence, et coûterait : le contrat public deviendrait celui d'un
firmware tiers, chaque écran devrait connaître une forme non validée, et la
moindre évolution d'appareil deviendrait une rupture d'interface.

## Décision

1. **Le contrat public de l'application est le modèle de domaine**, plus
   `/api/v1/capabilities` pour dire ce qui est disponible, absent ou inconnu. Le
   frontend ne connaît **aucun** chemin Fronius et ne reçoit aucune charge utile
   brute d'appareil.
2. **La Solar API d'abord.** Un ajout à la liste blanche interne exige une
   métrique que la Solar API ne fournit pas, écrite dans `docs/ENDPOINTS.md`.
3. **Un ajout sans consommateur n'est pas un ajout.** Toute entrée nouvelle doit
   nommer son usage — un champ du modèle, un endpoint, un écran. Ce qui est
   extrait mais inutilisé reste documenté dans `docs/ENDPOINTS.md`, pas dans le
   code.
4. **Une donnée non mesurée n'est pas calculée.** Une capacité dont la mesure
   manque reste indisponible avec son motif ; un champ absent reste `nil`, jamais
   `0` ni une valeur de remplacement empruntée à un autre champ.
5. **Un endpoint non éprouvé est annoncé comme tel** (`unverified`), y compris
   quand sa méthode et ses champs sont prouvés par le bundle : la preuve manquante
   est la réponse de l'appareil, pas la lecture du client officiel.
6. **Aucun secret vers le frontend**, en aucune circonstance.

## Conséquences

- « Exposer toutes les données de la Solar API » n'est **pas** un objectif : c'est
  un inventaire, pas un besoin. L'inventaire vit dans `docs/ENDPOINTS.md`.
- Le coût d'un ajout est désormais mesurable et inégal, et se lit dans l'arbre :
  ajouter un champ à un DTO déjà extrait et déjà consommé a coûté **un** fichier de
  production plus son test (les champs d'événement, `internal/detect/detector.go`) ;
  exposer un champ d'identité nouveau jusqu'à l'écran en coûte **14** (dont une
  migration SQL), puisque `device` est figé par `0002_device.sql` — décision
  reportée, coût consigné dans `docs/UNKNOWNS.md` #3.
- Un écran ne casse pas quand l'appareil change : seule la couche d'extraction
  bouge, et elle est couverte par des tests unitaires.
- Les 10 chemins dont la charge utile n'est jamais lue par le client officiel
  (`/status/registrationData`, `/status/powerflowcontroller`, `/commands/StandbyState`,
  `/commands/GetSDPFile`, `/commands/ConnectedPowermeter?componentId=`, les deux
  variantes `?sync=true`, `/config/logging`, `/config/solarweb`, `/config/gridcode`)
  restent hors périmètre jusqu'à ce qu'une métrique les nomme.
- L'état hors ligne reste tenable : le frontend ne dépend que du modèle persisté.

## Alternatives écartées

- **Miroir 1:1 de l'API Solar** : le contrat public deviendrait un contrat tiers
  non validé sur `1.19.7-1` ; rupture à chaque évolution d'appareil ; impossible à
  marquer `unverified` champ par champ.
- **Indexer tous les signaux `channels`** (43 onduleur, 23 compteur, 10 batterie) :
  volume de stockage et de schéma sans mesure de besoin, et unités non validées ;
  contredit l'invariant « pas de valeur inventée ».
- **Exposer les chemins non `READ`** (34 sites d'appel, dont des `POST`) : viole
  l'ADR 0004.
- **Reproduire l'interface du fabricant** : interdit par `AGENTS.md` (aucune
  dépendance à l'interface du fabricant).

## Sources

- `docs/ENDPOINTS.md` (extraction par chemin, [SOURCE-JS], lignes du bundle)
- `docs/SOURCES.md` (provenance module par module, niveau de confiance)
- `docs/UNKNOWNS.md` (#3 identité, #4/#5 présence, #6 rôles, #11 événements)
- `AGENTS.md` (exigences), `docs/adr/0003`, `docs/adr/0004`
