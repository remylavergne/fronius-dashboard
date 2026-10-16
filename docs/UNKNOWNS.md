# Registre des inconnues

Chaque inconnue porte : ce qui est établi, **la décision provisoire** prise en
phase 1, et **ce qui la lèverait**. Une inconnue ne se transforme jamais en
affirmation : elle reste visible dans l'interface (`/api/v1/capabilities`) et
dans `docs/SOURCES.md`.

| # | Inconnue | Décision provisoire | Ce qui la lèverait |
|---|---|---|---|
| 1 | Forme du JSON de `GetPowerFlowRealtimeData` | `[HYPOTHÈSE]` encodée dans `dto.go`, décodage tolérant, fixtures marquées synthétiques | une capture réelle par `cmd/fronius-probe` et la promotion des fixtures |
| 2 | Authentification **de la Solar API** sur le firmware 1.19.7-1 | `[À-TESTER]` : le client n'en envoie aucune ; les chemins internes, eux, sont authentifiés après un 401 | une réponse 401/403 de la sonde sur le chemin Solar API |
| 3 | **Source partielle** de l'identité de l'onduleur | `[SOURCE-JS]` + `[VÉRIFIÉ]` simulateur : `/status/version` est la source nommée, **seul `serialNumber` est un champ prouvé** (avec `numberOfPhases` et `devicegroup`) ; `modelName`/`firmwareVersion` sont des candidats. `device: null` tant qu'aucune lecture n'a abouti ; `device.identity` indisponible avec motif sinon | une capture réelle : confirmer les noms des clés de modèle et de firmware, puis les placer en tête des listes candidates |
| 4 | Présence d'un Fronius Smart Meter | détectée par l'enveloppe `Cgi` de `/components/PowerMeter/readable` : `Body.Data` non vide ⇒ présent, vide ⇒ absent, illisible ⇒ **non déterminé** (jamais « absent »). Aucun champ nommé n'est utilisé | une capture réelle de `/components/PowerMeter/readable` |
| 5 | Présence d'une batterie | même règle que #4 sur `/components/BatteryManagementSystem/readable` ; `battery.soc` reste indisponible **même batterie détectée**, faute de preuve qu'une mesure d'état de charge existe côté API | une capture réelle, puis la lecture de l'état de charge (phase ultérieure) |
| 6 | Matrice de permissions par endpoint et par rôle | la **liste blanche du code** (`internal/fronius/internalapi/allowlist.go`) est la seule autorisation exécutable : quatre chemins internes, `READ`, GET ; les 79 autres sont inventoriés pour être évités | un compte de test et des captures ; voir `docs/ENDPOINTS.md` |
| 7 | Récupération d'un accès Technician | aucun secret ni moyen autonome confirmé ; **aucune tentative** | procédure officielle du fabricant, documentation de l'installateur |
| 8 | En-tête de défi non standard `x-www-authenticate` | `[SOURCE-JS]` + `[VÉRIFIÉ]` simulateur **et** client tiers : un seul retry sur 401, défi lu d'abord dans l'en-tête non standard, `www-authenticate` en repli | l'appareil réel, pour confirmer qu'il utilise bien l'en-tête non standard |
| 9 | Détection dynamique du matériel | **implémentée** : `internal/detect` interroge les quatre chemins, la détection est persistée puis injectée dans `model.Capabilities(detection)` | la levée de #3, #4 et #5 par capture matérielle |
| 11 | Forme de `/status/activeEvents` | lecture seule établie ; les clés `code`, `state`, `severity`, `level`, `message`, `text` sont des **candidats**. Le JSON brut est conservé (borné à 2 048 octets) et la déduplication repose sur `code`, puis `state`, puis l'empreinte SHA-256 du JSON — donc aucune information n'est perdue | une capture réelle : elle indiquera les clés de message et de sévérité |
| 12 | Signature du défi Digest (`cnonce="NaN"`) | non reproduite : le client officiel émet `NaN` par un bug de conversion ; nous émettons un `cnonce` aléatoire de 16 caractères hexadécimaux | si l'appareil refuse malgré tout, remplacer par la constante `"NaN"` (repli documenté dans `CHANGELOG.md`) |
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

