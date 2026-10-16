# Registre des inconnues

Chaque inconnue porte : ce qui est établi, **la décision provisoire** prise en
phase 1, et **ce qui la lèverait**. Une inconnue ne se transforme jamais en
affirmation : elle reste visible dans l'interface (`/api/v1/capabilities`) et
dans `docs/SOURCES.md`.

| # | Inconnue | Décision provisoire | Ce qui la lèverait |
|---|---|---|---|
| 1 | Forme du JSON de `GetPowerFlowRealtimeData` | `[HYPOTHÈSE]` encodée dans `dto.go`, décodage tolérant, fixtures marquées synthétiques | une capture réelle par `cmd/fronius-probe` et la promotion des fixtures |
| 2 | Authentification **de la Solar API** sur le firmware 1.19.7-1 | `[À-TESTER]` : le client n'en envoie aucune ; les chemins internes, eux, sont authentifiés après un 401 | une réponse 401/403 de la sonde sur le chemin Solar API |
| 3 | **Source partielle** de l'identité de l'onduleur | `[SOURCE-JS]` + `[VÉRIFIÉ]` simulateur : `/status/version` est la source nommée. **Résolu par l'extraction** : la version logicielle se lit sur le chemin prouvé `swrevisions.GEN24` (l. 128882, 135137), que le client officiel affiche comme version courante — l'application l'utilise désormais (`firmwareOf`). Champs prouvés : `serialNumber`, `numberOfPhases`, `devicegroup`, `hwrevisions.FAMILYID`, et `hardwareId`/`devicename`/`articleNumber` (lus, non exposés). **Reste ouvert** : aucune clé de **modèle** n'est observée ⇒ `Model` reste `nil` (le groupe d'appareil n'est plus employé comme repli), et les trois champs extraits ne sont pas exposés jusqu'à l'écran — les brancher coûte 14 fichiers dont une migration SQL, décision reportée faute de consommateur (ADR 0008) | une capture réelle de `/status/version` : confirmer la version, trancher sur l'existence d'une clé de modèle |
| 4 | Présence d'un Fronius Smart Meter | détectée par l'enveloppe `Cgi` de `/components/PowerMeter/readable` : `Body.Data` non vide ⇒ présent, vide ⇒ absent, illisible ⇒ **non déterminé** (jamais « absent »). La détection ne dépend d'aucun champ nommé ; l'extraction relève en revanche les champs utilisables ensuite (`channels` : 23 signaux `SMARTMETER_*`, `attributes.addr`, `attributes["[ENABLE]"]` — `docs/ENDPOINTS.md`) | une capture réelle de `/components/PowerMeter/readable` |
| 5 | Présence d'une batterie et état de charge | présence : même règle que #4 sur `/components/BatteryManagementSystem/readable` ; `battery.soc` reste **indisponible** dans l'application — décision de périmètre inchangée. Nuance apportée par l'extraction : le client officiel **lit bien** un état de charge relatif, `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` en `%`, dans `channels` de ce chemin (l. 128986), et aussi `inverters[<id>].SOC` dans `/status/powerflow` (l. 129053) : la preuve d'existence d'une mesure existe donc côté API interne, sans qu'aucune lecture n'ait été faite sur l'appareil | une capture réelle, puis une décision explicite (justification par une métrique indisponible en Solar API) avant toute lecture de l'état de charge |
| 6 | Matrice de permissions par endpoint et par rôle | l'extraction (voir `docs/ENDPOINTS.md`) établit le **rôle déclaré par le client** — condition d'affichage, jamais une autorisation : `_isUserAllowed` (l. 75972-75984) accorde `customer` à `customer \| technician \| support`, et `technician`/`support` à `technician \| support` ; **une seule** entrée du registre déclare `user: "customer"` (`/commands/GetUTCTime`, l. 76039). Elle établit aussi le **compte envoyé** (`_getUserAndPass`, l. 46807-46813 : compte du groupe de requêtes, sauf `Login` en `passwordUser`) — **aucune permission serveur n'est prouvable depuis un bundle**. Décision inchangée : la **liste blanche du code** (`internal/fronius/internalapi/allowlist.go`) est la seule autorisation exécutable : quatre chemins internes, `READ`, GET ; les 79 autres sont inventoriés pour être évités | un compte de test et des captures HTTP par rôle (401 / 403 / 200) ; voir `docs/ENDPOINTS.md` |
| 7 | Récupération d'un accès Technician | aucun secret ni moyen autonome confirmé ; **aucune tentative** | procédure officielle du fabricant, documentation de l'installateur |
| 8 | En-tête de défi non standard `x-www-authenticate` | `[SOURCE-JS]` + `[VÉRIFIÉ]` simulateur **et** client tiers : un seul retry sur 401, défi lu d'abord dans l'en-tête non standard, `www-authenticate` en repli | l'appareil réel, pour confirmer qu'il utilise bien l'en-tête non standard |
| 9 | Détection dynamique du matériel | **implémentée** : `internal/detect` interroge les quatre chemins, la détection est persistée puis injectée dans `model.Capabilities(detection)` | la levée de #3, #4 et #5 par capture matérielle |
| 11 | Forme de `/status/activeEvents` | **Résolu par l'extraction et appliqué** : charge utile = tableau d'objets ; `severity` est un **entier** (1 erreur, 2 avertissement, 3 information, l. 135728-135733) — avant correction, seules des chaînes étaient reconnues et tout événement matériel était classé « information » ; l'identité d'instance est `uuid`, sinon `prefix`-`eventID` (l. 135693, 164699) — dédupliquer sur `code` seul faisait perdre un second événement de même type ; le texte affiché est `label` (l. 135654). Les clés `code`, `state`, `level`, `message`, `text`, `description` restent des **candidats de secours**, et `viewer`, `category`, `confirmable`, `activeUntil`, `sourceID`, `timestamp` sont conservés dans `Detail` (JSON brut borné à 2 048 octets) sans être interprétés | une capture réelle : confirmer noms et types, la sémantique de `viewer` (le client y voit un niveau d'audience, l. 135709-135717) et le fait que `label` porte bien un texte lisible |
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
officiel (voie sans en-tête, module `yTJA`, l. 183050), et l'extraction y relève
les champs **lus** : `serialNumber`, `numberOfPhases`, `devicegroup`, `hardwareId`,
`devicename`, `articleNumber`, `swrevisions.GEN24`, `hwrevisions.FAMILYID`.

**Résolu pour la version logicielle.** `swrevisions.GEN24` (l. 128882, 135137) est
la clé que le client officiel affiche comme version courante : elle passe
désormais avant les candidats plats `firmwareVersion`/`firmware`/`version`, et le
test `TestVersionExtractsKnownAndCandidateKeys` couvre les deux chemins. Aucune
clé de **modèle** n'étant observée, `Model` reste `nil` : le groupe d'appareil
(`devicegroup`) n'est plus employé comme repli, car c'est un champ frère d'une
autre nature (l. 183035).

**Décision.** L'identité est lue par `internal/detect` et persistée dans la table
`device`. Les clés non prouvées sont extraites par liste ordonnée de candidats, et
une clé absente donne `null`, jamais `""` ni « inconnu ». Les trois champs extraits
mais sans consommateur (`hardwareId`, `devicename`, `articleNumber`) ne sont pas
persistés : les exposer coûte 14 fichiers dont une migration SQL (`0004`), coût
mesuré et décision reportée tant qu'aucun besoin ne les nomme (ADR 0008). Sans
lecture aboutie, `GET /api/v1/system` renvoie `device: null` et une détection
`unknown` avec son motif : le PDF n'est jamais recopié, il décrit un instant, pas
une mesure.

**Ce qui la lèverait.** Une capture réelle de `/status/version` par
`cmd/fronius-probe --endpoint /status/version` (§ `docs/PROBE-READONLY.md`) :
confirmer la version, trancher sur l'existence d'une clé de modèle.

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
l'état de charge, une décision explicite : le client officiel lit bien
`BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` (`%`) dans les `channels` du chemin
batterie (l. 128986) et `inverters[<id>].SOC` dans `/status/powerflow`
(l. 129053), donc la mesure existe côté API interne ; son usage par cette
application reste une décision séparée, non prise ici, qui exige une justification
(une métrique que la Solar API ne fournit pas) et une capture pour confirmer le
nom du signal sur le firmware **1.19.7-1**.

## 6. Matrice de permissions

**Établi.** Le bundle déclare des rôles côté **client** (`RoleEverybody`,
`RoleCustomer`, `RoleTechnician`, `RoleSupport`, `SetupAccessRole01/02`) qui
conditionnent l'affichage. Aucun rôle serveur n'est prouvé. `docs/ENDPOINTS.md`
classe les 79 chemins en READ / MUTATION / DANGEROUS **par intention et usage
dans le JS**.

**Ce que l'extraction méthode/arguments/DTO ajoute.** Pour chaque chemin lu par le
client officiel, `docs/ENDPOINTS.md` donne désormais la méthode, les arguments,
l'enveloppe et le rôle **déclaré** par le client. Trois faits en ressortent :
`_isUserAllowed` (l. 75972-75984) n'implémente qu'une hiérarchie d'affichage
(`customer` ⊂ `technician` ⊂ `support`), **une seule** entrée du registre déclare
un rôle (`user: "customer"` sur `/commands/GetUTCTime`, l. 76040), et le compte
effectivement envoyé est celui du groupe de requêtes (`fallback`, l. 46809) sauf
pour `Login` (`passwordUser`, l. 46811). Rien de tout cela n'est une autorisation :
un bundle client ne peut pas prouver ce que le firmware accepte.

**Décision.** La liste blanche du code est la seule autorisation exécutable :
quatre chemins internes plus le chemin Solar API, tous `READ`, tous en GET. Tout
le reste est inventorié pour être évité, jamais pour être autorisé. Une cellule
« inconnu » n'est pas une permission implicite — et une méthode désormais
**observée** (par exemple la `POST` du client officiel sur
`/commands/ConnectedPowermeter`, l. 91025) ne change pas cette liste.

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

Documentées pour mémoire, sans décision nécessaire en phase 1 : garde Angular
`oi.a` et contrôles de composants, format du fichier `sdp.cry`, rôle exact de
`SupportAccount`, profils et gestionnaire de mots de passe, nécessité d'une
authentification applicative pour l'accès distant (à trancher par un ADR avant
tout accès distant permanent).

Les DTO des commandes internes en lecture seule ne sont **plus** une inconnue hors
périmètre : ils sont extraits dans `docs/ENDPOINTS.md`. Ce qui reste `[NON
OBSERVÉ]` après extraction — charge utile jamais consommée par le client officiel
— est nommé dans cette table : `/status/registrationData` (bloc JSON),
`/status/powerflowcontroller`, `/commands/StandbyState`, `/commands/GetSDPFile`,
`/commands/ConnectedPowermeter?componentId=`, les deux variantes
`?sync=true` et `/config/logging`, `/config/solarweb`, `/config/gridcode`.
