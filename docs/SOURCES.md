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
| `CNEH` | Stockage des identifiants dans `localStorage` (clé `AuthHash`) | 24793 | INTERDIT à reproduire |
| `JWhm` | Classe HTTP de base : `callGet$`, `callPost$`, `callGetWithoutCatch$`, `callPostWithoutCatch$`, `_getUserAndPass` | 46762 | Phase 2 |
| `LsM5` | DAO de login, mise à jour du store | 48467 | Contexte |
| `i3oe` | Service de commandes (`Login`, `Logout`, `SetPassword`, `ActivatePasswords`, …) | 90502 | Phase 2 / hors périmètre |
| `pMcQ` | Énumération du **type d'enveloppe** de réponse : `Default = 0`, `Cgi = 1`, `CommandResult = 2` (l. 155872-155877) | 155866 | Extraction des enveloppes |
| `mgOI` | Composant de login | 121442 | Contexte |
| `nuFz` | Action `[Login] login`, logique de rôle dans le store | 122473 | Contexte |
| `xeXE` | Génération des en-têtes HTTP Digest | 182391 | Phase 2 (implémenté) |
| `ytu1` | Ré-export du module Digest | 183396 | Phase 2 (implémenté) |
| `aYRN` | Registre d'endpoints « pollés » (classe `b`, héritant de `JWhm`) : chemin, intervalle, type d'enveloppe et rôle déclaré par entrée (l. 76027-76045) | 75885 | Chemins internes de la phase 2 |
| `yTJA` | Service d'état : deuxième client, indépendant de `JWhm`, qui lit `httpClient.get(this.url)` **sans** en-tête `Authorization` (l. 183019-183021) | 182982 | Deuxième voie vers les mêmes chemins |
| `rCKn` | DAO de configuration : une instance par chemin `/config/*` (l. 161462-161485), `loadData()` = `GET` (l. 161422) et `saveData()` = `POST` (l. 161428), enveloppe `Default` | 161333 | Extraction des méthodes `/config/*` |
| `lFFc` | `/commands/License` et ses variantes `?status=`, `?acknowledgement=`, `?serviceFile=` (l. 120669-120677) | 120656 | Extraction (hors liste blanche) |
| `966A` | `/commands/InverterRollbackStatus` (l. 21341) | 21319 | Extraction (hors liste blanche) |
| `rqlu` | Identifiant d'événement `prefix-eventID` et clé de traduction des états (l. 164699, 164705) | 164690 | DTO de `/status/activeEvents` |
| `jRQl` | Énumération `T_SystemServiceId` (`GridlimitsAutotest = 21`, l. 116025) | 115999 | Argument `serviceId` |

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

Commandes de l'extraction méthode/arguments/DTO (voir `docs/ENDPOINTS.md`) :

