# Classification des endpoints — READ / MUTATION / DANGEROUS

Inventaire **statique** issu de `03-API-INVENTAIRE.md` (chemins littéraux présents
dans `references/fronius-original.js`) complété par la lecture des modules
`i3oe`, `JWhm`, `mgOI`, `nuFz`.

## Méthode et limites

- La classification décrit **l'intention du chemin et son usage dans le bundle**.
  Elle ne constitue **jamais** une permission accordée par le firmware : aucune
  matrice de rôles n'est prouvée à ce stade (voir `docs/UNKNOWNS.md`).
- `READ` : n'a pas d'effet persistant attendu côté appareil.
- `MUTATION` : modifie un état, une session, un fichier ou une préférence.
- `DANGEROUS` : peut couper la production, modifier la protection réseau
  électrique, le firmware ou les accès.
- Tout ce qui n'est pas `READ` est **hors périmètre** et n'est jamais appelé par
  l'application, la sonde ou les tests.

## Liste blanche exécutable

Cinq entrées, et aucune autre : c'est la table figée dans
`internal/fronius/internalapi/allowlist.go` (quatre chemins internes) et dans le
client Solar API (un chemin). `cmd/fronius-probe` n'accepte rien d'autre
(`--list`, `--endpoint`).

| Chemin | Méthode | Classification | Enveloppe | Preuve |
|---|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | READ | — | `[OBSERVÉ-CONVERSATION]` « répond » ; **absent du bundle** ; méthode GET imposée par `internal/fronius/readonly.go` |
| `/status/version` | GET | READ | `Default` | `[SOURCE-JS]` l. 183050 (voie **sans** en-tête, module `yTJA`) ; **absent du registre `aYRN`** (voir la note ci-dessous) ; `[VÉRIFIÉ]` sur le simulateur |
| `/status/activeEvents` | GET | READ | `Default` | `[SOURCE-JS]` l. 76030 (registre, intervalle 10 000 ms) + l. 183060 (variante `?sync=true`, sans en-tête) ; `[VÉRIFIÉ]` sur le simulateur. Champs lus : voir la table d'extraction |
| `/components/PowerMeter/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76035 + l. 75958 (registre `aYRN`, seule voie) ; `[VÉRIFIÉ]` sur le simulateur |
| `/components/BatteryManagementSystem/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76032 + l. 75958 (registre `aYRN`, seule voie) ; `[VÉRIFIÉ]` sur le simulateur |

Note : dans le client officiel, **deux voies** existent — avec en-tête
`Authorization` (module `aYRN`, registre pollé) et sans (module `yTJA`, service
d'état) — mais elles ne se recouvrent pas : `/components/PowerMeter/readable` et
`/components/BatteryManagementSystem/readable` n'apparaissent que dans `aYRN`,
tandis que `/status/version` n'apparaît que dans `yTJA` (vérifié par recherche du
littéral dans le bundle). Le classement `READ` porte sur l'effet, pas sur
l'authentification : aucun des quatre n'a d'effet persistant observé.

## Chemins explicitement écartés

