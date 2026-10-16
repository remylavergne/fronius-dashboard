# Inventaire statique des chemins présents dans le bundle

> Extraction par expression régulière du JavaScript fourni. **Inventaire indicatif** : un chemin peut être utilisé en lecture, en écriture ou uniquement dans une fonctionnalité non disponible ; méthode et permissions ne sont pas inférées. Les chemins peuvent être incomplets lorsqu’ils sont construits dynamiquement.

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

- Méthode exacte, paramètres, exemples de JSON **expurgés**.
- Fréquence d'échantillonnage raisonnable, unités, timestamps, erreurs, timeout.
- Permissions déclarées vs effectivement vérifiées, rôles et firmware concernés.
- Effets de bord possibles, classification `read-only` / `mutation` / `dangerous`.
- Documentation officielle ou tests de contrat disponibles.
