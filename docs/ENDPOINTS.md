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

## Liste blanche de la phase 1

| Chemin | Méthode | Classification | Preuve |
|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | READ | `[OBSERVÉ-CONVERSATION]` « répond » ; **absent du bundle** ; méthode GET imposée par `internal/fronius/readonly.go` |

C'est le **seul** endpoint que la phase 1 appelle. `cmd/fronius-probe` n'accepte
rien d'autre (`--list`).

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
| `/components/BatteryManagementSystem/readable` | READ | Lecture d'état |
| `/components/DataManager/readable` | READ | Lecture d'état |
| `/components/Ohmpilot/readable` | READ | Lecture d'état |
| `/components/PowerMeter/readable` | READ | Lecture d'état ; candidat `[À-TESTER]` pour détecter la présence d'un Smart Meter |
| `/components/cache/readable` | READ | Lecture d'état |
| `/status/activeEvents` | READ | Lecture d'événements |
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
| `/status/version` | READ | Candidat `[À-TESTER]` pour l'identité (modèle, firmware) |
| `/update/start` | **DANGEROUS** | Déclenche une mise à jour de firmware |

## Règle applicable

Aucune commande `POST`, `PUT`, `PATCH` ou `DELETE` n'est émise vers l'onduleur.
Cette règle est structurelle (`internal/fronius/readonly.go`), pas déclarative :
une requête non-GET est rejetée avant d'atteindre le transport, et la vérification
`grep -rn 'MethodPost\|MethodPut\|MethodDelete\|MethodPatch' internal cmd apps/web/src`
ne doit rien trouver hors du message d'erreur de la garde et de ses tests.

Les endpoints `READ` marqués `[À-TESTER]` ne seront appelés qu'après une revue
explicite et une confirmation de l'utilisateur, dans le cadre de la phase 2.
