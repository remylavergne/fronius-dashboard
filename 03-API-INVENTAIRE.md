# Inventaire statique des chemins présents dans le bundle

> Extraction par expression régulière du JavaScript fourni. **Inventaire indicatif** : un chemin peut être utilisé en lecture, en écriture ou uniquement dans une fonctionnalité non disponible. Les chemins peuvent être incomplets lorsqu’ils sont construits dynamiquement ; la méthode et les arguments de chaque chemin réellement interrogé sont désormais relevés depuis le registre pollé et les sites d’appel du bundle, avec leurs numéros de ligne, dans `docs/ENDPOINTS.md`. Les permissions, elles, ne sont jamais inférées.

## Endpoints `/commands/` (35)

- `/commands/ActivatePasswords`
- `/commands/ConfirmEvent`
- `/commands/ConnectedPowermeter`
- `/commands/CreateSDPFile`
- `/commands/DCInjectionTest`
- `/commands/GetAutoTestReport`
- `/commands/GetSDPFile`
- `/commands/GetServiceState`
- `/commands/GetUTCTime`
- `/commands/GetUpdateStatus`
- `/commands/GridcodeSelection`
- `/commands/InverterControl`
- `/commands/InverterRollback`
- `/commands/InverterRollbackStatus`
- `/commands/License`
- `/commands/LicenseExchange`
- `/commands/LicenseExchangeData`
- `/commands/ListGridcodes`
- `/commands/ListOhmpilots`
- `/commands/Login`
- `/commands/Logout`
- `/commands/PasswordRecovery`
- `/commands/ReInitInterface`
- `/commands/RemoteAccess`
- `/commands/ScanAvailableWlan`
- `/commands/ServiceFile`
- `/commands/SetPassword`
- `/commands/SetUTCTime`
- `/commands/SetWPSActive`
- `/commands/StandbyRequestState`
- `/commands/StandbyState`
- `/commands/StartSystemService`
- `/commands/SupportAccount`
- `/commands/VerifyVCode`
- `/commands/factoryReset`

## Endpoints `/config/` (24)

- `/config/batteries`
- `/config/common`
- `/config/emrs`
- `/config/gridcode`
- `/config/ics`
- `/config/iomapping`
- `/config/logging`
- `/config/meter`
- `/config/modbus`
- `/config/multinetwork`
- `/config/ohmpilot`
- `/config/pcc`
- `/config/policy`
- `/config/powerlimits`
- `/config/pushservice`
- `/config/setup/gridcode/general`
- `/config/setup/gridcode/gridfeatures`
- `/config/setup/gridcode/interfaceprotection`
- `/config/setup/gridcode/safety`
- `/config/setup/powerunit`
- `/config/solar_api`
- `/config/solarweb`
- `/config/timeofuse`
- `/config/wizard`

## Autres chemins repérés

- `/components/BatteryManagementSystem/readable`
- `/components/DataManager/readable`
- `/components/Ohmpilot/readable`
- `/components/PowerMeter/readable`
- `/components/cache/readable`
- `/status/activeEvents`
- `/status/capabilities`
- `/status/common`
- `/status/deviceDB`
- `/status/devices`
- `/status/emrs`
- `/status/events`
- `/status/ioServices`
- `/status/license`
- `/status/license/features`
- `/status/network`
- `/status/powerflow`
- `/status/powerflowcontroller`
- `/status/registrationData`
- `/status/version`
- `/update/start`

## API confirmées / essais historiques

| Chemin                                        | Méthode  | Auth                | Résultat       | Source                 |
| --------------------------------------------- | -------- | ------------------- | -------------- | ---------------------- |
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET      | à documenter        | répond         | conversation           |
| `/config/common`                              | GET      | Customer Digest     | 200            | conversation           |
| `/config/modbus`                              | GET      | Customer Digest     | 200            | conversation           |
| `/config/meter`                               | GET      | Customer Digest     | 200            | conversation           |
| `/config/iomapping`                           | GET      | Customer Digest     | 200            | conversation           |
| `/config/powerlimits`                         | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/config/setup/powerunit`                     | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/config/setup/gridcode/general`              | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/commands/Login?user=<username>`             | GET      | Digest passwordUser | défini dans JS | bundle `i3oe`          |
| `/commands/PasswordRecovery?user=<username>`  | GET/POST | spécifique          | défini dans JS | bundle et conversation |

## À relever par endpoint avant implémentation

**Déjà relevé dans le bundle** (fait ; table par chemin et numéros de ligne dans
`docs/ENDPOINTS.md`, section « Méthode, arguments et DTO par chemin (extraction
bundle) ») : méthode HTTP, arguments d'URL, enveloppe de réponse, champs
réellement lus par un consommateur, et rôle déclaré par le client — 54 chemins
couverts, y compris les variantes `?sync=true`, `?status=true`, `?doScan=`,
`?serviceId=`, `?componentId=`, `?testName=`.

**Reste à relever, et cela exige une capture sur l'appareil** (sonde
`cmd/fronius-probe`, hors de cette étape) :

- exemples de JSON **expurgés** : forme réelle des charges utiles, en particulier
  celles marquées `[NON OBSERVÉ]` (aucun consommateur dans le client officiel
  n'en lit les champs) et celles dont les clés sont dynamiques ;
- unités, timestamps et erreurs effectivement renvoyés par le firmware
  `1.19.7-1` ;
- permissions réellement **vérifiées par le serveur** (401/403 par rôle) : le
  bundle ne prouve que des conditions d'affichage (`docs/UNKNOWNS.md` #6) ;
- fréquence d'échantillonnage raisonnable et timeout adaptés à l'appareil ;
- effets de bord observés et classification `read-only` / `mutation` /
  `dangerous` confirmée par l'usage ;
- documentation officielle ou tests de contrat disponibles.
