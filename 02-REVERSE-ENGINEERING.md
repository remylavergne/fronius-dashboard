# Reverse engineering du client Web Fronius

## Cartographie des modules Webpack
| Module | Rôle et éléments trouvés |
|---|---|
| `mgOI` | Composant de login, injection `_commands` via `p = n("i3oe")` ; clés d'accès setup distinctes de Technician. |
| `i3oe` | Service de commandes : `Login`, `Logout`, `SetPassword`, `ActivatePasswords`, autres commandes. |
| `JWhm` | Classe HTTP de base : `callGet$`, `callPost$`, `callGetWithoutCatch$`, `callPostWithoutCatch$`, `_getUserAndPass`. |
| `CNEH` | Stockage local des identifiants `AuthHash`. |
| `LsM5` | DAO de login / mise à jour du store NgRx. |
| `nuFz` | Action `[Login] login` et logique de rôle dans le store. |
| `ytu1` | Génération des en-têtes HTTP Digest. |

## Authentification
- [SOURCE-JS] `GET /commands/Login?user=<username>` ; le service `i3oe` passe `{user: username, password, type: passwordUser}`.
- [SOURCE-JS] `JWhm._getUserAndPass` : `passwordUser` renvoie `username:password` ; `fallback` récupère les identifiants de `_requestGroup`.
- [SOURCE-JS] `JWhm.callGet$` et `callPost$` réessaient **une fois** sur HTTP 401 après lecture de `x-www-authenticate` et mise à jour du nonce. En-tête `Authorization` calculé par `ytu1.DigestHeader`.
- [OBSERVÉ-CONVERSATION] Realm `Webinterface area`, algorithme MD5, mécanisme Digest particulier avec `X-WWW-Authenticate` ; un client Python urllib avec prise en charge spécifique a déjà fonctionné pour des GET Customer.
- [SOURCE-JS] `CNEH` écrit `localStorage.setItem("AuthHash", username + ";" + password)` : **identifiants en clair** ; ne jamais reproduire ce stockage dans le nouveau frontend.
- [SOURCE-JS] Angular définit des rôles cumulés : `user` (`RoleEverybody`), `customer` (+`RoleCustomer`), `technician` (+`RoleTechnician`), `support` (+`RoleSupport`), `profi` (+`SetupAccessRole01`), `utility` (+`SetupAccessRole02`). Ces noms sont ceux du client, pas des preuves de permission effective serveur.
- [SOURCE-JS] Clés codées dans le composant setup `77634` et `86536` ; elles concernent des déverrouillages de type Profi/Utility, **pas des mots de passe Technician**.

## Dérivation des nouveaux mots de passe
- [SOURCE-JS] `createNode(user, password, includeSalt)` : `hashedPassword = MD5("user:Webinterface area:password")` ; pour utilisateur différent de `user` et mot de passe non vide, PBKDF2 avec **100 000 itérations**, sortie **64 octets**, sel de **4 octets** généré par `Math.random()` puis représenté en hexadécimal.
- [SOURCE-JS] `pbkdf2_salt` n'est renseigné que si `includeSalt` est vrai ; `pbkdf2` est calculé lorsque les conditions sont réunies.
- [SOURCE-JS] Ne pas inférer la PRF exacte de PBKDF2 à partir de ce seul extrait : suivre l'import crypto pour confirmation.

## Récupération
- [SOURCE-JS] `GET /commands/PasswordRecovery?user=<user>` obtient un challenge / `remainingSignatureChecks`.
- [SOURCE-JS] `recoverPassword()` envoie une requête POST avec `new` (HA1 MD5), `pbkdf2_salt`, `signature`, et le champ `pbkdf2_technician` / `_support` / `_customer` / `_user` selon le compte.
- [SOURCE-JS] Le navigateur **ne calcule pas la signature** : elle est saisie dans le formulaire puis transmise. Vérification côté firmware, échec possible `ERROR_VERIFY_SIGNATURE`.
- [OBSERVÉ-CONVERSATION] Ne pas essayer de signatures aléatoires ; nombre de tentatives limité.

## Maintenance
- [SOURCE-JS] `SupportAccount` expose activation/désactivation et terminaison de session avec `resetPassword: true` : ne pas appeler sans objectif explicite et vérification des droits.
- [SOURCE-JS] `CreateSDPFile` → statut → `GetSDPFile` pour télécharger `sdp.cry`. Format interne inconnu, ne pas supposer qu'il contient des secrets exploitables.
- [SOURCE-JS] `ServiceFile` fait référence à `service.data`.
- [OBSERVÉ-CONVERSATION] Routes Angular avec garde `oi.a` ; **son implémentation et les contrôles de composants restent à auditer**. Ne pas présenter une matrice des permissions de pages comme validée.

## Résultat sur Technician
Aucun mot de passe, secret de signature ou moyen de récupération autonome confirmé. Pistes légitimes : anciens navigateurs/profils, gestionnaires de mots de passe, documentation de l'installateur, procédure officielle. Ne pas contourner les protections ou modifier les paramètres réseau électrique.
