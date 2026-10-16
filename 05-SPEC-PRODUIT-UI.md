# Spécification produit / écrans

## Écran 1 — Vue d'ensemble
- KPI : production instantanée (W/kW), énergie du jour (kWh), énergie totale si exposée, état de l'onduleur, dernier rafraîchissement.
- Courbe puissance PV sur 24 h, zoom/pan, info-bulles avec unités, sélecteur 24 h / 7 j / 30 j / personnalisé.
- Répartition d'énergie **uniquement si mesures disponibles** ; sans Smart Meter, masquer consommation maison, import/export réseau et autoconsommation réelle.
- État de connexion et badge « données périmées ».

## Écran 2 — Production & historique
- Courbes W, agrégats kWh par jour/semaine/mois/année, comparaisons de périodes, calendrier de production, export CSV.
- Gestion des données manquantes (trous visibles, pas d'interpolation silencieuse), heure d'été/hiver, granularité adaptative.

## Écran 3 — Onduleur
- Modèle, série (masquée en mode partage), firmware, WebUI, connectivité, états opérationnels, diagnostics disponibles.
- Tensions/courants DC/AC, fréquence et températures **seulement si exposés et confirmés**.

## Écran 4 — Événements et santé
- Historique des alertes/états, filtres de gravité, période, acquittement **local uniquement** au MVP, export.
- État du collecteur, derniers succès/échecs, latence et taux de données manquantes.

## Écran 5 — Paramètres
- Adresse de l'onduleur, intervalle de collecte, rétention, langue, fuseau, unités, sauvegarde/restauration de la base.
- Tester la connectivité en lecture seule ; gestion sécurisée des secrets.

## Écran 6 — Capacités et contrôle (phase ultérieure)
- Afficher droits vérifiés et non vérifiés, mode lecture seule, explication des fonctions indisponibles.
- Boutons d'écriture uniquement après autorisation, revue et tests dédiés ; confirmation et journal d'audit.

## Design system
- Navigation latérale desktop / menu mobile, cartes shadcn, typographie lisible, contraste AA, skeletons, tooltips, légendes, palette accessible.
- Graphiques Recharts (ligne/aire/barres), pas de camembert sans données pertinentes, export PNG/CSV si demandé.
- Tuiles cliquables vers détails, raccourcis de période, affichage unités et sources de mesure.

## Critères d'acceptation MVP
- Affichage en temps réel sur réseau local avec latence et fréquence documentées.
- Courbe 24 h persistante après redémarrage ; périodes vides gérées proprement.
- Aucune valeur de consommation inventée ; aucun mot de passe exposé au navigateur.
- Fonctionne avec onduleur hors ligne (message clair et historique conservé).
- Responsive et accessible clavier ; tests automatisés UI + API.
