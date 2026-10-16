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
| `xeXE` | Génération des en-têtes HTTP Digest | 182391 | Phase 2 (implémenté) |
| `ytu1` | Ré-export du module Digest | 183395 | Phase 2 (implémenté) |
| `aYRN` | Registre d'endpoints « pollés » (module `b`), héritant de `JWhm` : intervalles et type d'enveloppe par chemin | 75885 | Chemins internes de la phase 2 |
| `yTJA` | Service d'état : lit les mêmes chemins **sans** en-tête `Authorization` | 182982 | Deuxième voie vers les mêmes chemins |

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
| `grep -c 'www-authenticate'` (hors préfixe `x-`) | `0` | Le client officiel ne lit que l'en-tête non standard. |

**Conclusion structurante : le bundle ne contient aucun chemin Solar API.** Il ne
fournit donc **aucun schéma** de `GetPowerFlowRealtimeData` : ni noms de champs,
ni unités, ni emplacements. Le schéma de `internal/fronius/solarapi/dto.go` est
une `[HYPOTHÈSE]` fondée sur la documentation publique de la Solar API v1 et sur
des observations rapportées. C'est la raison d'être de la sonde
`cmd/fronius-probe` et de `docs/PROBE-READONLY.md`.

## Authentification Digest — implémentée en phase 2

Tout ce qui suit est `[SOURCE-JS]` (module `xeXE`, l. 182391-182440, et module
`JWhm`, l. 46761-46795), reproduit dans `internal/fronius/digest` et vérifié par
le simulateur **et** par un client tiers (`curl --digest`, § Vérifications).

### Deux voies HTTP vers les mêmes chemins

C'est le fait structurant de la phase 2 : les mêmes chemins internes sont lus par
deux clients distincts du bundle.

| Voie | Module | En-tête `Authorization` | Preuve |
|---|---|---|---|
| Registre « pollé » | `aYRN` (l. 75885), `class b extends f.a` où `f = n("JWhm")` (l. 75899) | oui : `callGet$` = GET + Digest + **un** retry sur 401 (l. 75961-75963) | `/status/version` l. 76042, `/status/activeEvents` l. 76030, `/components/PowerMeter/readable` l. 76031, `/components/BatteryManagementSystem/readable` l. 76032 |
| Service d'état | `yTJA` (l. 182982), classe `A` **indépendante de `JWhm`** | **non** : `this.httpClient.get(this.url)` (l. 183020), aucune construction d'en-tête | `/status/version` l. 183050, `/status/common` l. 183049, `/status/network` l. 183052, `/components/DataManager/readable` l. 183057 |

Conséquence retenue : **un même chemin peut répondre sans authentification**. Le
client de cette application émet donc toujours la requête nue d'abord, et ne
s'authentifie qu'en réponse à un `401` — exactement ce que fait `callGet$`.

### Forme de l'en-tête produit

`Digest username="U", realm="R", nonce="N", uri="URI", response="RESP"`,
puis `, opaque="O"` si le défi en porte, puis
`, qop=QOP, nc=NC, cnonce="CNONCE"` (l. 182425-182428).

| Point | Valeur `[SOURCE-JS]` | Traitement retenu |
|---|---|---|
| Algorithme | `HA1 = MD5(user:realm:pass)`, `HA2 = MD5(METHOD:uri)`, `response = MD5(HA1:nonce[:nc:cnonce:qop]:HA2)` | reproduit à l'identique ; ancré sur le vecteur RFC 2617 |
| `qop` | tronqué au premier jeton (`qop="auth,auth-int"` → `auth`) | reproduit |
| `nc` | `String(++NC)` complété à 8 chiffres par `NC_PAD` → **décimal** zéro-complété, compteur monotone | reproduit (`%08d`, incrémenté sous verrou) |
| Sans `qop` | ni `qop`, ni `nc`, ni `cnonce` dans l'en-tête (forme RFC 2069) | reproduit |
| `algorithm=` | **jamais émis** par le client officiel | jamais émis (le défi `algorithm=SHA-256` est refusé, pas contourné) |
| `cnonce` | `Number(randomBytes(8)).toString(16)` où `randomBytes` joint des octets hexadécimaux par `,` (l. 182405-182411) → `Number("a3,ff,…")` vaut `NaN` : le client officiel émet littéralement `cnonce="NaN"` | **non reproduit** : 8 octets de `crypto/rand` en hexadécimal (16 caractères). Le serveur relit le `cnonce` de l'en-tête, il ne peut pas le vérifier. Repli documenté dans `CHANGELOG.md` si l'appareil refuse malgré tout. |
| Guillemets | `qop=` et `nc=` **sans** guillemets, `cnonce` **entre** guillemets | reproduit, vérifié caractère par caractère |
| Défi incomplet | `realm` ou `nonce` vide ⇒ en-tête vide (l. 182412-182414) | traduit en erreur explicite, jamais en en-tête vide |

