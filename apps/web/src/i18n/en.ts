/**
 * English translations.
 *
 * Deliberately flat dictionary: no i18n library is needed as long as a single
 * language is served. The structure (`Record<string, string>` indexed by dotted
 * key, derived types) is ready for adding `fr` and `de` without changing the
 * components.
 */
export const en = {
  'app.title': 'Fronius Monitoring',
  'app.tagline': 'Local monitoring of a Fronius GEN24 inverter',

  'nav.aria': 'Main navigation',
  'nav.overview': 'Overview',
  'nav.system': 'System',
  'nav.capabilities': 'Capabilities',
  'nav.settings': 'Settings',

  'kpi.production': 'Instantaneous production',
  'kpi.energyToday': 'Energy of the day',
  'kpi.energyYear': 'Energy of the year',
  'kpi.energyTotal': 'Total energy',
  'kpi.energyDerived': 'estimated from the total',
  'kpi.collector': 'Collector',
  'kpi.samples': 'Recorded samples',
  'kpi.lastSample': 'Last measurement',
  'kpi.lastSuccess': 'Last success',
  'kpi.uptime': 'Uptime',
  'kpi.runDuration': 'Duration of the last cycle',

  'chart.title': 'Produced power',
  'chart.range.24h': '24 hours',
  'chart.range.7d': '7 days',
  'chart.range.30d': '30 days',
  'chart.ariaRange': 'Displayed period',
  'chart.empty': 'No data for this period',
  'chart.noPower': 'No power value in this period.',
  'chart.range.custom': 'Custom',
  'chart.from': 'Start',
  'chart.to': 'End',
  'chart.step': 'Step',
  'chart.invalidRange': 'Invalid range: the end must be after the start.',
  'chart.stepHelp': 'auto lets the server choose the finest step (at most 1500 points).',

  'state.loading': 'Loading…',
  'state.error': 'API error',
  'state.offline': 'API unreachable: monitoring may be stopped.',
  'state.offlineHint': 'Start it with `make dev-api`, then reload the page.',
  'state.empty': 'No data',
  'state.emptyHint': 'No measurement has been recorded yet for this inverter.',
  'state.stale': 'Stale data',
  'state.fresh': 'Fresh data',
  'state.retry': 'Retry',
  'state.unavailable': 'Unavailable',
  'state.notFound': 'Unknown page',
  'state.notFoundHint': 'This address does not match any page of the interface.',
  'state.present': 'present',
  'state.absent': 'absent',
  'state.unknown': 'unknown',

  'quality.measured': 'measured',
  'quality.estimated': 'estimated',
  'quality.missing': 'missing',

  'capabilities.title': 'Monitoring capabilities',
  'capabilities.intro':
    'What this application can actually render with the currently validated hardware and protocols. An unavailable capability never displays a zero value: it states what is missing.',
  'capabilities.col.metric': 'Quantity',
  'capabilities.col.status': 'Status',
  'capabilities.col.quality': 'Quality',
  'capabilities.col.requires': 'Prerequisite',
  'capabilities.col.evidence': 'Evidence',
  'capabilities.col.reason': 'Reason',
  'capabilities.available': 'available',
  'capabilities.unavailable': 'unavailable',
  'capabilities.verified': 'verified',
  'capabilities.unverified': 'unverified',
  'capabilities.readOnly': 'read-only',

  'nav.sources': 'Data sources',
  'sources.title': 'Data sources',
  'sources.intro':
    'The whole catalogue of what this inverter answers, what this project does with each field of each answer, and what is still unmapped. A source is one endpoint or one query variant of an endpoint; a field is one value inside its answer. Counters come from the same list the backend returns. The legend at the end of the page defines both vocabularies: a field counted as ignored is a decision, a field counted as unverified is an absence of evidence.',

  'sources.summary.title': 'Catalogue summary',
  'sources.summary.sources': 'Sources',
  'sources.summary.sourcesHint': 'endpoints and query variants catalogued',
  'sources.summary.fields': 'Fields',
  'sources.summary.fieldsHint': 'values named by the catalogue, all sources together',

  'sources.state.called': 'called',
  'sources.state.calledDescription': 'read by this project today',
  'sources.state.captured': 'captured',
  'sources.state.capturedDescription':
    'not read, but a redacted answer was captured on the reference hardware',
  'sources.state.candidate': 'candidate',
  'sources.state.candidateDescription':
    'documented or read by the manufacturer interface, never observed on this firmware',
  'sources.state.archived': 'archived',
  'sources.state.archivedDescription':
    'was read once, its capture is kept, and it is no longer used',

  'sources.status.displayed': 'displayed',
  'sources.status.displayedDescription': 'read into the model and shown by the interface',
  'sources.status.stored': 'stored',
  'sources.status.storedDescription': 'read and persisted, whether or not it is displayed',
  'sources.status.validated': 'validated',
  'sources.status.validatedDescription': 'read to accept or reject the payload, never shown',
  'sources.status.ignored': 'ignored',
  'sources.status.ignoredDescription': 'present in the answer and deliberately not used',
  'sources.status.unverified': 'unverified',
  'sources.status.unverifiedDescription':
    'never observed carrying a value on the reference installation',

  'sources.transport.solarApi': 'Solar API v1',
  'sources.transport.internalApi': 'Internal API',

  'sources.filter.transport': 'Transport',
  'sources.filter.state': 'State',
  'sources.filter.search': 'Search',
  'sources.filter.searchPlaceholder': 'Path, what it provides, or a field path',
  'sources.filter.allTransports': 'All transports',
  'sources.filter.allStates': 'All states',
  'sources.filter.visible': 'Sources shown',
  'sources.filter.none': 'No source matches these filters',
  'sources.filter.noneHint': 'Clear the search, or widen the transport and state filters.',

  'sources.called': 'read by this project',
  'sources.id': 'Source id',
  'sources.method': 'Method',
  'sources.fixture': 'Captured fixture',
  'sources.proofs': 'Proofs',
  'sources.firmware': 'firmware',
  'sources.docs': 'References',
  'sources.note': 'Note',
  'sources.capability': 'Capability',
  'sources.candidateCapability': 'Candidate capability',
  'sources.unmapped': 'Fields ignored or unverified',

  'sources.fields.inventory': 'Field inventory',
  'sources.fields.none': 'No field is inventoried for this source.',
  'sources.fields.path': 'Field',
  'sources.fields.type': 'Type',
  'sources.fields.status': 'Status',
  'sources.fields.why': 'Why',
  'sources.fields.capability': 'Capability',

  'sources.legend.title': 'Legend',
  'sources.legend.intro':
    'Two vocabularies, deliberately distinct: the state says what this project does with a source, the field status what it does with one value inside the answer.',
  'sources.legend.states': 'Source states',
  'sources.legend.statuses': 'Field statuses',

  'system.title': 'System',
  'system.application': 'Monitoring application',
  'system.config': 'Effective configuration',
  'system.device': 'Inverter',
  'system.identity': 'Identity',
  'system.model': 'Model',
  'system.deviceName': 'Device name',
  'system.firmware': 'Firmware',
  'system.serialNumber': 'Serial number',
  'system.articleNumber': 'Article number',
  'system.source': 'Source',
  'system.readAt': 'Read at',
  'system.detection': 'Hardware detection',
  'system.resource.meter': 'Meter',
  'system.resource.battery': 'Battery',
  'system.resource.internalApi': 'Internal API',
  'system.resource.deviceEvents': 'Events',
  'system.lastError': 'Last detection error',
  'system.noIdentity':
    'Identity not determined: no successful read. See the detection reason below.',
  'system.status': 'Collector status',

  'settings.title': 'Settings',
  'settings.intro': 'Effective configuration, read-only.',
  'settings.readOnlyNote':
    'Editing will be added with ADR 0005 (authentication and remote access): the REST API has no authentication and therefore accepts no write. The inverter credentials themselves are used only by the service to read the device.',
  'settings.field.version': 'Application version',
  'settings.field.goVersion': 'Go version',
  'settings.field.solarapiUrl': 'Solar API URL',
  'settings.field.apiAddr': 'API listen address',
  'settings.field.pollInterval': 'Collection interval',
  'settings.field.staleAfter': 'Staleness threshold',
  'settings.field.tz': 'Time zone',
  'settings.field.dbPath': 'Database',
  'settings.field.retention': 'Retention (days)',
  'settings.retentionUnlimited': '0 — unlimited retention',

  'health.ok': 'operational',
  'health.degraded': 'degraded',
  'health.error': 'error',
  'health.db': 'Database',
  'health.collector': 'Collector',
  'health.lastError': 'Last error',

  'footer.sources': 'Local data, measurement produced by the Solar API v1.',
} as const

export type TranslationKey = keyof typeof en
export type Dictionary = Partial<Record<TranslationKey, string>>
