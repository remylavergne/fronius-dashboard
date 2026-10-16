# Sécurité, limites et plan de validation

## Interdits par défaut
- Aucun POST/PUT/PATCH/DELETE vers le GEN24 sans approbation explicite et revue du endpoint.
- Aucune tentative de signature de récupération, brute force de mot de passe, ni manipulation de `gridcode`, `powerlimits`, relais, modes de secours, mise à jour firmware.
- Aucun stockage de mot de passe dans localStorage, logs, fixtures commitées ou exports publics.
- Ne pas réutiliser les clés de déverrouillage setup comme identifiants Technician.

## Tests
- Tests unitaires : parser Solar API, unités, énergie vs puissance, séries temporelles, erreurs, calculs d'agrégats, passage heure d'été.
- Tests HTTP : 200, 401 Digest challenge, nonce renouvelé, 403, 404, 429, 5xx, timeout, connexion coupée, JSON invalide, valeurs nulles.
- Tests sécurité : secrets absents des logs, CORS restreint, authentification interface si accessible au-delà de localhost, limites de requêtes, validation des paramètres.
- Tests UI : états sans Smart Meter, données manquantes, offline, longue période, mobile, lecteur d'écran.
- Tests intégration : simulateur/mock Fronius à partir de fixtures anonymisées ; pas d'accès matériel requis en CI.
- Tests manuels sur matériel : **GET uniquement**, fréquence raisonnable, validation de résultats, capture de preuves expurgées.

## Règles de confiance
- `[SOURCE-JS]` : prouvé dans bundle fourni ; peut différer sur d'autres firmwares.
- `[SOURCE-PDF]` : export système horodaté.
- `[OBSERVÉ-CONVERSATION]` : résultat rapporté d'un test antérieur ; conserver les preuves si nécessaire.
- `[HYPOTHÈSE]` : proposition non validée.
- `[À-TESTER]` : jamais présenté comme fonctionnel.

## Politique de données
- Retenir puissance W, énergie Wh/kWh, timestamps UTC, source, qualité, état d'acquisition.
- Ne pas stocker de mot de passe dans l'historique ; masquer IP et série dans exports destinés au public.
- Documenter conservation et suppression des données.