### Retry et stockage du défi

- **Un seul** retry sur HTTP 401 ; les variantes `callGetWithoutCatch$` /
  `callPostWithoutCatch$` ne réessaient pas (l. 46787-46790).
- Le défi est lu dans l'en-tête de réponse **`x-www-authenticate`** (7
  occurrences dans le bundle, **0** occurrence de `www-authenticate` seul) ;
  cette application essaie le non standard d'abord, puis le standard.
- Le défi brut est mémorisé tel quel et re-analysé à chaque en-tête
  (`setNonceHeader`, l. 29232) ; `getUserAndPass` retourne `username:password`
  (l. 29238-29240), découpé au **premier** `:` — un mot de passe peut en contenir.

### Ce qui n'est jamais reproduit

- `CNEH` : `localStorage.setItem("AuthHash", username + ";" + password)`,
  identifiants **en clair** dans le navigateur (l. 29232, 15 occurrences de
  `AuthHash`).
- `createNode` : `MD5("user:Webinterface area:password")` puis PBKDF2
  (100 000 itérations, sortie 64 octets, sel de 4 octets issu de
  `Math.random()`). Hors périmètre.
- Les identifiants ne quittent jamais le processus serveur : ni vers le
  frontend, ni vers une réponse HTTP, ni vers un journal (type `secret.String`).

Ce qui ne peut pas être affirmé : que le firmware accorde effectivement telle
permission à tel rôle. Les noms de rôles (`customer`, `technician`, `support`,
`profi`, `utility`) sont des `[SOURCE-JS]` côté client, et la liste blanche de
`internal/fronius/internalapi/allowlist.go` reste la **seule** autorisation
exécutable de cette application.

## Vérifications exécutées sur le Digest `[VÉRIFIÉ]`

| Vérification | Résultat |
|---|---|
| Vecteur RFC 2617 (`Mufasa` / `testrealm@host.com` / `Circle Of Life`, `nc=00000001`, `cnonce=0a4f113b`) | `response="6629fae49393a05397450978507c4ef1"` — `go test ./internal/fronius/digest/ -run TestRFC2617Vector` |
| Forme de l'en-tête | ordre des champs, `qop=`/`nc=` sans guillemets, `cnonce` entre guillemets, **aucun** `algorithm=` — test sur la chaîne complète |
| Nombre de requêtes par cas | défi frais : 2 (nue puis authentifiée) ; nonce en cache : 1 ; nonce périmé : 3 (cache refusé, requête nue, défi frais) ; 401 sans défi : 1 (aucun retry) ; 401 persistant : 2 (aucune troisième tentative) |
| Client tiers | `curl --digest -u sim:simpass` obtient `200` sur le simulateur en mode `--emit-standard-challenge` — un implémenteur indépendant recalcule le Digest et le serveur l'accepte |
| Unicité du compteur | 50 requêtes concurrentes (`go test -race`) produisent 50 `nc` distincts et croissants |


## Champs des chemins internes

Colonne « clé prouvée ? » : **oui** signifie que le nom est lu dans le bundle à
la ligne citée ; **non** signifie que le nom est un **candidat** du simulateur,
ajouté pour exercer la tolérance de lecture du client, et à confirmer par une
capture (`cmd/fronius-probe`).

| Chemin | Champ | Clé prouvée ? | Preuve |
|---|---|---|---|
| `/status/version` | `serialNumber` | oui | l. 45243 (`t.serialNumber`), l. 183034 |
| `/status/version` | `numberOfPhases` | oui | l. 183034 |
| `/status/version` | `devicegroup` | oui | l. 183035 (`"P" === t.devicegroup[0] ? 1 : 3`) |
| `/status/version` | `firmwareVersion`, `modelName` | **non** | candidats du simulateur, valeurs visiblement fausses (`1.24.3-sim`) |
| `/components/PowerMeter/readable` | *(aucun)* | — | seule l'enveloppe `Cgi` est établie (l. 75958, 183011) : la détection se fait sur `Body.Data` non vide |
| `/components/BatteryManagementSystem/readable` | *(aucun)* | — | idem |
| `/status/activeEvents` | *(aucun)* | — | seule la lecture est établie (l. 76030) ; le JSON brut est conservé borné à 2 048 octets |
| `/status/activeEvents` | `code`, `state`, `severity`, `level`, `message`, `text`, `description` | **non** | candidats : ils servent à la déduplication et à la sévérité, avec repli systématique |

