# Registre des inconnues

Chaque inconnue porte : ce qui est établi, **la décision provisoire** prise en
phase 1, et **ce qui la lèverait**. Une inconnue ne se transforme jamais en
affirmation : elle reste visible dans l'interface (`/api/v1/capabilities`) et
dans `docs/SOURCES.md`.

| # | Inconnue | Décision provisoire | Ce qui la lèverait |
|---|---|---|---|
| 1 | Forme du JSON de `GetPowerFlowRealtimeData` | `[HYPOTHÈSE]` encodée dans `dto.go`, décodage tolérant, fixtures marquées synthétiques | une capture réelle par `cmd/fronius-probe` et la promotion des fixtures |
| 2 | Authentification de la Solar API sur le firmware 1.19.7-1 | `[À-TESTER]` : le client n'en envoie aucune | une réponse 401/403 de la sonde ⇒ phase 2 prioritaire |
| 3 | Source HTTP de l'identité de l'onduleur | `device: null`, capacité `device.identity` indisponible | `GetInverterInfo` / `GetLoggerInfo` / `/status/version` (phase 2) |
| 4 | Présence d'un Fronius Smart Meter | considérée non prouvée ⇒ `grid.import_export`, `load.consumption`, `autoconsumption.ratio` indisponibles, **aucun calcul** | `GetMeterRealtimeData` ou `/components/PowerMeter/readable` en GET |
| 5 | Présence d'une batterie | non prouvée ⇒ `battery.soc` indisponible | `GetStorageRealtimeData` en GET ; le PDF mentionne `Battery Operation` **actif** sans inventaire matériel |
| 6 | Matrice de permissions par endpoint et par rôle | inconnue ⇒ un seul endpoint appelé, GET, sans paramètre | API interne (phase 2) avec un compte de test ; voir `docs/ENDPOINTS.md` |
| 7 | Récupération d'un accès Technician | aucun secret ni moyen autonome confirmé ; **aucune tentative** | procédure officielle du fabricant, documentation de l'installateur |
| 8 | Sémantique de l'en-tête non standard `x-www-authenticate` | `[SOURCE-JS]` : un seul retry sur 401 ; non testé sur matériel | test contrôlé en phase 2, avec accord explicite |
| 9 | Détection dynamique du matériel | absente : la configuration ne porte pas ces flags, les capacités valent `false` | lectures de détection en phase 2, puis injection dans `model.Capabilities` |
| 10 | Valeurs aberrantes de l'appareil | **aucun filtrage** : une valeur reçue est enregistrée telle quelle ; seules les valeurs *absentes* sont distinguées | un besoin réel observé (dérive, pic, compteur qui recule) et une règle explicite |

## 1. Forme du JSON de la Solar API

**Établi.** Le bundle du fabricant ne contient **aucun** chemin Solar API :
`fcgi` 0 occurrence, `RealtimeData` 0, `PowerFlowRealtime` 0, `solar_api` 1
(uniquement `/config/solar_api`, un endpoint de configuration). Le JS ne fournit
donc aucun schéma. Vérifications reproductibles dans `docs/SOURCES.md`.

**Décision.** La forme est encodée comme hypothèse dans un seul fichier
(`internal/fronius/solarapi/dto.go`), le décodage ignore les champs inconnus, et
les fixtures portent `synthetic: true, confidence: "HYPOTHÈSE"`. Les capacités
`production.*` sont disponibles mais marquées « non vérifié » avec la réserve
« Schéma non validé sur firmware 1.19.7-1 ».

**Repli si la capture diffère.** Corriger `dto.go`, les fixtures et
`docs/SOURCES.md` (en citant la version de firmware). Les colonnes SQLite ne
changent pas : elles sont déjà en unités SI de base (W, Wh, %).

## 2. Authentification de la Solar API

**Établi.** `[OBSERVÉ-CONVERSATION]` : l'endpoint « répond ». Le mécanisme
d'authentification interne du fabricant (Digest MD5, en-tête de défi non
standard `x-www-authenticate`, un seul retry sur 401, module `xeXE` ré-exporté
par `ytu1`) a été relu dans le bundle, mais rien ne prouve qu'il s'applique à la
Solar API.

**Décision.** Ne construire aucune authentification en phase 1 : elle serait du
code mort, et le projet interdit d'inventer des identifiants.

**Repli.** Si la sonde renvoie 401/403 : remonter la phase 2 (adaptateur Digest
isolé), avec les identifiants **exclusivement** dans l'environnement du backend,
jamais dans le frontend ni dans le dépôt. Le simulateur sait déjà produire un
défi (`--fault 401`) pour développer cette voie sans matériel.

## 3. Identité de l'onduleur

