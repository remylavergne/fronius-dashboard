# Sonde lecture seule — protocole et accord préalable

`cmd/fronius-probe` est le **seul** outil de ce dépôt capable de contacter un
onduleur réel autre que le simulateur. Ce document décrit ce qu'il fait
exactement, ce qu'il refuse de faire, et ce que l'utilisateur doit faire de son
résultat.

Règle fondatrice : **l'agent de développement n'exécute jamais cette sonde**, ni
sur le matériel, ni contre une adresse non locale. Elle est livrée pour être
lancée par la personne responsable de l'installation, qui décide du moment.

## 1. Ce que la sonde demande avant de partir

Avant toute exécution contre le matériel réel, quatre questions doivent avoir une
réponse explicite :

1. **Adresse cible** — l'URL exacte de l'appareil (par exemple
   `http://192.0.2.10`). L'hôte et le port sont-ils ceux de l'onduleur et
   non d'un autre équipement ?
2. **Moment** — est-il acceptable que le journal de l'onduleur enregistre cette
   requête à cet instant ?
3. **Réseau** — l'accès est-il prévu (Wi-Fi de l'installation, VPN, poste sur le
   même réseau) ?
4. **Sortie** — où le résultat expurgé sera-t-il conservé, et qui le relira ?

Aucune de ces réponses ne peut être devinée par l'agent : elles relèvent de
l'utilisateur.

## 2. Requête exacte émise

La sonde ne peut émettre **qu'une seule** requête, figée dans le binaire :

```
GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi HTTP/1.1
Accept: application/json
Host: <adresse fournie avec --base-url>
```

- **Méthode** : `GET` uniquement. La liste blanche ne contient qu'une entrée,
  classée `READ`, et sa méthode est une constante du code.
- **Paramètres** : aucun. La sonde n'ajoute ni requête de chaîne, ni corps, ni
  champ de formulaire.
- **En-têtes** : le seul en-tête ajouté est `Accept: application/json`. Aucune
  valeur d'authentification n'est construite.
- **Répétition** : aucune. La sonde émet exactement une requête puis rend la
  main. Aucun retry, aucune boucle, aucun intervalle.

Le contractuel est vérifié par construction, pas par discipline :

- `fronius.ReadOnly` est interposé devant le transport et rejette toute requête
  dont la méthode n'est pas `GET`, avant toute ouverture de connexion
  (`internal/fronius/readonly.go`) ;
- la vérification `grep -rn 'MethodPost\|MethodPut\|MethodDelete\|MethodPatch' internal cmd`
  ne trouve rien hors du message d'erreur de cette garde et de ses tests.

Pour afficher la liste blanche sans émettre aucune requête :

```bash
go run ./cmd/fronius-probe --list
```

## 3. Garde d'adresse locale

Par défaut, la sonde **refuse** toute adresse qui n'est pas la boucle locale
(`127.0.0.1`, `::1`, `localhost`) :

```bash
go run ./cmd/fronius-probe --base-url http://192.0.2.10
# fronius-probe: refus: http://192.0.2.10 n'est pas une adresse locale ;
# relancer avec --allow-non-loopback après avoir lu docs/PROBE-READONLY.md
# et obtenu l'accord explicite de l'utilisateur
```

Le refus intervient **avant** toute résolution de nom et toute connexion :
aucun paquet ne part vers l'appareil. La levée de la garde est un acte
volontaire : `--allow-non-loopback`.

## 4. Commande à lancer

Vers le simulateur local (aucun risque, aucun matériel) :

```bash
# terminal 1
go run ./cmd/fronius-sim

# terminal 2
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081
```

Vers l'onduleur réel, après l'accord du point 1 :

```bash
go run ./cmd/fronius-probe \
  --base-url http://192.0.2.10 \
  --allow-non-loopback \
  --timeout 5s \
  --save test/fixtures/solarapi
```

Une seule requête sera émise. Si l'appareil répond en erreur, la sonde le
signale et ne réessaie pas.

## 5. Sortie attendue

```
Requête
  GET http://192.0.2.10/solar_api/v1/GetPowerFlowRealtimeData.fcgi
  classification : READ
  liste blanche : Mesure de production du site (Body.Data.Site) et des onduleurs (Body.Data.Inverters)

Réponse
  statut   : 200 OK
  durée    : 12ms
  octets   : 741
  en-têtes (valeurs sensibles masquées)
    Content-Type: application/json
    Date: Fri, 16 Oct 2026 09:58:15 GMT

Corps expurgé
{ … }
```

Interprétation :

| Observation | Conséquence |
|---|---|
| `200 OK` et `Body.Data.Site` renseigné | La forme supposée de `dto.go` est confirmée : promouvoir la fixture (§ 7). |
| `401` ou `403` avec `x-www-authenticate` | La Solar API exige une authentification sur ce firmware : la phase 2 (adaptateur Digest) devient prioritaire. |
| `404` | Le chemin diffère sur ce firmware : corriger `internal/fronius/solarapi/dto.go` et ce document. |
| Champs ou unités différents de ceux attendus | Corriger `dto.go`, les fixtures et `docs/SOURCES.md`. Les colonnes SQLite ne changent pas (unités SI de base). |
| Absence de `P_Grid`, `P_Load`, `rel_*` | Comportement attendu sans Smart Meter ; confirme l'invariance « pas de zéro inventé ». |

