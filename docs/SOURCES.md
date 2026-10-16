# Sources, provenance et niveaux de confiance

Ce document tient le registre de **d'où vient chaque information** utilisée par
l'application, et avec quel niveau de preuve. Toute affirmation sur le matériel
ou sur un protocole doit pouvoir être rattachée à une ligne de ce fichier.

## Niveaux de confiance

| Étiquette | Signification |
|---|---|
| `[SOURCE-JS]` | Lu directement dans `references/fronius-original.js`. Prouve la présence d'un code côté client, **pas** une permission accordée par le firmware. |
| `[SOURCE-PDF]` | Lu dans l'export système du fabricant (`fronius-information.pdf`, 16 octobre 2026). |
| `[OBSERVÉ-CONVERSATION]` | Rapporté lors de l'analyse, non rejoué dans cette session. |
| `[HYPOTHÈSE]` | Forme ou comportement supposé, encodé comme tel dans le code. |
| `[À-TESTER]` | Vérifiable facilement, mais non vérifié : nécessite le matériel ou la sonde. |
| `[VÉRIFIÉ]` | Exécuté et observé dans ce dépôt (tests, simulateur, commande reproductible). |

Règle permanente : « trouvé dans le bundle » ≠ « autorisé par le serveur ».
Aucun élément `[SOURCE-JS]` ne doit être présenté comme une permission firmware.

## Artefact du fabricant

`references/fronius-original.js` — 184 378 lignes, ≈ 8,6 Mo. C'est le bundle
JavaScript de l'interface Web du fabricant, conservé **non modifié** comme
source primaire d'analyse, conformément à `AGENTS.md`.

Ce fichier est un artefact logiciel propriétaire (Fronius International GmbH).
Il est **explicitement exclu du périmètre de la licence MIT** du présent dépôt
(voir `LICENSE` et la section Licence en fin de document).

## Repères de code dans le bundle

Numéros de ligne dans `references/fronius-original.js`. Ils peuvent changer
après un reformatage du fichier ; les modules sont identifiés par leur `id`
Webpack, qui reste stable.

| Module | Rôle | Début | Retenu pour |
|---|---|---|---|
| `CNEH` | Stockage des identifiants dans `localStorage` (clé `AuthHash`) | 24792 | INTERDIT à reproduire |
| `JWhm` | Classe HTTP de base : `callGet$`, `callPost$`, `callGetWithoutCatch$`, `callPostWithoutCatch$`, `_getUserAndPass` | 46761 | Phase 2 |
| `LsM5` | DAO de login, mise à jour du store | 48466 | Contexte |
| `i3oe` | Service de commandes (`Login`, `Logout`, `SetPassword`, `ActivatePasswords`, …) | 90501 | Phase 2 / hors périmètre |
| `mgOI` | Composant de login | 121441 | Contexte |
| `nuFz` | Action `[Login] login`, logique de rôle dans le store | 122472 | Contexte |
| `xeXE` | Génération des en-têtes HTTP Digest | 182391 | Phase 2 |
| `ytu1` | Ré-export du module Digest | 183395 | Phase 2 |

Autres repères : `createNode` (dérivation de mots de passe) vers 43079,
`recoverPassword` vers 137550, `createSdpFile` vers 136669.

## Vérifications exécutées sur le bundle

Commandes reproductibles depuis la racine du dépôt, opérant en **lecture seule**
sur l'artefact :

| Recherche | Résultat | Conséquence |
|---|---|---|
| `grep -c fcgi` | `0` | Aucun endpoint `.fcgi` n'est appelé par l'interface du fabricant. |
| `grep -c RealtimeData` | `0` | `GetPowerFlowRealtimeData` est absent du bundle. |
| `grep -c PowerFlowRealtime` | `0` | idem. |
| `grep -c solar_api` | `1` | Unique occurrence : `new h(t,"/config/solar_api",this)` — un endpoint de **configuration**, pas de mesure. |
| `grep -c x-www-authenticate` | `7` | En-tête de défi non standard, tel qu'implémenté côté client. |
| `grep -c` sur `www-authenticate` sans préfixe `x-` | `0` | Aucune variante standard n'est lue par le client. |
| `grep -c AuthHash` | `15` | Identifiants stockés en clair côté client (voir ci-dessous). |

**Conclusion structurante : le bundle ne contient aucun chemin Solar API.** Il ne
fournit donc **aucun schéma** de `GetPowerFlowRealtimeData` : ni noms de champs,
ni unités, ni emplacements. Le schéma de `internal/fronius/solarapi/dto.go` est
une `[HYPOTHÈSE]` fondée sur la documentation publique de la Solar API v1 et sur
des observations rapportées. C'est la raison d'être de la sonde
`cmd/fronius-probe` et de `docs/PROBE-READONLY.md`.

## Authentification (contexte phase 2, non implémentée)

