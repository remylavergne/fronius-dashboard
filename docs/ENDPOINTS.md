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
| `/status/version` | GET | READ | `Default` | `[SOURCE-JS]` l. 76042 (registre pollé, module `aYRN`) et l. 183050 (voie **sans** en-tête, module `yTJA`) ; `[VÉRIFIÉ]` sur le simulateur |
| `/status/activeEvents` | GET | READ | `Default` | `[SOURCE-JS]` l. 76030 (intervalle 10 000 ms) ; `[VÉRIFIÉ]` sur le simulateur. Forme du corps non établie |
| `/components/PowerMeter/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76031 + l. 75958 ; `[VÉRIFIÉ]` sur le simulateur |
| `/components/BatteryManagementSystem/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76032 ; `[VÉRIFIÉ]` sur le simulateur |

Note : ces chemins sont atteints par **deux voies** dans le client officiel — avec
en-tête `Authorization` (module `aYRN`) et sans (module `yTJA`). Le classement
`READ` porte sur l'effet, pas sur l'authentification : aucun des quatre n'a
d'effet persistant observé.

## Chemins explicitement écartés

| Chemin | Raison |
|---|---|
| `/config/meter`, `/config/batteries` | Lecture plausible, mais leur littéral et leur verbe sont hors de la zone lisible du bundle (module `rCKn`) : les appeler reviendrait à inventer un contrat. La présence du compteur et de la batterie est donc déduite de `Body.Data` des chemins `Cgi`, sans lire ces configurations. |
| `/status/events`, `/status/devices`, `/status/common`, `/components/DataManager/readable` | Inventoriés, lisibles, mais hors du besoin de la phase 2 : ne pas appeler ce dont on n'a pas besoin réduit la surface. |

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
| `/status/*` restants (16 chemins) | inconnu `[À-TESTER]` | inconnu | rôles d'affichage Angular `[SOURCE-JS]`, non prouvés | aucun | aucun attendu (lecture d'état) |
| `/components/*/readable` restants (3 chemins) | inconnu `[À-TESTER]` | inconnu | idem | aucun | aucun attendu |
| `/config/*` en lecture (24 chemins) | `GET` observé sur 4 chemins | inconnu | Digest « Customer » `[OBSERVÉ-CONVERSATION]` | 4 chemins : `/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` → HTTP 200 ; `/config/powerlimits`, `/config/setup/*` → 200 `{}` | aucun (lecture) |
| `/config/*` en écriture (24 chemins) | inconnu (POST/PUT probable) | inconnu | inconnu | **aucun** | modification de configuration ; `DANGEROUS` pour `gridcode`, `pcc`, `powerlimits`, `modbus`, `setup/*` |
| `/commands/*` en lecture (8 chemins : `ConnectedPowermeter`, `GetAutoTestReport`, `GetSDPFile`, `GetServiceState`, `GetUTCTime`, `GetUpdateStatus`, `InverterRollbackStatus`, `StandbyState`, `ListGridcodes`, `ListOhmpilots`) | `GET` probable | `user` pour certains | `passwordUser` ou compte de service `[SOURCE-JS]` | **aucun** | aucun attendu |
| `/commands/*` en mutation (17 chemins : `ActivatePasswords`, `ConfirmEvent`, `CreateSDPFile`, `LicenseExchange`, `Login`, `Logout`, `ScanAvailableWlan`, `SetPassword`, `SetUTCTime`, `SetWPSActive`, `StandbyRequestState`, `VerifyVCode`, …) | `GET`/`POST` selon la commande `[SOURCE-JS]` | variable | variable selon la commande | **aucun** | modification d'état, de session, de fichier ou de préférence |
| `/commands/*` dangereux (10 chemins : `DCInjectionTest`, `GridcodeSelection`, `InverterControl`, `InverterRollback`, `PasswordRecovery`, `ReInitInterface`, `RemoteAccess`, `StartSystemService`, `SupportAccount`, `factoryReset`) | inconnu | inconnu | inconnu | **aucun** | coupure de production, firmware, protection réseau électrique, accès ou effacement |
| `/commands/License`, `LicenseExchangeData`, `ServiceFile` | inconnu | inconnu | inconnu | **aucun** | indéterminé ⇒ traité comme une écriture |
| `/update/start` | inconnu | inconnu | inconnu | **aucun** | mise à jour de firmware de l'appareil |

Ce que cette matrice établit : **cinq chemins sont appelés**, tous en `GET`, sans
paramètre, sans effet de bord observé, et sans aucune entrée `MUTATION` ou
`DANGEROUS` dans la liste blanche exécutable. Les 79 autres chemins recensés dans
ce document restent inventoriés pour être explicitement évités. Les cellules
« inconnu » ne sont pas des autorisations implicites : ce sont des inconnues
(voir `docs/UNKNOWNS.md`).

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