**Établi.** L'export PDF fournit modèle, version de firmware et numéro de série
`[SOURCE-PDF]`, mais **aucun endpoint** ne les expose aujourd'hui : ni le bundle,
ni la Solar API telle qu'implémentée ici.

**Décision.** `GET /api/v1/system` renvoie `device: null`, et l'interface
l'explique au lieu de combler avec les valeurs du PDF — celles-ci décrivent un
instant, pas une mesure.

**Ce qui la lèverait.** `GetInverterInfo`, `GetLoggerInfo` ou `/status/version`
en GET (phase 2), puis comparaison avec l'export PDF.

## 4 et 5. Smart Meter et batterie

**Établi.** Ni le PDF ni le bundle ne prouvent la présence ou l'absence d'un
Fronius Smart Meter ou d'une batterie. Le PDF déclare `Battery Operation` et
`Full Backup` **actifs** : ce sont des fonctionnalités logicielles déclarées,
pas un inventaire matériel. L'Ethernet est `disconnected`.

**Décision.** Traiter les deux comme absents et **interdire tout calcul** de
consommation, d'import/export, d'autoconsommation et d'état de charge. Ces
capacités sont indisponibles avec un motif explicite, et aucune carte
correspondante n'apparaît dans l'interface — les cartes ne sont pas écrites en
dur, elles dérivent de `/api/v1/capabilities`.

**Ce qui la lèverait.** Une lecture de détection en GET
(`GetMeterRealtimeData`, `GetStorageRealtimeData`, `/components/PowerMeter/readable`,
`/components/BatteryManagementSystem/readable`), puis l'injection du résultat
dans `model.Capabilities(hasMeter, hasBattery, hasInternalAPI)`. La signature
existe déjà : il ne manque que la détection.

## 6. Matrice de permissions

**Établi.** Le bundle déclare des rôles côté **client** (`RoleEverybody`,
`RoleCustomer`, `RoleTechnician`, `RoleSupport`, `SetupAccessRole01/02`) qui
conditionnent l'affichage. Aucun rôle serveur n'est prouvé. `docs/ENDPOINTS.md`
classe les 79 chemins en READ / MUTATION / DANGEROUS **par intention et usage
dans le JS**.

**Décision.** Un seul endpoint appelé (Solar API, GET, sans paramètre). Tout le
reste est inventorié pour être évité, jamais pour être autorisé. Une cellule
« inconnu » n'est pas une permission implicite.

## 7. Récupération Technician

**Établi.** `[SOURCE-JS]` : `GET /commands/PasswordRecovery?user=<user>` obtient
un challenge et `remainingSignatureChecks` ; le navigateur **ne calcule pas** la
signature, elle est saisie par l'utilisateur puis vérifiée par le firmware
(échec possible `ERROR_VERIFY_SIGNATURE`). Aucun mot de passe, aucun secret de
signature, aucun moyen autonome n'a été trouvé.

**Décision.** Aucune tentative, même « pour voir » : le nombre de tentatives est
limité et l'échec peut verrouiller l'accès. L'application ne met en œuvre aucune
récupération.

**Voies légitimes.** Documentation officielle du fabricant, documentation
laissée par l'installateur, gestionnaire de mots de passe, profils de navigateur
antérieurs.

## 9. Détection dynamique du matériel

**Décision.** `model.Capabilities(hasMeter, hasBattery, hasInternalAPI)` prend
les trois détections en paramètres ; l'API les appelle aujourd'hui avec `false`
et l'assume explicitement (`internal/api/server.go`). Aucun flag de
configuration ne permet de forcer ces valeurs : cela créerait une capacité
« disponible » sans mesure derrière.

## 10. Valeurs aberrantes

**Décision.** Aucun filtrage n'est appliqué en phase 1 : si l'appareil renvoie
une valeur, elle est enregistrée. Le modèle ne distingue que **présent** et
**absent** (`nil`), jamais « plausible » et « aberrant ». Inventer une règle de
rejet sans observation reviendrait à décider à la place de l'utilisateur ce
qu'est une mesure valide.

**Ce qui la lèverait.** Un cas réel observé (compteur qui recule, pic
impossible, valeur négative là où elle ne peut pas l'être) et une règle écrite :
seuils, marquage en `Quality`, conservation de la valeur brute. En attendant, la
responsabilité est laissée au capteur et l'interface affiche ce qui est reçu.

## Inconnues hors périmètre MVP

Documentées pour mémoire, sans décision nécessaire en phase 1 : DTO exacts des
commandes internes utiles en lecture seule, garde Angular `oi.a` et contrôles de
composants, format du fichier `sdp.cry`, rôle exact de `SupportAccount`, profils
et gestionnaire de mots de passe, nécessité d'une authentification applicative
pour l'accès distant (à trancher par un ADR avant tout accès distant permanent).