Ce qui peut être affirmé `[SOURCE-JS]` :

- Défi lu dans l'en-tête de réponse **`x-www-authenticate`** (non standard) ;
  `realm` et `nonce` obligatoires, sinon l'en-tête `Authorization` reste vide.
- `HA1 = MD5(user:realm:pass)`, `HA2 = MD5(METHOD:uri)`,
  `response = MD5(HA1:nonce[:nc:cnonce:qop]:HA2)`.
- **Un seul** retry sur HTTP 401 (GET et POST) ; les variantes
  `callGetWithoutCatch$` / `callPostWithoutCatch$` ne réessaient pas.
- Identifiants `"user:password"` découpés au premier `:`.
- `CNEH` écrit `localStorage.setItem("AuthHash", username + ";" + password)` :
  identifiants **en clair** dans le navigateur. Ce stockage n'est jamais
  reproduit par cette application.
- `createNode` : `MD5("user:Webinterface area:password")` puis PBKDF2
  (100 000 itérations, sortie 64 octets, sel de 4 octets issu de
  `Math.random()`). Hors périmètre MVP, documenté seulement.

Ce qui ne peut pas être affirmé : que le firmware accorde effectivement telle
permission à tel rôle. Les noms de rôles (`customer`, `technician`, `support`,
`profi`, `utility`) sont des `[SOURCE-JS]` côté client.

## Inventaire des endpoints

`03-API-INVENTAIRE.md` recense les chemins **présents dans le bundle** :
35 sous `/commands/`, 24 sous `/config/`, 20 autres (`/components/*`,
`/status/*`, `/update/start`). Cet inventaire est **statique** : il ne prouve ni
l'accessibilité, ni la méthode, ni les permissions.

