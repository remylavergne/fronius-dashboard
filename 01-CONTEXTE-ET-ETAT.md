# Contexte et état vérifié

## Matériel et réseau

- [SOURCE-PDF] Modèle : **Fronius Primo GEN24 3.6 Plus** ; puissance nominale **3680 W** ; numéro de série **33319328**.
- [SOURCE-PDF] Firmware `GEN24` / `GEN24PRIMO` / `DEVICEGROUP` **1.19.7-1** ; WebUI **1.14.2-1**.
- [SOURCE-PDF] Wi-Fi connecté, IP **192.0.2.10**, masque **255.255.254.0**, passerelle/DNS **192.0.2.1** ; Ethernet déconnecté au moment de l'export.
- [SOURCE-PDF] Licences/active features : `Battery Operation`, `Full Backup` ; **une licence active ne prouve pas qu'une batterie est physiquement installée**.
- [OBSERVÉ-CONVERSATION] Pas de Fronius Smart Meter détecté et pas de batterie installée. Adresse AP rapportée `192.168.250.181`.

## Accès et objectifs

- [OBSERVÉ-CONVERSATION] Compte `customer` fonctionnel ; mot de passe `technician` oublié ; navigateur actuel neuf, `AuthHash` contient uniquement Customer.
- [OBSERVÉ-CONVERSATION] Solar API active ; `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` répond.
- [OBSERVÉ-CONVERSATION] Lecture Customer HTTP 200 sur `/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping` ; `/config/powerlimits`, `/config/setup/powerunit`, `/config/setup/gridcode/general` ont renvoyé `{}`.
- [OBSERVÉ-CONVERSATION] Permissions déclarées dans la réponse `/config/modbus` : lecture/écriture `RoleCustomer` et `RoleTechnician` pour `_master`, `_slave`. Dans `/config/meter`, écriture `_primary` et `_secondary` réservée `RoleTechnician` ; dans `/config/iomapping`, `_features` Customer/Technician lecture et écriture. **Métadonnées de permissions ≠ validation d'une opération POST.**
- [OBSERVÉ-CONVERSATION] PasswordRecovery propose un challenge à six chiffres et un compteur d'environ cinq tentatives de signature ; ne pas consommer ces tentatives.

## Objectifs

1. Interface moderne de supervision **autonome par rapport au frontend Angular Fronius** (pas nécessairement indépendante du firmware/API Fronius).
2. Backend Go pour acquisition, normalisation, historisation, API et éventuelles commandes autorisées.
3. Frontend React + TypeScript + shadcn/ui + graphiques responsives.
4. Cartographie technique des API et rôles ; comprendre les possibilités de récupération légitime de Technician, sans brute-force.

## Limites / inconnues

- Pas de spécification officielle complète des endpoints internes.
- Version firmware ancienne possible : vérifier les changements avant tout upgrade, sans le déclencher automatiquement.
- Pas de consommation maison, d'import/export réseau ni d'autoconsommation fiables sans mesure dédiée.
- Pas de preuve que le backend autorise les écritures accessibles dans les écrans Customer.
- Pas de clé de signature de récupération Technician dans les éléments identifiés.