## 6. Règles d'expurgation

L'expurgation s'applique **avant** tout affichage et **avant** toute écriture sur
disque. Elle est implémentée dans `cmd/fronius-probe/redact.go` et testée dans
`probe_test.go`.

| Cible | Règle |
|---|---|
| Champs JSON | Toute clé contenant `password`, `passwd`, `secret`, `nonce`, `token`, `authorization`, `credential`, `serial`, `mac`, `ssid`, `hostname`, `ipaddress` ou `sessionid` voit sa valeur remplacée par `<redacted>`. |
| Valeurs textuelles | Adresse IPv4, adresse IPv6, adresse MAC, nom d'hôte ou nom pleinement qualifié → `<redacted>`. |
| Corps non JSON | Les affectations `password=…`, `nonce=…`, `serial=…`, etc. sont expurgées, puis les motifs d'adresses sont appliqués au texte. |
| En-têtes HTTP | `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `WWW-Authenticate` / `x-www-authenticate`, `X-CSRF-Token`, `X-Auth-Token` : valeur intégralement masquée. Les autres en-têtes sont masqués si leur valeur ressemble à une adresse. |
| Fichier écrit | Mode `0600`, nom horodaté (`probe_<Endpoint>_<UTC>.json`). |

Deux règles de non-régression sont testées explicitement :

- **les mesures ne sont jamais expurgées** : une puissance, une énergie, un
  horodatage ou un mode restent intacts (sinon la fixture serait inutilisable) ;
- **une heure n'est pas une adresse IPv6** : `12:00:00` et
  `2026-06-21T12:00:00+02:00` ne sont pas masqués (`looksLikeIPv6` exige au
  moins quatre groupes, ou la forme compressée `::`).

Le JSON est réécrit avec une indentation stable, ce qui peut **réordonner les
clés** d'un objet. Le résultat reste un JSON valide ; c'est le prix d'une
expurgation structurelle fiable.

## 7. Promotion d'une fixture expurgée

1. Vérifier le fichier écrit : ouvrir `probe_*.json` et confirmer qu'aucun
   numéro de série, nom d'hôte, adresse ou identifiant ne subsiste.
2. Masquer le numéro de série de l'appareil s'il apparaît sous une forme non
   prévue par l'expurgation, puis remplacer le champ racine `_provenance` par :

   ```json
   "_provenance": {
     "synthetic": false,
     "source": "fronius-probe",
     "confidence": "VÉRIFIÉ (matériel)",
     "firmware": "1.19.7-1",
     "captured_at": "<horodatage UTC de la capture>",
     "serial_number": "<masqué>"
   }
   ```

3. Renommer selon le cas observé (`powerflow_produce_only.json`,
   `powerflow_with_meter.json`, …) et remplacer la fixture synthétique
   correspondante.
4. Relancer `go test ./...` : le test de contrat
   (`internal/fronius/solarapi/contract_test.go`) échouera sur les valeurs
   attendues. **C'est le but** : mettre à jour les valeurs attendues *après*
   vérification, jamais pour faire passer le test.
5. Mettre à jour `docs/SOURCES.md` (statut de la capture, firmware, date) et
   consigner le changement dans `CHANGELOG.md`.
6. Réévaluer les capacités dans `internal/model/capabilities.go` : les mentions
   « Schéma non validé sur firmware 1.19.7-1 » peuvent être retirées seulement
   pour ce qui a été effectivement observé.

Une fixture promue devient la référence contractuelle du projet. Toute capture
ultérieure doit être comparée à elle avant d'être adoptée.

## 8. Règle de non-écriture

**Aucune commande `POST`, `PUT`, `PATCH` ou `DELETE` ne doit être émise vers
l'onduleur tant qu'une revue de sécurité dédiée n'a pas autorisé un endpoint
nommé.**

Cette règle couvre :

- l'application (`froniusd`) : tous ses clients sont construits derrière
  `fronius.ReadOnly` ;
- la sonde (`fronius-probe`) : liste blanche d'un seul GET ;
- le simulateur (`fronius-sim`) : il ne contacte jamais l'onduleur ;
- les tests et la CI : aucun test n'effectue de requête sortante vers un
  onduleur.

Les endpoints `MUTATION` et `DANGEROUS` sont inventoriés et classés dans
`docs/ENDPOINTS.md`, avec pour seul objectif de **savoir ce qu'il ne faut pas
appeler**. L'inventaire n'est pas une autorisation.

La phase 2 pourra élargir la liste blanche à des lectures d'API interne
(`/status/*`, `/config/*` en GET). Chaque ajout devra être :

1. justifié par une métrique que la Solar API ne fournit pas ;
2. documenté dans `docs/ENDPOINTS.md` (rôle déclaré, rôle testé, effet de bord) ;
3. accompagné d'un test qui prouve que la méthode est bien `GET` ;
4. autorisé explicitement par l'utilisateur avant la première exécution sur le
   matériel.
