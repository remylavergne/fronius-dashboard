/**
 * Traductions françaises.
 *
 * Dictionnaire plat volontairement : aucune librairie i18n n'est nécessaire
 * tant qu'une seule langue est servie. La structure (`Record<string, string>`
 * indexé par clé pointée, types dérivés) est prête pour ajouter `en` et `de`
 * sans changer les composants.
 */
export const fr = {
  'app.title': 'Supervision Fronius',
  'app.tagline': 'Supervision locale d’un onduleur Fronius GEN24',

  'nav.aria': 'Navigation principale',
  'nav.overview': 'Vue d’ensemble',
  'nav.system': 'Système',
  'nav.capabilities': 'Capacités',
  'nav.settings': 'Paramètres',

  'kpi.production': 'Production instantanée',
  'kpi.energyToday': 'Énergie du jour',
  'kpi.energyYear': 'Énergie de l’année',
  'kpi.energyTotal': 'Énergie totale',
  'kpi.collector': 'Collecteur',
  'kpi.samples': 'Échantillons enregistrés',
  'kpi.lastSample': 'Dernière mesure',
  'kpi.lastSuccess': 'Dernier succès',
  'kpi.uptime': 'Durée de fonctionnement',
  'kpi.runDuration': 'Durée du dernier cycle',

  'chart.title': 'Puissance produite',
  'chart.range.24h': '24 heures',
  'chart.range.7d': '7 jours',
  'chart.range.30d': '30 jours',
  'chart.ariaRange': 'Période affichée',
  'chart.empty': 'Aucune donnée sur cette période',
  'chart.noPower': 'Aucune valeur de puissance dans cette période.',
  'chart.range.custom': 'Personnalisé',
  'chart.from': 'Début',
  'chart.to': 'Fin',
  'chart.step': 'Pas',
  'chart.invalidRange': 'Plage invalide : la fin doit être postérieure au début.',
  'chart.stepHelp': 'auto laisse le serveur choisir le pas le plus fin (au plus 1500 points).',

  'state.loading': 'Chargement…',
  'state.error': 'Erreur de l’API',
  'state.offline': 'API injoignable : la supervision est peut-être arrêtée.',
  'state.offlineHint': 'Démarrez-la avec `make dev-api`, puis rechargez la page.',
  'state.empty': 'Aucune donnée',
  'state.emptyHint': 'Aucune mesure n’a encore été enregistrée pour cet onduleur.',
  'state.stale': 'Données périmées',
  'state.fresh': 'Données à jour',
  'state.retry': 'Réessayer',
  'state.unavailable': 'Indisponible',
  'state.notFound': 'Page inconnue',
  'state.notFoundHint': 'Cette adresse ne correspond à aucune page de l’interface.',
  'state.present': 'présent',
  'state.absent': 'absent',
  'state.unknown': 'non déterminé',

  'quality.measured': 'mesuré',
  'quality.estimated': 'estimé',
  'quality.missing': 'absent',

  'capabilities.title': 'Capacités de la supervision',
  'capabilities.intro':
    'Ce que cette application peut réellement restituer avec le matériel et les protocoles actuellement validés. Une capacité indisponible n’affiche jamais de valeur nulle : elle indique ce qui manque.',
  'capabilities.col.metric': 'Grandeur',
  'capabilities.col.status': 'État',
  'capabilities.col.quality': 'Qualité',
  'capabilities.col.requires': 'Prérequis',
  'capabilities.col.evidence': 'Preuve',
  'capabilities.col.reason': 'Motif',
  'capabilities.available': 'disponible',
  'capabilities.unavailable': 'indisponible',
  'capabilities.verified': 'vérifié',
  'capabilities.unverified': 'non vérifié',
  'capabilities.readOnly': 'lecture seule',

  'system.title': 'Système',
  'system.application': 'Application de supervision',
  'system.config': 'Configuration effective',
  'system.device': 'Onduleur',
  'system.identity': 'Identité',
  'system.model': 'Modèle',
  'system.firmware': 'Firmware',
  'system.serialNumber': 'Numéro de série',
  'system.source': 'Source',
  'system.readAt': 'Lecture',
  'system.detection': 'Détection matérielle',
  'system.resource.meter': 'Compteur',
  'system.resource.battery': 'Batterie',
  'system.resource.internalApi': 'API interne',
  'system.resource.deviceEvents': 'Événements',
  'system.lastError': 'Dernière erreur de détection',
  'system.noIdentity':
    'Identité non déterminée : aucune lecture aboutie. Voir le motif de la détection ci-dessous.',
  'system.status': 'État du collecteur',

  'settings.title': 'Paramètres',
  'settings.intro': 'Configuration effective, en lecture seule.',
  'settings.readOnlyNote':
    'L’édition sera ajoutée avec l’ADR 0005 (authentification et accès distant) : l’API REST n’a aucune authentification et n’accepte donc aucune écriture. Les identifiants de l’onduleur, eux, servent uniquement au service pour lire l’appareil.',
  'settings.field.version': 'Version de l’application',
  'settings.field.goVersion': 'Version de Go',
  'settings.field.solarapiUrl': 'URL de la Solar API',
  'settings.field.apiAddr': 'Adresse d’écoute de l’API',
  'settings.field.pollInterval': 'Intervalle de collecte',
  'settings.field.staleAfter': 'Seuil de péremption',
  'settings.field.tz': 'Fuseau',
  'settings.field.dbPath': 'Base de données',
  'settings.field.retention': 'Rétention (jours)',
  'settings.retentionUnlimited': '0 — conservation illimitée',

  'health.ok': 'opérationnel',
  'health.degraded': 'dégradé',
  'health.error': 'en erreur',
  'health.db': 'Base de données',
  'health.collector': 'Collecteur',
  'health.lastError': 'Dernière erreur',

  'footer.sources': 'Données locales, mesure produite par la Solar API v1.',
} as const

export type TranslationKey = keyof typeof fr
export type Dictionary = Partial<Record<TranslationKey, string>>