| Recherche | Résultat | Conséquence |
|---|---|---|
| `grep -n 'callGet\$(\|callGetWithoutCatch\$(\|callPost\$(\|callPostWithoutCatch\$('` | `53` occurrences | moins les 4 définitions de `JWhm` (l. 46782, 46787, 46791, 46796) et les 3 lignes de `_refreshValue` (l. 75958, 75960, 75963) ⇒ **46 lignes**, dont 2 délégations internes (l. 46784, 46793) ⇒ **44 sites d'appel** |
| `grep -n '_httpClient\.\(get\|post\)(' \| awk '$1 < 46761 \|\| $1 > 46820'` (les `\|` sont les échappements shell) | `7` | 6 appels bruts sur des chemins de l'appareil (SDP, Service/License blob), 1 hors périmètre (icônes Angular Material, l. 54377) |
| `grep -n '/status/version'` | `1` | une seule occurrence (l. 183050, module `yTJA`) : `/status/version` **n'est pas** dans le registre pollé `aYRN` |
| `grep -n 'new h(t,"/config'` | `24` | un DAO et un littéral par chemin `/config/*` (l. 161462-161485) |

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
| Registre « pollé » | `aYRN` (l. 75885), `let b = class extends f.a` où `f = n("JWhm")` (l. 75899, 75901) | oui : `callGet$` = GET + Digest + **un** retry sur 401 (l. 46782-46786) | `/status/devices` l. 76027, `/status/network` l. 76028, `/status/emrs` l. 76029, `/status/activeEvents` l. 76030, `/status/events` l. 76031, `/components/BatteryManagementSystem/readable` l. 76032, `/components/cache/readable` l. 76033, `/components/Ohmpilot/readable` l. 76034, `/components/PowerMeter/readable` l. 76035, `/status/powerflow` l. 76036, `/commands/GetUTCTime` l. 76037, `/status/common` l. 76041, `/commands/LicenseExchange` l. 76042, `/commands/GetServiceState?serviceId=` l. 76043, `/commands/RemoteAccess` l. 76044, `/commands/StandbyState` l. 76045 — **`/status/version` n'y figure pas** |
| Service d'état | `yTJA` (l. 182982), classe `A` **indépendante de `JWhm`** | **non** : `this.httpClient.get(this.url)` (l. 183020), aucune construction d'en-tête | `/status/common` l. 183049, `/status/version` l. 183050, `/status/registrationData` l. 183051, `/status/network` l. 183052, `/status/capabilities` l. 183053, `/status/ioServices` l. 183054, `/status/powerflowcontroller` l. 183055, `/status/deviceDB` l. 183056, `/components/DataManager/readable` l. 183057, `/status/license` l. 183058, `/status/license/features` l. 183059, `/status/activeEvents?sync=true` l. 183060, `/status/events?sync=true` l. 183061 |

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
| `/status/version` | `serialNumber` | oui | l. 45243, l. 127380, l. 143048 (`t.serialNumber`) |
| `/status/version` | `numberOfPhases` | oui | l. 183034 |
| `/status/version` | `devicegroup` | oui | l. 183035 (`"P" === t.devicegroup[0] ? 1 : 3`) |
| `/status/version` | `hwrevisions.FAMILYID` | oui | l. 143049 |
| `/status/version` | `hardwareId`, `devicename`, `articleNumber` | oui | l. 126568, 128813, 128814, 128842 — **lus** par le client officiel, **non utilisés** par l'application à ce stade : les exposer jusqu'à l'écran coûte 14 fichiers dont une migration SQL (`docs/UNKNOWNS.md` #3), décision reportée |
| `/status/version` | `swrevisions.GEN24` | oui | l. 128882, 135137 — **lu par l'application** : c'est la version logicielle affichée par le client officiel ; clé imbriquée, lue avant les candidats plats (`firmwareOf`, `internal/fronius/internalapi/client.go`) |
| `/status/version` | `firmwareVersion` | **non** | candidat de secours, utilisé seulement si `swrevisions.GEN24` est absent |
| `/status/version` | `modelName`, `model` | **non** | candidats : **aucune clé de modèle** n'est observée dans le bundle. `devicegroup` n'est pas un repli de modèle — c'est le groupe d'appareil, champ frère (l. 183035) |
| `/components/PowerMeter/readable` | `channels` (23 signaux `SMARTMETER_*`), `attributes.addr`, `attributes["[ENABLE]"]` | oui | l. 129011, 129015, 130976-130983 ; enveloppe `Cgi` (l. 75958, 183011) ; la **détection** se fait toujours sur `Body.Data` non vide |
| `/components/BatteryManagementSystem/readable` | `channels` (10 signaux) | oui | l. 128986 ; enveloppe `Cgi` (l. 75958) |
| `/status/activeEvents` | `severity` (entier : 1 erreur, 2 avertissement, 3 information) | oui | l. 135690 + l. 135728-135733 (`getEventLevel`) ; **utilisé par l'application** (`severityValue`, `internal/detect/detector.go`) — avant correction, la sévérité entière n'était pas reconnue et tout événement matériel était classé « information » |
| `/status/activeEvents` | `uuid`, `prefix`-`eventID` (identité d'instance) | oui | l. 135693, 164699, 135742 (comparaison), 135760 (`ConfirmEvent` reçoit `{uuid}`) ; **utilisés par l'application** comme clé d'anti-duplication |
| `/status/activeEvents` | `label` (texte affiché en repli par le client) | oui | l. 135654 ; **utilisé par l'application** comme texte d'événement |
| `/status/activeEvents` | `timestamp`, `activeUntil` (secondes d'horloge appareil) | oui | l. 135694-135695 — non utilisés : `TS` reste l'horloge du collecteur, axe de tri et de rétention |
| `/status/activeEvents` | `viewer`, `category`, `confirmable`, `sourceID` | oui | l. 135691, 135696, 135697, 135701 — lus par le client, **non utilisés** par l'application : `Detail` conserve le JSON brut |
| `/status/activeEvents` | `code`, `state`, `level`, `message`, `text`, `description` | **non** | candidats de l'application : ils servent à la déduplication et à la sévérité, avec repli systématique |

Le client extrait l'identité par **listes de clés candidates ordonnées**
(`internal/fronius/internalapi/client.go`) : ajouter une clé observée en tête de
la liste correspondante suffit, aucune structure n'est à changer.

## Extraction méthode / arguments / DTO par chemin

`docs/ENDPOINTS.md` (section « Méthode, arguments et DTO par chemin (extraction
bundle) ») porte la table par chemin. Provenance de cette extraction, tout en
confiance `[SOURCE-JS]`, **module par module**, dans le bundle conservé non
modifié :

| Module | Lignes | Ce qui en est tiré |
|---|---|---|
| `aYRN` | 75885-76048 | registre pollé : chemin, intervalle, type d'enveloppe (l. 76027-76045) ; `_refreshValue` prouve que **toutes** les entrées sont lues en `GET` (l. 75955-75964) ; `_isUserAllowed` (l. 75972-75984) et `{user: "customer"}` (l. 76037-76040) |
| `JWhm` | 46762-46822 | quatre méthodes d'appel : `callGet$`/`callGetWithoutCatch$` (GET) et `callPost$`/`callPostWithoutCatch$` (POST), plus `_getUserAndPass` (l. 46782-46813) |
| `pMcQ` | 155866-155878 | énumération du type d'enveloppe : `Default`, `Cgi`, `CommandResult` |
| `yTJA` | 182982-183066 | deuxième client sans en-tête : littéraux des chemins `/status/*` et `/components/DataManager/readable`, enveloppe par chemin (l. 183010-183021) |
| `i3oe` | 90502-91250 | sites d'appel des `/commands/*`, arguments d'URL et corps de requête |
| `rCKn` | 161333-161540 | DAO `/config/*` : un littéral par chemin, `loadData` (GET, l. 161422), `saveData` (POST, l. 161428) |
| `lFFc`, `966A` | 120656-120780, 21319-21345 | `/commands/License` et variantes, `/commands/InverterRollbackStatus` |
| `rqlu`, `jRQl` | 164690-164709, 115999-116025 | identifiant d'événement (`prefix`-`eventID`), énumération `T_SystemServiceId` |

Les champs d'aboutissement (signaux `channels`, clés `Hosts[]`, etc.) sont
relevés dans les composants consommateurs, dont les numéros de ligne figurent dans
la même table. Version de référence inchangée : `WebUI 1.14.2-1` et
`GEN24/DEVICEGROUP/GEN24PRIMO 1.19.7-1` (§ Matériel de référence).

Ce que l'extraction **n'établit pas** : aucune permission serveur. Les rôles
relevés sont des conditions d'affichage du client (voir `docs/UNKNOWNS.md` #6).

## Inventaire des endpoints

`03-API-INVENTAIRE.md` recense les chemins **présents dans le bundle** :
35 sous `/commands/`, 24 sous `/config/`, 20 autres (`/components/*`,
`/status/*`, `/update/start`). Cet inventaire est **statique** : il ne prouve ni
l'accessibilité, ni la méthode, ni les permissions.

| Chemin | Méthode | Auth | Statut de la preuve |
|---|---|---|---|
| `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | aucune en phase 1 | `[OBSERVÉ-CONVERSATION]` « répond » ; **absent du bundle** ; schéma `[HYPOTHÈSE]` |
| `/status/powerflow` | GET | interne | `[SOURCE-JS]` — interrogé toutes les 5 000 ms par le client officiel ; sert de base à `FRONIUS_POLL_INTERVAL` |
| `/status/version` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 183050 (voie `yTJA`, sans en-tête — **ce chemin n'est pas dans le registre `aYRN`**) ; `[VÉRIFIÉ]` sur le simulateur. Fournit `serialNumber`, `numberOfPhases`, `devicegroup` |
| `/status/activeEvents` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76030 (intervalle 10 000 ms) ; `[VÉRIFIÉ]` sur le simulateur. Champs lus relevés dans `docs/ENDPOINTS.md` |
| `/components/PowerMeter/readable` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76035 (enveloppe `Cgi`) ; `[VÉRIFIÉ]` sur le simulateur. Présence déduite de `Body.Data` |
| `/components/BatteryManagementSystem/readable` | GET | Digest « après 401 » | **appelé** (phase 2) : `[SOURCE-JS]` l. 76032 ; `[VÉRIFIÉ]` sur le simulateur. Présence déduite de `Body.Data` |
| `/config/meter`, `/config/batteries` | `GET` (l. 161422) | Digest « après 401 » | **hors liste** par choix de périmètre : littéral et verbe **observés** (l. 161471 et 161462 ; module `rCKn`), l'affirmation inverse ayant été corrigée |
| `/config/*` (24 chemins), `/status/*`, `/components/*` | GET | Digest « Customer » | `[OBSERVÉ-CONVERSATION]` pour quelques-uns (`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` : 200 ; `/config/powerlimits`, `/config/setup/*` : 200 `{}`) ; phase 2 |
| `/commands/*` | GET/POST | Digest selon commande | `[SOURCE-JS]` ; plusieurs sont des mutations — **hors périmètre**, jamais appelées |

Ce qui manque encore et ce qui le lèverait :

- Schéma réel de la Solar API → exécution de `fronius-probe` (§ `docs/PROBE-READONLY.md`).
- Absence ou présence d'une authentification sur le firmware **1.19.7-1** → sonde
  `[À-TESTER]` : le simulateur exerce les deux cas, il ne prouve pas le firmware.
- Noms réels du **modèle** et du **firmware** dans `/status/version` → sonde
  `[À-TESTER]` : l'extraction établit les champs effectivement lus par le client
  (`serialNumber`, `numberOfPhases`, `devicegroup`, `hardwareId`, `devicename`,
  `articleNumber`, `swrevisions`, `hwrevisions`), mais **ni `modelName` ni
  `firmwareVersion`** n'y figurent : ils restent des candidats du simulateur.
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