| Chemin | Méthode | Auth | Statut de la preuve |
|---|---|---|---|
| `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | aucune en phase 1 | `[OBSERVÉ-CONVERSATION]` « répond » ; **absent du bundle** ; schéma `[HYPOTHÈSE]` |
| `/status/powerflow` | GET | interne | `[SOURCE-JS]` — interrogé toutes les 5 000 ms par le client officiel ; sert de base à `FRONIUS_POLL_INTERVAL` |
| `/config/*` (24 chemins), `/status/*`, `/components/*` | GET | Digest « Customer » | `[OBSERVÉ-CONVERSATION]` pour quelques-uns (`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` : 200 ; `/config/powerlimits`, `/config/setup/*` : 200 `{}`) ; phase 2 |
| `/commands/*` | GET/POST | Digest selon commande | `[SOURCE-JS]` ; plusieurs sont des mutations — **hors périmètre**, jamais appelées |

Ce qui manque encore et ce qui le lèverait :

- Schéma réel de la Solar API → exécution de `fronius-probe` (§ `docs/PROBE-READONLY.md`).
- Absence ou présence d'une authentification Solar API sur le firmware 1.19.7-1 → sonde `[À-TESTER]`.
- Source HTTP de l'identité de l'onduleur (modèle, firmware, numéro de série) :
  aujourd'hui le PDF uniquement. Candidats `[À-TESTER]` : `/status/version`,
  `/status/common`, `/components/PowerMeter/readable`.
- Présence d'un Smart Meter ou d'une batterie : aucune preuve, ni dans le PDF,
  ni dans le bundle. Voir `docs/UNKNOWNS.md`.

## Forme supposée de la réponse Solar API v1

Encodée **uniquement** dans `internal/fronius/solarapi/dto.go` (`[HYPOTHÈSE]`) :

- enveloppe `{"Body":{"Data":{…}},"Head":{…}}` ;
- `Body.Data.Site` : `Mode`, `P_PV`, `P_Grid`, `P_Load`, `P_Akku`, `E_Day`,
  `E_Year`, `E_Total`, `rel_Autonomy`, `rel_SelfConsumption`,
  `Meter_Location_Current` ;
- `Body.Data.Inverters` : objet indexé par numéro (« 1 », « 2 », …) avec `DT`,
  `P`, `E_Day`, `E_Year`, `E_Total` ;
- `Head` : `RequestArguments`, `Status` (`Code`, `Reason`, `UserMessage`),
  `Timestamp` (RFC3339).

Règles de normalisation appliquées par `internal/model` :

- unités conservées telles quelles : puissances en **W**, énergies en **Wh**,
  ratios en **%** (conversion en kW/kWh seulement à l'affichage, `units.go`) ;
- `Timestamp` inexploitable ⇒ horodatage du collecteur, `Quality = estimated` ;
- champ absent ⇒ `nil`, **jamais** `0` ; un `0` explicite de l'appareil reste `0` ;
- `P_Grid`, `P_Load`, `P_Akku`, `rel_*` restent `null` sans Smart Meter ni
  batterie : aucune consommation ni import/export n'est calculé sans mesure.

Repère fourni pour la réconciliation future : la documentation officielle
Fronius Solar API v1, à confronter au firmware **1.19.7-1**. Si la sonde révèle
d'autres noms ou d'autres unités, la correction porte sur `dto.go`, les fixtures
et ce fichier — les colonnes SQLite restent en unités de base.

## Autres endpoints Solar API — candidats non vérifiés

Ces noms proviennent de la **documentation publique Fronius Solar API v1**.
Aucun n'a été relu dans `references/fronius-original.js` (qui ne contient aucun
chemin Solar API) ni observé sur le firmware **1.19.7-1** : ils sont donc tous
`[À-TESTER]`. Ils sont listés pour cadrer la phase 2, pas pour être appelés.

| Endpoint candidat | Ce qu'il apporterait | Statut |
|---|---|---|
| `/solar_api/GetAPIVersion` | Version de l'API exposée par l'appareil | `[À-TESTER]` |
| `/solar_api/v1/GetInverterRealtimeData.fcgi` | Mesures par onduleur (`CommonInverterData`, `3PInverterData`, `CumulationInverterData`) : tension, courant, fréquence, température, état | `[À-TESTER]` (redondant avec PowerFlow pour la puissance) |
| `/solar_api/v1/GetInverterInfo.fcgi` | **Identité** : modèle, numéro de série, firmware par onduleur | `[À-TESTER]` — candidat direct pour la capacité `device.identity` |
| `/solar_api/v1/GetLoggerInfo.fcgi` | Identité du datalogger / de l'interface | `[À-TESTER]` |
| `/solar_api/v1/GetMeterRealtimeData.fcgi` | Mesures du Fronius Smart Meter, si présent | `[À-TESTER]` — permettrait de **détecter** un compteur et de débloquer `grid.import_export`, `load.consumption`, `autoconsumption.ratio` |
| `/solar_api/v1/GetStorageRealtimeData.fcgi` | État de charge et puissance de la batterie, si présente | `[À-TESTER]` — débloquerait `battery.soc` |
| `/solar_api/v1/GetActiveDeviceInfo.fcgi` | Liste des appareils actifs du système | `[À-TESTER]` |
| `/solar_api/v1/GetDeviceInfo.fcgi` | Inventaire d'appareils | `[À-TESTER]` |

Conséquence pour la phase 1 : la détection de la présence d'un Smart Meter ou
d'une batterie est **réalisable sans écriture**, par de simples GET sur ces
endpoints, mais elle n'est pas implémentée faute de preuve d'existence sur le
firmware. Tant qu'elle ne l'est pas, les capacités correspondantes restent
indisponibles avec un motif explicite (`internal/model/capabilities.go`).

## Fixtures

`test/fixtures/solarapi/*.json` portent un champ racine `_provenance` **non
consommé** par le client :

| Fichier | `synthetic` | `confidence` | Origine |
|---|---|---|---|
| `powerflow_produce_only.json` | `true` | `HYPOTHÈSE` | `cmd/fronius-sim`, seed 42, instant fixé |
| `powerflow_with_meter.json` | `true` | `HYPOTHÈSE` | idem, avec `--with-meter` |
| `powerflow_status7.json` | `true` | `HYPOTHÈSE` | idem, `Head.Status.Code = 7` |
| `powerflow_invalid.json` | — | — | corps tronqué, volontairement illisible |

Promotion d'une fixture : une capture réelle produite par `fronius-probe --save`
doit passer `synthetic` à `false`, `confidence` à `VÉRIFIÉ (matériel)`, porter le
numéro de série masqué, et être annoncée dans `CHANGELOG.md`. Voir
`docs/PROBE-READONLY.md`.

## Matériel de référence

`[SOURCE-PDF]` export système du 16 octobre 2026 : WebUI `1.14.2-1`,
`GEN24/DEVICEGROUP/GEN24PRIMO 1.19.7-1`, numéro de série `33319328`, 3680 W,
Wi-Fi `192.0.2.10` / `255.255.254.0` / passerelle `192.0.2.1`,
Ethernet `disconnected`, fonctionnalités actives `Battery Operation` et
`Full Backup`.

Limite explicite : cet export **ne contient aucun inventaire de composants**.
Ni le PDF ni le bundle ne prouvent la présence ou l'absence d'un Fronius Smart
Meter ou d'une batterie. Les fonctionnalités « actives » du PDF décrivent des
capacités logicielles déclarées, pas le matériel installé.

## Licence

L'application est distribuée sous licence MIT (Copyright © 2026 Annika & Remy).

`references/fronius-original.js` est un artefact du fabricant, conservé à titre
documentaire comme source primaire exigée par `AGENTS.md`. Il **n'est pas**
couvert par la licence MIT et ne peut pas être redistribué comme partie du
logiciel produit. La voie de repli, si l'artefact ne doit pas être versionné,
consiste à l'ajouter à `.gitignore` et à ne conserver ici que sa provenance.