Le client extrait l'identité par **listes de clés candidates ordonnées**
(`internal/fronius/internalapi/client.go`) : ajouter une clé observée en tête de
la liste correspondante suffit, aucune structure n'est à changer.

## Inventaire des endpoints

`03-API-INVENTAIRE.md` recense les chemins **présents dans le bundle** :
35 sous `/commands/`, 24 sous `/config/`, 20 autres (`/components/*`,
`/status/*`, `/update/start`). Cet inventaire est **statique** : il ne prouve ni
l'accessibilité, ni la méthode, ni les permissions.

| Chemin | Méthode | Auth | Statut de la preuve |
|---|---|---|---|
| `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | aucune en phase 1 | `[OBSERVÉ-CONVERSATION]` « répond » ; **absent du bundle** ; schéma `[HYPOTHÈSE]` |
| `/status/powerflow` | GET | interne | `[SOURCE-JS]` — interrogé toutes les 5 000 ms par le client officiel ; sert de base à `FRONIUS_POLL_INTERVAL` |
| `/status/version` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76042 et 183050 ; `[VÉRIFIÉ]` sur le simulateur. Fournit `serialNumber`, `numberOfPhases`, `devicegroup` |
| `/status/activeEvents` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76030 (intervalle 10 000 ms) ; `[VÉRIFIÉ]` sur le simulateur. Forme du corps non établie |
| `/components/PowerMeter/readable` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76031 (enveloppe `Cgi`) ; `[VÉRIFIÉ]` sur le simulateur. Présence déduite de `Body.Data` |
| `/components/BatteryManagementSystem/readable` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76032 ; `[VÉRIFIÉ]` sur le simulateur. Présence déduite de `Body.Data` |
| `/config/meter`, `/config/batteries` | non observé | — | **hors liste** : littéral et verbe hors de la zone lisible du bundle (module `rCKn`), non observés. Les utiliser reviendrait à inventer un contrat |
| `/config/*` (24 chemins), `/status/*`, `/components/*` | GET | Digest « Customer » | `[OBSERVÉ-CONVERSATION]` pour quelques-uns (`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` : 200 ; `/config/powerlimits`, `/config/setup/*` : 200 `{}`) ; phase 2 |
| `/commands/*` | GET/POST | Digest selon commande | `[SOURCE-JS]` ; plusieurs sont des mutations — **hors périmètre**, jamais appelées |

Ce qui manque encore et ce qui le lèverait :

- Schéma réel de la Solar API → exécution de `fronius-probe` (§ `docs/PROBE-READONLY.md`).
- Absence ou présence d'une authentification sur le firmware **1.19.7-1** → sonde
  `[À-TESTER]` : le simulateur exerce les deux cas, il ne prouve pas le firmware.
- Noms réels du **modèle** et du **firmware** dans `/status/version` → sonde
  `[À-TESTER]` : seuls `serialNumber`, `numberOfPhases` et `devicegroup` sont
  établis ; `modelName` et `firmwareVersion` sont des candidats du simulateur.
- Forme de `/status/activeEvents` → sonde `[À-TESTER]` : le détail brut est
  conservé borné, donc aucune information n'est perdue en attendant.
- Présence d'un Smart Meter ou d'une batterie sur l'installation : le simulateur
  exerce les quatre combinaisons, le matériel tranchera. Voir `docs/UNKNOWNS.md`.

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

La sonde phase 2 écrit un fichier par chemin exploré, nommé d'après le chemin :
`/status/version` → `probe_status_version_<UTC>.json`, etc. Le nom ne peut pas
venir du seul dernier segment (« readable » pour les deux composants).

Promotion d'une fixture : une capture réelle produite par `fronius-probe --save`
doit passer `synthetic` à `false`, `confidence` à `VÉRIFIÉ (matériel)`, porter
`firmware`, `captured_at`, `http_status` et `digest_required`, ne contenir aucun
numéro de série, et être annoncée dans `CHANGELOG.md`. Voir
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