| Chemin | Raison |
|---|---|
| `/config/meter`, `/config/batteries` | Lecture plausible et **désormais établie** : `GET` par `loadData()` (l. 161422), littéraux aux l. 161471 et 161462, l'écriture par `saveData()` en `POST` (l. 161428). La justification antérieure — « littéral et verbe hors de la zone lisible du bundle (module `rCKn`) » — était fausse et est retirée. Ces deux chemins restent **hors** de l'application par choix de périmètre, non par manque de contrat : la présence du compteur et de la batterie est déduite de `Body.Data` des chemins `Cgi`, sans lire ces configurations. |
| `/status/events`, `/status/devices`, `/status/common`, `/components/DataManager/readable` | Inventoriés et lisibles (méthode, arguments et champs sont désormais relevés dans la table d'extraction ci-dessous), mais hors du besoin de la phase 2 : ne pas appeler ce dont on n'a pas besoin réduit la surface. |

## `/commands/` (35 chemins)

| Chemin | Classification | Motif |
|---|---|---|
| `/commands/ActivatePasswords` | MUTATION | Active des mots de passe |
| `/commands/ConfirmEvent` | MUTATION | Acquitte un événement |
| `/commands/ConnectedPowermeter` | READ | Interroge le compteur raccordé |
| `/commands/CreateSDPFile` | MUTATION | Crée un fichier de service |
| `/commands/DCInjectionTest` | DANGEROUS | Test d'injection DC sur l'installation |
| `/commands/GetAutoTestReport` | READ | Lit un rapport |
| `/commands/GetSDPFile` | READ | Lit un fichier de service |
| `/commands/GetServiceState` | READ | Lit l'état de service |
| `/commands/GetUTCTime` | READ | Lit l'heure de l'appareil |
| `/commands/GetUpdateStatus` | READ | Lit l'état de mise à jour |
| `/commands/GridcodeSelection` | DANGEROUS | Sélectionne le code réseau / la protection |
| `/commands/InverterControl` | DANGEROUS | Commande l'onduleur |
| `/commands/InverterRollback` | DANGEROUS | Rétrograde le firmware de l'onduleur |
| `/commands/InverterRollbackStatus` | READ | Lit l'état de rétrogradation |
| `/commands/License` | INDÉTERMINÉ | Sémantique non établie ⇒ traité comme une écriture |
| `/commands/LicenseExchange` | MUTATION | Échange de licence |
| `/commands/LicenseExchangeData` | INDÉTERMINÉ | Idem |
| `/commands/ListGridcodes` | READ | Liste les codes réseau |
| `/commands/ListOhmpilots` | READ | Liste les Ohmpilots |
| `/commands/Login` | MUTATION | Ouvre une session (`GET /commands/Login?user=<username>`) |
| `/commands/Logout` | MUTATION | Ferme la session |
| `/commands/PasswordRecovery` | DANGEROUS | Procédure de récupération de mot de passe |
| `/commands/ReInitInterface` | DANGEROUS | Réinitialise l'interface |
| `/commands/RemoteAccess` | DANGEROUS | Active l'accès distant |
| `/commands/ScanAvailableWlan` | MUTATION | Déclenche un balayage radio |
| `/commands/ServiceFile` | INDÉTERMINÉ | Idem |
| `/commands/SetPassword` | MUTATION | Définit un mot de passe |
| `/commands/SetUTCTime` | MUTATION | Règle l'horloge |
| `/commands/SetWPSActive` | MUTATION | Active WPS |
| `/commands/StandbyRequestState` | MUTATION | Demande un état de veille |
| `/commands/StandbyState` | READ | Lit l'état de veille |
| `/commands/StartSystemService` | DANGEROUS | Démarre un service système |
| `/commands/SupportAccount` | DANGEROUS | Active / termine un compte de support (`resetPassword: true`) |
| `/commands/VerifyVCode` | MUTATION | Vérifie un code |
| `/commands/factoryReset` | DANGEROUS | Réinitialisation d'usine |

## `/config/` (24 chemins)

Tous ces chemins sont des **ressources de configuration**. La lecture en GET a été
observée avec un Digest « Customer » pour quatre d'entre eux
(`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` :
HTTP 200) `[OBSERVÉ-CONVERSATION]` ; toute écriture est une `MUTATION` au
minimum, et `DANGEROUS` dès qu'elle touche au réseau électrique, aux limites de
puissance ou aux interfaces de contrôle.

| Chemin | Classification lecture | Classification écriture |
|---|---|---|
| `/config/batteries` | READ | MUTATION |
| `/config/common` | READ `[OBSERVÉ-CONVERSATION]` 200 | MUTATION |
| `/config/emrs` | READ | MUTATION |
| `/config/gridcode` | READ | **DANGEROUS** |
| `/config/ics` | READ | MUTATION |
| `/config/iomapping` | READ `[OBSERVÉ-CONVERSATION]` 200 | MUTATION |
| `/config/logging` | READ | MUTATION |
| `/config/meter` | READ `[OBSERVÉ-CONVERSATION]` 200 | MUTATION |
| `/config/modbus` | READ `[OBSERVÉ-CONVERSATION]` 200 | **DANGEROUS** (expose une interface de contrôle) |
| `/config/multinetwork` | READ | MUTATION |
| `/config/ohmpilot` | READ | MUTATION |
| `/config/pcc` | READ | **DANGEROUS** (point de couplage réseau) |
| `/config/policy` | READ | MUTATION |
| `/config/powerlimits` | READ `[OBSERVÉ-CONVERSATION]` 200 `{}` | **DANGEROUS** (limites de puissance) |
| `/config/pushservice` | READ | MUTATION |
| `/config/setup/gridcode/general` | READ `[OBSERVÉ-CONVERSATION]` 200 `{}` | **DANGEROUS** |
| `/config/setup/gridcode/gridfeatures` | READ | **DANGEROUS** |
| `/config/setup/gridcode/interfaceprotection` | READ | **DANGEROUS** |
| `/config/setup/gridcode/safety` | READ | **DANGEROUS** |
| `/config/setup/powerunit` | READ `[OBSERVÉ-CONVERSATION]` 200 `{}` | **DANGEROUS** |
| `/config/solar_api` | READ | MUTATION |
| `/config/solarweb` | READ | MUTATION |
| `/config/timeofuse` | READ | MUTATION |
| `/config/wizard` | READ | MUTATION |

## Autres chemins (20)

| Chemin | Classification | Motif |
|---|---|---|
| `/components/BatteryManagementSystem/readable` | READ | **appelé** : présence d'une batterie déduite de `Body.Data` |
| `/components/DataManager/readable` | READ | Lecture d'état |
| `/components/Ohmpilot/readable` | READ | Lecture d'état |
| `/components/PowerMeter/readable` | READ | **appelé** : présence d'un Smart Meter déduite de `Body.Data` |
| `/components/cache/readable` | READ | Lecture d'état |
| `/status/activeEvents` | READ | **appelé** : événements actifs de l'appareil |
| `/status/capabilities` | READ | Lecture des capacités |
| `/status/common` | READ | Lecture d'état ; candidat `[À-TESTER]` pour l'identité de l'appareil |
| `/status/deviceDB` | READ | Lecture de la base d'appareils |
| `/status/devices` | READ | Lecture des appareils |
| `/status/emrs` | READ | Lecture d'état |
| `/status/events` | READ | Lecture d'événements |
| `/status/ioServices` | READ | Lecture d'état |
| `/status/license` | READ | Lecture de licence |
| `/status/license/features` | READ | Lecture des fonctionnalités actives |
| `/status/network` | READ | Lecture réseau |
| `/status/powerflow` | READ | Interrogé toutes les 5 000 ms par le client officiel `[SOURCE-JS]` |
| `/status/powerflowcontroller` | READ | Lecture d'état |
| `/status/registrationData` | READ | Lecture d'état |
| `/status/version` | READ | **appelé** : identité (`serialNumber`, `numberOfPhases`, `devicegroup`) |
| `/update/start` | **DANGEROUS** | Déclenche une mise à jour de firmware |

## Méthode, arguments et DTO par chemin (extraction bundle)

Extraction `[SOURCE-JS]` faite chemin par chemin dans
`references/fronius-original.js` (184 378 lignes) : elle remplace les cellules de
groupe de la matrice ci-dessous pour les chemins `READ`. Comptes annoncés, chacun
reproductible par une commande du bundle :

- **54 chemins** couverts, soit **56 lignes** (les variantes `?sync=true` sont
  listées à part) : 17 sous `/status/`, 5 sous `/components/`, 10 sous
  `/commands/`, 24 sous `/config/` ;
- **46 lignes d'appel** aux quatre enveloppes du client, hors des quatre
  définitions de `JWhm` (l. 46782, 46787, 46791, 46796) et des trois lignes de
  `_refreshValue` (l. 75958, 75960, 75963) — dont **deux sont des délégations
  internes** à `JWhm` (l. 46784, 46793) ⇒ **44 sites d'appel réels** ;
- **7 appels HTTP bruts** hors `JWhm` : six portent sur des chemins de l'appareil
  (téléchargements SDP/Service/License), le septième (l. 54377) est le chargeur
  d'icônes d'Angular Material, **hors périmètre** ;
- **10 `[NON OBSERVÉ]`** (aucun consommateur de charge utile trouvé) et **7
  `schéma`** (consommateur présent, mais aucun champ nommé lu : la charge utile
  est remise telle quelle à un constructeur de formulaire ou à un mappeur).

Règles appliquées :

- **Méthode** portée par l'enveloppe appelée : `callGet$` /
  `callGetWithoutCatch$` ⇒ `GET` + Digest (l. 46782-46790) ; `callPost$` /
  `callPostWithoutCatch$` ⇒ `POST` + Digest (l. 46791-46799) ; `_get$()` du module
  `yTJA` (l. 183019-183021) et les téléchargements de fichiers ⇒ `GET` **sans**
  en-tête, avec pour ces derniers un seul retry manuel sur 401 et
  `responseType: "blob"` (l. 90876-90886).
- **Enveloppe** : `Default` = corps brut ; `Cgi` = `Body.Data` (l. 75958) ;
  `CommandResult` = `resultData` (l. 75960) ; mêmes règles dans `yTJA`
  (l. 183010-183016).
- **Arguments** : les variantes de requête (`?sync=true`, `?status=true`,
  `?doScan=`, `?componentId=`, `?serviceId=`) sont des **arguments du même
  chemin**, pas de nouveaux chemins. Aucun chemin observé n'était absent de la
  classification : rien n'a eu à y être ajouté.
- **DTO** : seuls les champs **réellement lus** par un consommateur sont inscrits.
  `[NON OBSERVÉ]` signifie « aucun consommateur de charge utile trouvé », jamais
  « charge utile vide » ; aucune forme n'est présumée.
- **Rôle déclaré** : condition d'affichage du client, **jamais** une permission
  serveur (voir `docs/UNKNOWNS.md` #6). « aucun » = le client n'ajoute aucune
  option de rôle et prend le compte du groupe de requêtes (l. 46807-46813).

### `/status/*` : voie pollée `aYRN` et service d'état `yTJA`

Les entrées du registre pollé `aYRN` (l. 76027-76045) sont lues par `callGet$`
(l. 75963) ⇒ `GET` + Digest ; le 2ᵉ argument (`5e3`, `1e4`, `15e3`) est
l'intervalle de rafraîchissement en ms. Les entrées du service d'état `yTJA` sont
lues par `httpClient.get(this.url)` (l. 183020) ⇒ `GET` **sans** en-tête.

| Chemin | Méthode | Arguments | Enveloppe | DTO (champs lus) | Rôle déclaré | Preuve (lignes) |
|---|---|---|---|---|---|---|
| `/status/devices` | `GET` | aucun | `Default` | tableau d'objets : `type`, `id`, `model`, `serial`, `name`, `additionalLabel`, `status` | aucun | l. 76027 (registre, 5 000 ms) ; l. 75963 (enveloppe) ; consommateurs l. 129075, 129089-129097, 129112, 129143-129146, 131216, 131225 |
| `/status/network` | `GET` | aucun | `Default` | `eth0.network.status`, `eth0.ip.ip`, `ip.dns`, `ip.mask`, `ip.gateway`, `wlan0.network.interface_mode`, `wlan0.ip.ip`, `uap0.network.status`, `uap0.network.interface_mode` | aucun | l. 76028 (registre, 5 000 ms) **et** l. 183052 (voie `yTJA`, sans en-tête) ; consommateurs l. 127383, 128678-128683, 143035, 143061-143066 |
| `/status/emrs` | `GET` | aucun | `Default` | objet à clés dynamiques (`Object.entries`) ; les clés préfixées `_` sont écartées | aucun | l. 76029 (registre, 5 000 ms) ; consommateur l. 134656 |
| `/status/activeEvents` | `GET` | aucun | `Default` | tableau d'objets : `severity`, `viewer`, `uuid`, `timestamp` (s), `activeUntil` (s), `category`, `confirmable`, `label`, `sourceID`, `prefix`, `eventID` | aucun | l. 76030 (registre, 10 000 ms) ; consommateurs l. 135689-135701, 135654, 164699, 71151, 154444 |
| `/status/events` | `GET` | aucun | `Default` | **les mêmes champs** que `/status/activeEvents` (même mapper) | aucun | l. 76031 (registre, 10 000 ms) ; consommateur l. 135651 |
| `/status/activeEvents?sync=true` | `GET` | `sync=true` | `Default` | `[NON OBSERVÉ]` — émission utilisée comme simple déclencheur | aucun (requête nue, sans en-tête) | l. 183060 (voie `yTJA`) ; seul site d'appel l. 135633 |
| `/status/events?sync=true` | `GET` | `sync=true` | `Default` | `[NON OBSERVÉ]` — idem | aucun (requête nue, sans en-tête) | l. 183061 (voie `yTJA`) ; seul site d'appel l. 135633 |
| `/status/powerflow` | `GET` | aucun | `Default` | `site.P_PV`, `site.P_Grid`, `site.P_Load`, `site.P_Akku`, `site.E_Day`, `site.E_Year`, `site.E_Total`, `inverters[<id>].SOC`, `inverters[<id>].BatMode`, `Smartloads.Ohmpilots[<id>].P_AC_Total` | aucun | l. 76036 (registre, 5 000 ms) ; consommateurs l. 129034, 129043-129063, 136261 |
| `/status/common` | `GET` | aucun | `Default` | `licenseExchangeData`, `serviceState`, `updateRunning`, `networkState.connectedViaAP` | aucun | l. 76041 (registre, 5 000 ms) **et** l. 183049 (voie `yTJA`, sans en-tête) ; consommateurs l. 25824, 45241, 127386, 135196, 143145 |
| `/status/version` | `GET` | aucun | `Default` | `serialNumber`, `hardwareId`, `devicename`, `articleNumber`, `swrevisions.GEN24`, `hwrevisions.FAMILYID`, `numberOfPhases`, `devicegroup[0]` | aucun (voie sans en-tête) | l. 183050 (voie `yTJA` — **ce chemin n'est pas dans le registre `aYRN`**) ; consommateurs l. 45243, 126568, 127380, 128813-128814, 128842, 128881-128882, 135137, 143048-143049, 183034-183035 |
| `/status/registrationData` | `GET` | aucun | `Default` | `[NON OBSERVÉ]` — charge traitée comme un bloc JSON (`JSON.stringify` vers un `<textarea>`), aucun champ nommé lu | aucun (voie sans en-tête) | l. 183051 ; consommateur l. 143036, affichage l. 143144 |
| `/status/capabilities` | `GET` | aucun | `Default` | `gpios` | aucun (voie sans en-tête) | l. 183053 (accesseur `ioCapabilities`) ; consommateurs l. 126565, 126582 |
| `/status/ioServices` | `GET` | aucun | `Default` | `services[].name`, `services[].label` | aucun (voie sans en-tête) | l. 183054 ; consommateurs l. 126566, 126576-126578 |
| `/status/powerflowcontroller` | `GET` | aucun | `Default` | `[NON OBSERVÉ]` — accesseur `powerflowcontroller`, aucun consommateur de charge utile | aucun (voie sans en-tête) | l. 183055 |
| `/status/deviceDB` | `GET` | aucun | `Default` | `Inverters[<id>].NominalPower` | aucun (voie sans en-tête) | l. 183056 ; consommateur l. 129072 |
| `/status/license` | `GET` | aucun | `Default` | `serialNumber`, `powerValue`, `sequenceNumber` | aucun (voie sans en-tête) | l. 183058 ; consommateur l. 128785 |
| `/status/license/features` | `GET` | aucun | `Default` | objet à clés dynamiques (`Object.keys` / `t[clé]`), aucune clé nommée | aucun (voie sans en-tête) | l. 183059 ; consommateur l. 128793, 128797 |

### Composants `Cgi`

Enveloppe `Cgi` ⇒ la charge utile utile est `Body.Data` (l. 75958). Dans
`Body.Data`, les clés de premier niveau sont les **identifiants de composant** ;
le client les parcourt (`Object.keys`, l. 129130) et lit `[id].channels`.

| Chemin | Méthode | Arguments | Enveloppe | DTO (champs lus) | Rôle déclaré | Preuve (lignes) |
|---|---|---|---|---|---|---|
| `/components/cache/readable` | `GET` | aucun | `Cgi` (`Body.Data`) | `[id].channels` (43 signaux, voir ci-dessous), `[id].attributes["inverter.devicetype"]` | aucun | l. 76033 (registre, 5 000 ms) ; l. 75958 (enveloppe) ; consommateurs l. 129070, 129091, 129123, 129130, 128972, 128976 |
| `/components/BatteryManagementSystem/readable` | `GET` | aucun | `Cgi` (`Body.Data`) | `[id].channels` (10 signaux), clés = identifiants de composant | aucun | l. 76032 (registre, 5 000 ms) ; l. 75958 ; consommateurs l. 129098, 129123, 129130, 128986 |
| `/components/Ohmpilot/readable` | `GET` | aucun | `Cgi` (`Body.Data`) | `[id].channels` (4 signaux) | aucun | l. 76034 (registre, 5 000 ms) ; l. 75958 ; consommateurs l. 129105, 129002 |
| `/components/PowerMeter/readable` | `GET` | aucun | `Cgi` (`Body.Data`) | `[id].channels` (23 signaux), `[id].attributes.addr`, `[id].attributes["[ENABLE]"]` | aucun | l. 76035 (registre, 5 000 ms) ; l. 75958 ; consommateurs l. 129114, 129011, 129015, 130974-130983 |
| `/components/DataManager/readable` | `GET` | aucun | `Cgi` (`Body.Data`) | `[id].attributes.deviceTreeInfo` (JSON à décoder), puis `modbus.extras.defaults` dans l'arbre décodé | aucun (voie sans en-tête) | l. 183057 ; l. 183011 (enveloppe `Cgi`) ; consommateurs l. 132248-132249, 132251-132257 |

#### Signaux lus dans `channels`

Un signal absent ⇒ `undefined`, jamais `0` : les mappeurs construisent chaque
entrée à partir d'un accès `t.<SIGNAL>` (l. 128972, 128976, 128986, 129002,
129011, 129015). Les unités déclarées par le client sont celles du bundle.

| Composant | Signaux lus | Preuve |
|---|---|---|
| onduleur (`/components/cache/readable`) | `ACBRIDGE_POWERACTIVE_SUM_MEAN_F32`, `FEEDINPOINT_FREQUENCY_MEAN_F32`, `PV_POWERACTIVE_MEAN_01/02/03_F32`, `PV_VOLTAGE_MEAN_01/02/03_F32`, `PV_CURRENT_MEAN_01/02/03_F32`, `BAT_POWERACTIVE_MEAN_F32`, `BAT_VOLTAGE_OUTER_MEAN_01_F32`, `BAT_CURRENT_MEAN_F32`, `FEEDINPOINT_VOLTAGE_MEAN_01/02/03/12/23/31_F32`, `ACBRIDGE_CURRENT_ACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERAPPARENT_MEAN_01/02/03_F32`, `ACBRIDGE_POWERAPPARENT_SUM_MEAN_F32`, `ACBRIDGE_POWERREACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERREACTIVE_SUM_MEAN_F32`, `FEEDINPOINT_ANGLE_MEAN_12/13/23_F32`, `ISO_RESISTANCE_MEAN_F32`, `MODULE_TEMPERATURE_MEAN_01/03/04_F32`, `DEVICE_TEMPERATURE_AMBIENTEMEAN_F32`, `DCLINK_VOLTAGE_MEAN_F32` | l. 128972 (23 signaux), l. 128976 (20 signaux) |
| batterie (`/components/BatteryManagementSystem/readable`) | `LEGACY_CURRENT_DC_CHARGE_TARGET_F64`, `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16`, `BAT_TEMPERATURE_CELL_F64`, `BAT_TEMPERATURE_CELL_MIN_F64`, `BAT_TEMPERATURE_CELL_MAX_F64`, `DCLINK_VOLTAGE_MEAN_F32`, `POWERMANAGER_VOLTAGE_LINKMIN_F32`, `BAT_VOLTAGE_DC_CELL_MAX_F64`, `BAT_ENERGYACTIVE_ESTIMATION_MAX_CAPACITY_F64`, `BAT_ENERGYACTIVE_MAX_CAPACITY_F64` | l. 128986 (10 signaux ; les deux énergies sont converties `Ws → Wh`, l. 128989) |
| Ohmpilot (`/components/Ohmpilot/readable`) | `SMARTMETER_ENERGYACTIVE_CONSUMED_SUM_F64`, `OHMPILOT_POWERACTIVE_DESIRED_F64`, `ACBRIDGE_POWERACTIVE_SUM_MEAN_F32`, `LEGACY_TEMPERATURE_MEAN_00_F64` | l. 129002 (4 signaux) |
| Smart Meter (`/components/PowerMeter/readable`) | `SMARTMETER_POWERACTIVE_MEAN_SUM_F64`, `SMARTMETER_FREQUENCY_MEAN_F64`, `SMARTMETER_POWERACTIVE_01/02/03_F64`, `SMARTMETER_VOLTAGE_MEAN_01/02/03_F64`, `SMARTMETER_CURRENT_MEAN_01/02/03_F64`, `SMARTMETER_POWERAPPARENT_MEAN_SUM_F64`, `SMARTMETER_POWERAPPARENT_01/02/03_F64`, `SMARTMETER_POWERREACTIVE_MEAN_SUM_F64`, `SMARTMETER_POWERREACTIVE_01/02/03_F64`, `SMARTMETER_FACTOR_POWER_SUM_F64`, `SMARTMETER_FACTOR_POWER_01/02/03_F64` | l. 129011 (11 signaux), l. 129015 (12 signaux) |

### `/commands/*` en lecture : sites d'appel hors registre

| Chemin | Méthode | Arguments | Enveloppe | DTO (champs lus) | Rôle déclaré | Preuve (lignes) |
|---|---|---|---|---|---|---|
| `/commands/GetUTCTime` | `GET` | aucun | `CommandResult` | `epoch` (secondes) | `user: "customer"` (registre, l. 76039) | l. 76037-76040 (registre, 15 000 ms) ; l. 90579 (site direct) ; consommateurs l. 128622, 128767 |
| `/commands/GetServiceState?serviceId=<id>` | `GET` | `serviceId` (entier, `T_SystemServiceId` ; `GridlimitsAutotest` = 21, l. 116025) | `CommandResult` | `resultData.state` (repli `BLOCKED` si `success` est faux) | aucun | l. 76043 (registre, 5 000 ms) ; l. 90935, 90939 (site direct) ; consommateur l. 136831 |
| `/commands/StandbyState` | `GET` | aucun | `CommandResult` | `[NON OBSERVÉ]` — charge utile stockée telle quelle (accesseur `standbyState$`, l. 135484), aucun champ lu | aucun | l. 76045 (registre, 5 000 ms) ; consommateur l. 135524 |
| `/commands/InverterRollbackStatus` | `GET` | aucun | `CommandResult` | `partitionNumber`, `available` (formes déclarées par l'objet de repli du client) | aucun | l. 21334-21336 (site, module `966A`) ; consommateurs l. 135220, 135259, 135325, 135332 |
| `/commands/GetUpdateStatus` | `GET` | aucun | `CommandResult` | `status`, `lastActiveStatus`, `rebootPending`, `nsteps`, `cur_step`, `cur_percent` | aucun | l. 90610 (site) ; consommateurs l. 135220, 135249, 135315, 135435, 135446 |
| `/commands/ListGridcodes` | `GET` | aucun | `CommandResult` | `primary[<id>].group` (chaîne ou tableau), `primary[<id>].title` | aucun | l. 90730 (site) ; consommateurs l. 128640-128649 |
| `/commands/ListOhmpilots` | `GET` | aucun | `CommandResult` (repli `[]`) | tableau d'objets : `serial`, `masterId`, `label`, `ip`, `hardware`, `software` | aucun | l. 90921 (site) ; consommateurs l. 131187-131202, 131304-131311 |
| `/commands/GetAutoTestReport?testName=<nom>` | `GET` | `testName` (chaîne ; `GridlimitsAutotest` → `Italiantest`, l. 90968) | `CommandResult` (si `success`) | `Common.TESTTIMESTAMP`, puis toutes les clés dynamiquement, chacune avec sa métadonnée `_<clé>_meta` (`unit`, `displayType`, `valueType`, `displayIndex`) | aucun | l. 90964, 90976 (site) ; consommateurs l. 136812, 136913-136956 |
| `/commands/ConnectedPowermeter?componentId=<id>` | **`POST`** | `componentId` (dans l'URL) ; corps = charge utile du DAO appelant | `CommandResult` | `[NON OBSERVÉ]` — la charge utile n'est jamais lue (`take(1)` seul) | aucun | l. 91025 (site, POST) ; appel l. 130764 ; `componentId` provient de `[id].attributes` du polling (l. 130982) |
| `/commands/GetSDPFile` | `GET` (blob) | aucun | `Default` (corps binaire ; le client construit `{fileName, data}`) | `[NON OBSERVÉ]` — `{fileName, data}` n'est jamais dépaqueté (accesseur `cryData$`, l. 136664) | aucun | l. 90876-90896 (dont deux `_httpClient.get` bruts l. 90876, 90882) ; consommateur l. 136726-136727 |

Trois de ces dix chemins sont aussi lus par le **registre pollé** (avec en-tête
Digest), avec leur intervalle : `/commands/GetUTCTime` (15 000 ms, l. 76037),
`/commands/GetServiceState?serviceId=` (5 000 ms, l. 76043) et
`/commands/StandbyState` (5 000 ms, l. 76045) ; les sept autres n'existent que
sous forme d'appel direct. `ConnectedPowermeter` est le seul chemin classé `READ`
dont le client officiel émet une **`POST`**.

### `/config/*` : DAO `rCKn` (`loadData` / `saveData`)

Le module `rCKn` construit un DAO par chemin (`new h(t, "<chemin>", this)`,
l. 161462-161485) ; chacun expose `loadData()` (`GET` + Digest, l. 161422) et
`saveData()` (`POST` + Digest, l. 161428), tous deux avec le compte connecté
(`type: fallback`, l. 46809-46810) — **aucun rôle nommé**. L'enveloppe est
`Default` : le corps est consommé à la racine (le cache reçoit la réponse telle
quelle l. 161382, et `execute$` lit `writeSuccess` à la racine l. 161405-161406).
Les champs ci-dessous sont ceux **lus** par un consommateur de l'application.

| Chemin | Méthode | Arguments | Enveloppe | DTO (champs lus) | Rôle déclaré | Preuve (lignes) |
|---|---|---|---|---|---|---|
| `/config/common` | `GET` / `POST` | aucun | `Default` | `systemName`, `timezone`, `wattPeakValues[].id`, `wattPeakValues[].wattPeak` | compte connecté (`fallback`) | l. 161463 (déclaration), 161422, 161428 ; consommateurs l. 127249, 128765-128766, 129031, 129426-129427 |
| `/config/emrs` | `GET` / `POST` | aucun | `Default` | `rules.pin1..pin4`, `rules._pinN_meta`, `_rules_meta`, `priorities` | compte connecté | l. 161464 ; consommateurs l. 134420-134430, 134569 |
| `/config/iomapping` | `GET` / `POST` | aucun | `Default` | `features[].label`, `features[].ios[].service`, `features[].ios[].PinId`, `gpios` (`features[].enable` n'est qu'**écrit**) | compte connecté | l. 161467 ; consommateurs l. 126572-126582, 132901-132902 |
| `/config/meter` | `GET` / `POST` | aucun | `Default` | `primary.available`, `primary.isLoadMeter`, reste remis au constructeur de formulaire | compte connecté | l. 161471 ; consommateurs l. 130410, 130663, 131596, 132233, 134666 |
| `/config/batteries` | `GET` / `POST` | aucun | `Default` | `HYB_BM_PACMIN`, `_HYB_BM_PACMIN_meta.validators`, `_BAT_TYPE_meta.options`, `BAT_ENABLED` | compte connecté | l. 161462 ; consommateurs l. 129879-129881, 129945-129946, 131596 |
| `/config/modbus` | `GET` / `POST` | aucun | `Default` | `master.rtuif[].if`, `slave.rtuif[].if` | compte connecté | l. 161472 ; consommateurs l. 132444-132445, 132735-132741 |
| `/config/pcc` | `GET` / `POST` | aucun | `Default` | `dno`, `as4777` | compte connecté | l. 161475 ; consommateurs l. 132897, 134148 |
| `/config/policy` | `GET` / `POST` | aucun | `Default` | `newPolicyVersion.dmweb`, `policyTerms[].name`, `policyTerms[].label`, `policyTerms[].date` | compte connecté | l. 161476 ; consommateurs l. 127763-127772 |
| `/config/multinetwork` | `GET` / `POST` | aucun | `Default` | `eth0.ipConfig.hostname`, `eth0.ipConfig.dhcpEnabled`, `wlan0.ipConfig.dhcpEnabled`, `inetConnection.selInetIface` | compte connecté | l. 161473 ; consommateurs l. 127250-127251, 127255-127256 |
| `/config/ohmpilot` | `GET` / `POST` | aucun | `Default` | `selected` | compte connecté | l. 161474 ; consommateurs l. 131234, 135825, 135829 |
| `/config/pushservice` | `GET` / `POST` | aucun | `Default` | `Hosts[].Enable`, `Hosts[].Format.key`, `Hosts[].Format.value.Protocol`, `Hosts[].Format.value.Interval`, `Hosts[].Name`, `Hosts[].Host.Addr`, `Hosts[].Host.Remote`, `Hosts[].Auth.Use`, `Hosts[].Proxy.Use`, `_Hosts_Element_meta._Format_Element_meta` | compte connecté | l. 161477 ; consommateurs l. 134894, 134958-134962, 134964, 134968, 134973, 134986 |
| `/config/wizard` | `GET` / `POST` | aucun | `Default` | `active`, `stateNetwork`, `stateService`, `stateSolarweb` | compte connecté | l. 161478 ; consommateurs l. 81353, 135155, 143035, 143047-143051 |
| `/config/timeofuse` | `GET` / `POST` | aucun | `Default` | `timeofuse`, `_timeofuse_meta.entryBaseType._ScheduleType_meta.options` | compte connecté | l. 161479 ; consommateurs l. 137879, 137890 |
| `/config/setup/powerunit` | `GET` / `POST` | aucun | `Default` | `mppt`, `mppt.PV_MODE_COMBINE_U16`, `mppt1.PV_MODE_MPP_01_U16`, `mppt2.PV_MODE_MPP_02_U16`, `mppt3.PV_MODE_MPP_03_U16` | compte connecté | l. 161485 ; consommateurs l. 129313, 129322, 129430, 135527 |
| `/config/powerlimits` | `GET` / `POST` | aucun | `Default` | `schéma` — remis à `_powerLimitsMapper.mapToDestination` ; seuls `exportLimits.activePower.mode` et sa méta sont ensuite modifiés côté formulaire | compte connecté | l. 161465 ; consommateurs l. 130417, 130435, 130447-130454 |
| `/config/ics` | `GET` / `POST` | aucun | `Default` | `schéma` — remis à `Ge.a.build(t, this._form)` | compte connecté | l. 161466 ; consommateurs l. 133806, 133810 |
| `/config/setup/gridcode/general` | `GET` / `POST` | aucun | `Default` | `schéma` — remis au constructeur de formulaire, puis à `mapper.getMappedDataSets(…, "CHANNELS")` | compte connecté | l. 161481 ; consommateurs l. 133274, 128855-128859 |
| `/config/setup/gridcode/safety` | `GET` / `POST` | aucun | `Default` | `schéma` — idem | compte connecté | l. 161482 ; consommateur l. 133306 |
| `/config/setup/gridcode/interfaceprotection` | `GET` / `POST` | aucun | `Default` | `schéma` — idem | compte connecté | l. 161483 ; consommateur l. 133338 |
| `/config/setup/gridcode/gridfeatures` | `GET` / `POST` | aucun | `Default` | `schéma` — idem | compte connecté | l. 161484 ; consommateurs l. 133370, 128855-128865 |
| `/config/solar_api` | `GET` / `POST` | aucun | `Default` | `schéma` — remis au constructeur `addSourceStream(...).build()` | compte connecté | l. 161470 ; consommateur l. 155339 |
| `/config/logging` | `GET` / `POST` | aucun | `Default` | `[NON OBSERVÉ]` — accesseur `config.logging` déclaré, aucun consommateur | compte connecté | l. 161468 |
| `/config/solarweb` | `GET` / `POST` | aucun | `Default` | `[NON OBSERVÉ]` — accesseur `config.solarweb` déclaré, aucun consommateur | compte connecté | l. 161469 |
| `/config/gridcode` | `GET` / `POST` | aucun | `Default` | `[NON OBSERVÉ]` — accesseur `config.allGridSettings` déclaré, aucun consommateur | compte connecté | l. 161480 |

### Sites balayés non retenus (chemins non `READ`)

Sur les 44 sites d'appel, 34 portent sur des chemins que ce document classe
`MUTATION`, `DANGEROUS` ou `INDÉTERMINÉ` : ils sont inventoriés ici pour être
**évités**, avec le verbe réellement émis par le client officiel.

| Chemin | Verbe(s) observé(s) | Classification | Preuve (lignes) |
|---|---|---|---|
| `/commands/SetPassword` | `POST` (`?user=`) | MUTATION | l. 90512 |
| `/commands/ActivatePasswords` | `GET` | MUTATION | l. 90529 |
| `/commands/Login` | `GET` (`?user=`, `passwordUser`) | MUTATION | l. 90546 |
| `/commands/Logout` | `GET` | MUTATION | l. 90564 |
| `/commands/ReInitInterface` | `GET` (`?iface=`) | DANGEROUS | l. 90596 |
| `/commands/ScanAvailableWlan` | `GET` (`?doScan=`) | MUTATION | l. 90624 |
| `/commands/StandbyRequestState` | `GET` / `POST` | MUTATION | l. 90638, 90641 |
| `/commands/GridcodeSelection` | `GET` / `POST` | DANGEROUS | l. 90752, 90760 |
| `/commands/SetUTCTime` | `POST` | MUTATION | l. 90784 |
| `/commands/factoryReset` | `POST` | DANGEROUS | l. 90800 |
| `/commands/ConfirmEvent` | `POST` | MUTATION | l. 90814 |
| `/commands/InverterControl` | `POST` | DANGEROUS | l. 90828 |
| `/commands/DCInjectionTest` | `POST` | DANGEROUS | l. 90842 |
| `/commands/CreateSDPFile` | `GET`, `GET ?status=true` | MUTATION | l. 90857, 90860 |
| `/commands/SetWPSActive` | `GET` (`?set=`) | MUTATION | l. 90906 |
| `/commands/StartSystemService?serviceId=` | `GET` | DANGEROUS | l. 90949 |
| `/commands/RemoteAccess` | `GET` / `POST` | DANGEROUS | l. 90987, 90992 |
| `/commands/InverterRollback` | `POST` | DANGEROUS | l. 91008 |
| `/commands/PasswordRecovery` | `GET` / `POST` (`?user=`) | DANGEROUS | l. 91039, 91045 |
| `/commands/VerifyVCode` | `GET` (`?vCode=`) | MUTATION | l. 91066 |
| `/commands/LicenseExchange` | `POST` (`?exchangeMode=`), `GET`, `GET ?quit=true` | MUTATION | l. 91078, 91081, 91084 |
| `/commands/LicenseExchangeData` | `POST` / `GET` | INDÉTERMINÉ | l. 91093, 91096 |
| `/commands/ServiceFile` | `GET ?status=`, `POST` (XHR), `GET ?serviceFile=`, `GET ?acknowledgement=` (blob) | INDÉTERMINÉ | l. 91160, 91152, 91157, 91166, 91172 |
| `/commands/SupportAccount` | `POST`, `POST {resetPassword: true}` | DANGEROUS | l. 91193, 91196 |
| `/commands/License` | `GET ?status=`, `GET ?acknowledgement=`, `GET ?serviceFile=` (blob) | INDÉTERMINÉ (traité comme une écriture) | l. 120739, 120745, 120751 ; URL l. 120675-120677 |
| `/update/start` | `POST` (XHR) | DANGEROUS | l. 90677, 90712 |

Hors appareil : `_httpClient.get` de l. 54377 charge les icônes SVG d'Angular
Material — ce n'est pas un chemin Fronius.

### Rôles déclarés (client uniquement)

- Le registre pollé n'attache une option de rôle qu'à **une** entrée :
  `/commands/GetUTCTime` avec `{loginDao: …, user: "customer"}` (l. 76037-76040).
  Cette option alimente `_minimumRequiredUserOption` (l. 75909) et la condition
  `_isUserAllowed` (l. 75972-75984) : `customer` passe pour
  `customer | technician | support`, `technician` et `support` passent pour
  `technician | support`. C'est une **condition d'affichage**, pas une
  autorisation serveur.
- Le choix du compte envoyé est distinct du rôle :
  `_getUserAndPass` (l. 46807-46813) retourne soit le compte du groupe de requêtes
  (`fallback`, l. 46809), soit un couple `user:password` explicite
  (`passwordUser`, l. 46811). Les seuls sites directs observés en `fallback` sont
  le DAO `/config/*` (l. 161422, 161428) et les commandes `VerifyVCode`,
  `PasswordRecovery`, `SetPassword`, `ActivatePasswords` ; `Login` est le seul
  site en `passwordUser`.
- Les mappeurs de signaux déclarent eux aussi des rôles d'affichage :
  `_getRequiredUsersCustomer` (l. 128959-128960) =
  `technician | support | utility | unauthorized | user | customer`, et
  `_getRequiredUsersTechnician` (l. 128962-128963) =
  `technician | support | utility` (l. 128971, 128975, 128985, 129001, 129010,
  129014).
- **Aucun** de ces rôles n'est une permission serveur : le bundle ne contient
  aucune matrice d'autorisation, seulement des conditions d'affichage. L'inconnue
  #6 de `docs/UNKNOWNS.md` reste ouverte.

## Matrice endpoint / méthode / paramètres / rôle déclaré / rôle testé / effet de bord

« Rôle déclaré » provient du **client** : les rôles cumulés (`RoleEverybody`,
`RoleCustomer`, `RoleTechnician`, `RoleSupport`, `SetupAccessRole01/02`) sont
déclarés dans le bundle et conditionnent l'affichage, pas l'autorisation
serveur. Aucun rôle serveur n'est prouvé à ce stade : le renseigner exige
l'API interne (phase 2) et un compte de test, ce qui est hors périmètre ici.
« Rôle testé » vaut donc « aucun » partout sauf mention contraire : rien n'a été
rejoué sur le matériel.

| Chemin | Méthode | Paramètres | Rôle déclaré | Rôle testé | Effet de bord |
|---|---|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | `GET` (imposé par la garde) | aucun | aucun (pas d'authentification connue) `[À-TESTER]` | **simulateur** : 200 sans en-tête ; **matériel** : non exécuté | aucun : lecture seule |
| `/status/version` | `GET` (imposé par la garde) | aucun | Digest « Customer » `[SOURCE-JS]` — également lu sans en-tête par le module `yTJA` | **simulateur** : 401 sans en-tête puis 200 après Digest ; `curl --digest` accepté (client tiers) | aucun : lecture d'identité |
| `/status/activeEvents` | `GET` (imposé par la garde) | aucun | idem | idem | aucun : lecture d'événements |
| `/components/PowerMeter/readable` | `GET` (imposé par la garde) | aucun | idem | idem | aucun : lecture d'état |
| `/components/BatteryManagementSystem/readable` | `GET` (imposé par la garde) | aucun | idem | idem | aucun : lecture d'état |
| **Chemins `READ` désormais extraits** : `/status/*` (17 lignes), `/components/*/readable` (5), `/commands/*` en lecture (10), `/config/*` en lecture (24) | `GET`, sauf `POST` pour `/commands/ConnectedPowermeter` et pour l'écriture `/config/*` | par chemin : voir « Méthode, arguments et DTO par chemin » | rôles d'affichage Angular `[SOURCE-JS]`, non prouvés à ce stade ; `user: "customer"` déclaré sur le seul `/commands/GetUTCTime` | **aucun** | aucun attendu (lecture) ; `[OBSERVÉ-CONVERSATION]` : `GET` 200 sur les quatre chemins `/config/*` déjà essayés |
| `/config/*` en écriture (24 chemins) | `POST` observé (l. 161428) | par chemin : voir la table d'extraction | compte connecté (`fallback`, l. 161428) — rôle serveur inconnu | **aucun** | modification de configuration ; `DANGEROUS` pour `gridcode`, `pcc`, `powerlimits`, `modbus`, `setup/*` |
| `/commands/*` en mutation (17 chemins : `ActivatePasswords`, `ConfirmEvent`, `CreateSDPFile`, `LicenseExchange`, `Login`, `Logout`, `ScanAvailableWlan`, `SetPassword`, `SetUTCTime`, `SetWPSActive`, `StandbyRequestState`, `VerifyVCode`, …) | `GET`/`POST` selon la commande `[SOURCE-JS]` | variable | variable selon la commande | **aucun** | modification d'état, de session, de fichier ou de préférence |
| `/commands/*` dangereux (10 chemins : `DCInjectionTest`, `GridcodeSelection`, `InverterControl`, `InverterRollback`, `PasswordRecovery`, `ReInitInterface`, `RemoteAccess`, `StartSystemService`, `SupportAccount`, `factoryReset`) | `GET`/`POST` observés (voir la table d'extraction) | variable | inconnu | **aucun** | coupure de production, firmware, protection réseau électrique, accès ou effacement |
| `/commands/License`, `LicenseExchangeData`, `ServiceFile` | `GET`/`POST` observés (`?status=`, `?acknowledgement=`, `?serviceFile=`) | variable | inconnu | **aucun** | indéterminé ⇒ traité comme une écriture |
| `/update/start` | `POST` observé (XHR, l. 90677) | corps = fichier | inconnu | **aucun** | mise à jour de firmware de l'appareil |

Ce que cette matrice établit : **cinq chemins sont appelés**, tous en `GET`, sans
paramètre, sans effet de bord observé, et sans aucune entrée `MUTATION` ou
`DANGEROUS` dans la liste blanche exécutable. Les 79 autres chemins recensés dans
ce document restent inventoriés pour être explicitement évités. Les cellules
« inconnu » ne sont pas des autorisations implicites : ce sont des inconnues
(voir `docs/UNKNOWNS.md`).

Cette extraction **n'ajoute rien à la liste blanche** : elle remplace des
inconnues par des faits observés dans le client officiel, et elle montre que le
client officiel émet bien des `POST` — sur une trentaine de chemins hors liste
blanche et même sur un chemin classé `READ`
(`/commands/ConnectedPowermeter?componentId=`, l. 91025). La règle « aucune
commande non-`GET` » ci-dessous porte sur **l'application**, pas sur le client du
fabricant : c'est elle qui reste la seule autorisation exécutable.

## Règle applicable

Aucune commande `POST`, `PUT`, `PATCH` ou `DELETE` n'est émise vers l'onduleur.
Cette règle est structurelle (`internal/fronius/readonly.go`), pas déclarative :
une requête non-GET est rejetée avant d'atteindre le transport, et la vérification
`grep -rn 'MethodPost\|MethodPut\|MethodDelete\|MethodPatch' internal cmd apps/web/src`
ne doit rien trouver hors du message d'erreur de la garde et de ses tests.

Les endpoints `READ` marqués `[À-TESTER]` ne seront appelés qu'après une revue
explicite et une confirmation de l'utilisateur. L'ajout d'une entrée à
`internal/fronius/internalapi/allowlist.go` est une décision de ce type : un
chemin non listé n'est pas appelable, quel que soit le code appelant.