**Établi.** `[OBSERVÉ-CONVERSATION]` : l'endpoint « répond ». Rien ne prouve
qu'une authentification s'applique au chemin Solar API : dans le client officiel,
ce chemin n'existe même pas. Les chemins **internes**, eux, sont atteints par
deux voies (avec et sans en-tête), ce qui est établi et implémenté.

**Décision.** Le transport Digest est composé devant les deux clients, mais il
envoie toujours la requête **nue** d'abord : si le chemin Solar API n'exige rien,
aucun en-tête n'est ajouté et le comportement reste celui de la phase 1.

## 3. Identité de l'onduleur

**Établi.** L'export PDF fournit modèle, version de firmware et numéro de série
`[SOURCE-PDF]`. Côté HTTP, `/status/version` est la source nommée par le client
officiel (deux modules distincts l'appellent), et **trois champs seulement** y
sont prouvés : `serialNumber`, `numberOfPhases`, `devicegroup`.

**Décision.** L'identité est lue par `internal/detect` et persistée dans la table
`device`. `modelName` et `firmwareVersion` — candidats non prouvés — sont
extraits par liste ordonnée de clés, et une clé absente donne `null`, jamais
`""` ni « inconnu ». Sans lecture aboutie, `GET /api/v1/system` renvoie
`device: null` et une détection `unknown` avec son motif : le PDF n'est jamais
recopié, il décrit un instant, pas une mesure.

**Ce qui la lèverait.** Une capture réelle de `/status/version` par
`cmd/fronius-probe --endpoint /status/version`.

## 4 et 5. Smart Meter et batterie

**Établi.** Ni le PDF ni le bundle ne prouvent la présence ou l'absence d'un
Fronius Smart Meter ou d'une batterie. Le PDF déclare `Battery Operation` et
`Full Backup` **actifs** : ce sont des fonctionnalités logicielles déclarées,
pas un inventaire matériel. L'Ethernet est `disconnected`.

**Décision.** La présence est **détectée** par l'enveloppe `Cgi` : `Body.Data`
non vide ⇒ présent, vide ou absent ⇒ absent, corps illisible ⇒ erreur de décodage
donc **non déterminé**. Les trois états sont persistés tels quels
(`meter_state`, `battery_state`) : un 401 ne peut pas devenir « aucun Smart Meter
détecté ». Tout calcul de consommation, d'import/export et d'autoconsommation
reste interdit sans compteur détecté, et `battery.soc` reste indisponible même
batterie détectée. Aucune carte correspondante n'est écrite en dur dans
l'interface : elles dérivent de `/api/v1/capabilities`.

**Ce qui la lèverait.** Une capture réelle des deux chemins `readable` — et, pour
l'état de charge, une source de mesure qui reste à établir.

## 6. Matrice de permissions

**Établi.** Le bundle déclare des rôles côté **client** (`RoleEverybody`,
`RoleCustomer`, `RoleTechnician`, `RoleSupport`, `SetupAccessRole01/02`) qui
conditionnent l'affichage. Aucun rôle serveur n'est prouvé. `docs/ENDPOINTS.md`
classe les 79 chemins en READ / MUTATION / DANGEROUS **par intention et usage
dans le JS**.

**Décision.** La liste blanche du code est la seule autorisation exécutable :
quatre chemins internes plus le chemin Solar API, tous `READ`, tous en GET. Tout
le reste est inventorié pour être évité, jamais pour être autorisé. Une cellule
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

**Décision.** `model.Capabilities(detection)` prend une détection à trois états
par ressource (`unknown`, `absent`, `present`) ; l'API la construit à partir de
la dernière lecture persistée (`internal/detect`, table `device`). Aucun flag de
configuration ne permet de forcer ces valeurs : cela créerait une capacité
« disponible » sans mesure derrière, ou une « absence » sans lecture.

**Règle de trafic.** Une lecture qui reçoit un 401 alors qu'aucun identifiant
n'est configuré fait abandonner **ce chemin** jusqu'au redémarrage, avec le motif
qui nomme les deux variables d'environnement. Un état inconnu n'est pas une
absence, mais il n'y a pas lieu de réémettre un 401 toutes les minutes.

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
