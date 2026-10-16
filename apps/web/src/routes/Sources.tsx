import { useMemo, useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { ApiAlert } from '@/components/ApiAlert'
import { EmptyState } from '@/components/EmptyState'
import { KpiCard } from '@/components/KpiCard'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { useCapabilities, useSources } from '@/lib/queries'
import type { Capability, Source, SourceCatalogue, SourceSummary } from '@/lib/schema'
import { cn } from '@/lib/utils'

/**
 * One word of the page's vocabulary: the value the API sends, its label, the
 * sentence that explains it, and the badge that carries it.
 *
 * The tables below are keyed by plain string, never by a union type: the payload
 * sends strings, and a value this page does not know yet must still render — as
 * its raw token, with the neutral badge — rather than take the whole catalogue
 * down. The label is what carries the meaning; the colour only speeds up
 * scanning.
 */
type VocabularyEntry = {
  label: TranslationKey
  description: TranslationKey
  /** Extra badge classes, applied on top of the `outline` variant. */
  badge: string
}

type StateEntry = VocabularyEntry & { count: keyof SourceSummary }

/**
 * The four source states, in the order the backend counts them: the insertion
 * order of this record is the order of the summary strip and of the legend.
 */
const STATE_BY_ID: Record<string, StateEntry> = {
  called: {
    label: 'sources.state.called',
    description: 'sources.state.calledDescription',
    count: 'called',
    badge: 'border-transparent bg-primary text-primary-foreground',
  },
  captured: {
    label: 'sources.state.captured',
    description: 'sources.state.capturedDescription',
    count: 'captured',
    badge: 'border-transparent bg-chart-2 text-background',
  },
  candidate: {
    label: 'sources.state.candidate',
    description: 'sources.state.candidateDescription',
    count: 'candidates',
    badge: 'border-border bg-background text-foreground',
  },
  archived: {
    label: 'sources.state.archived',
    description: 'sources.state.archivedDescription',
    count: 'archived',
    badge: 'border-transparent bg-muted text-muted-foreground',
  },
}

/**
 * The five field statuses.
 *
 * `ignored` and `unverified` deliberately do not look the same: the first is a
 * decision taken on a value the device does send, the second an absence of
 * evidence — the legend spells both out.
 */
const STATUS_BY_ID: Record<string, VocabularyEntry> = {
  displayed: {
    label: 'sources.status.displayed',
    description: 'sources.status.displayedDescription',
    badge: 'border-transparent bg-primary text-primary-foreground',
  },
  stored: {
    label: 'sources.status.stored',
    description: 'sources.status.storedDescription',
    badge: 'border-transparent bg-chart-2 text-background',
  },
  validated: {
    label: 'sources.status.validated',
    description: 'sources.status.validatedDescription',
    badge: 'border-transparent bg-secondary text-secondary-foreground',
  },
  ignored: {
    label: 'sources.status.ignored',
    description: 'sources.status.ignoredDescription',
    badge: 'border-transparent bg-muted text-muted-foreground',
  },
  unverified: {
    label: 'sources.status.unverified',
    description: 'sources.status.unverifiedDescription',
    badge: 'border-dashed border-border bg-background text-muted-foreground',
  },
}

/** The two HTTP surfaces: documented Solar API v1, Digest-protected internal API. */
const TRANSPORT_BY_ID: Record<string, TranslationKey> = {
  solar_api: 'sources.transport.solarApi',
  internal_api: 'sources.transport.internalApi',
}

/** The five status counters of the summary, and the payload key each one reads. */
const FIELD_COUNT_KEYS: Record<string, keyof SourceSummary> = {
  displayed: 'fields_displayed',
  stored: 'fields_stored',
  validated: 'fields_validated',
  ignored: 'fields_ignored',
  unverified: 'fields_unverified',
}

const ALL = 'all'

/** Native controls, styled with the same tokens as the rest of the interface. */
const CONTROL_CLASS =
  'h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring'

/**
 * Capability chip.
 *
 * The id is resolved against `/api/v1/capabilities`: a row shows its label, and
 * an unavailable capability shows its reason next to it — an unavailable
 * capability is never rendered as a value. An id without a row is displayed as
 * it arrived, in monospace, since nothing can be asserted about it.
 */
function CapabilityChip({ id, capabilities }: { id: string; capabilities: Record<string, Capability> }) {
  const { t } = useT()
  const capability = capabilities[id]

  if (!capability) {
    return (
      <Badge variant="outline" className="font-mono">
        {id}
      </Badge>
    )
  }

  return (
    <span className="inline-flex flex-wrap items-baseline gap-1.5">
      <Badge variant={capability.available ? 'secondary' : 'outline'}>{capability.label}</Badge>
      {capability.available ? null : (
        <span className="text-xs text-muted-foreground">
          {capability.reason === ''
            ? t('capabilities.unavailable')
            : `${t('capabilities.unavailable')}: ${capability.reason}`}
        </span>
      )}
    </span>
  )
}

/**
 * Candidate capability chip.
 *
 * `would_unlock` names a capability that does **not** exist yet: it is what the
 * source would fill once wired, so it is never joined against
 * `/api/v1/capabilities` and never rendered as an available or unavailable
 * capability — it is a candidate, and the chip says so.
 */
function CandidateChip({ id }: { id: string }) {
  const { t } = useT()
  return (
    <Badge variant="outline" className="border-dashed">
      {t('sources.candidateCapability')}
      {' · '}
      <span className="font-mono">{id}</span>
    </Badge>
  )
}

/** One source: identity, provenance, and the inventory of its fields. */
function SourceBlock({
  source,
  capabilities,
}: {
  source: Source
  capabilities: Record<string, Capability>
}) {
  const { t } = useT()
  const [expanded, setExpanded] = useState(false)

  const fields = source.fields ?? []
  const proofs = source.proofs ?? []
  const docs = source.docs ?? []
  const state = STATE_BY_ID[source.state]
  const transportLabel = TRANSPORT_BY_ID[source.transport]
  // Address as it is called: the path, plus its query when it takes one.
  const address = source.query ? `${source.path}?${source.query}` : source.path
  // Domain id to DOM id: dots and slashes are legal in an `id` but make the
  // markup unreadable and defeat any `querySelector`-style debugging.
  const titleId = `source-${source.id.replace(/[^a-zA-Z0-9]+/g, '-')}`
  const inventoryId = `${titleId}-fields`

  // The two statuses that leave a value unused: `ignored` by decision,
  // `unverified` by absence of evidence. Counting them together is what makes
  // the head of the page actionable.
  const unmapped = fields.filter((field) => field.status === 'ignored' || field.status === 'unverified').length

  return (
    <Card role="article" aria-labelledby={titleId}>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle id={titleId} className="font-mono text-sm break-all">
            {address}
          </CardTitle>
          <Badge variant="outline" className={state?.badge}>
            {state ? t(state.label) : source.state}
          </Badge>
          {source.called ? (
            <Badge variant="outline" className="border-transparent bg-accent text-accent-foreground">
              {t('sources.called')}
            </Badge>
          ) : null}
        </div>
        <CardDescription>{source.provides}</CardDescription>
      </CardHeader>

      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
          <Badge variant="outline">{transportLabel ? t(transportLabel) : source.transport}</Badge>
          <span className="text-muted-foreground">
            {t('sources.id')}: <span className="font-mono text-foreground">{source.id}</span>
          </span>
          <span className="text-muted-foreground">
            {t('sources.method')}: <span className="font-mono text-foreground">{source.method}</span>
          </span>
          {source.capability ? (
            <span className="flex flex-wrap items-baseline gap-1.5 text-muted-foreground">
              {t('sources.capability')}:
              <CapabilityChip id={source.capability} capabilities={capabilities} />
            </span>
          ) : null}
          {source.would_unlock ? <CandidateChip id={source.would_unlock} /> : null}
        </div>

        {source.fixture ? (
          <p className="text-xs text-muted-foreground">
            {t('sources.fixture')}: <span className="font-mono">{source.fixture}</span>
          </p>
        ) : null}

        {proofs.length > 0 ? (
          <div className="text-xs">
            <p className="font-medium text-muted-foreground">{t('sources.proofs')}</p>
            <ul className="mt-1 space-y-1">
              {proofs.map((proof, index) => (
                <li key={`${proof.tag}-${String(index)}`}>
                  <span className="font-mono">{proof.tag}</span>
                  {proof.firmware ? (
                    <span className="text-muted-foreground">
                      {' — '}
                      {t('sources.firmware')} <span className="font-mono">{proof.firmware}</span>
                    </span>
                  ) : null}
                  <span className="mt-0.5 block text-muted-foreground">{proof.detail}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}

        {docs.length > 0 ? (
          <div className="text-xs">
            <p className="font-medium text-muted-foreground">{t('sources.docs')}</p>
            <ul className="mt-1 list-inside list-disc space-y-0.5 text-muted-foreground">
              {docs.map((doc) => (
                <li key={doc}>{doc}</li>
              ))}
            </ul>
          </div>
        ) : null}

        {source.note ? (
          <p className="text-xs text-muted-foreground">
            <span className="font-medium">{t('sources.note')}: </span>
            {source.note}
          </p>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            {`${t('sources.unmapped')}: ${String(unmapped)} / ${String(fields.length)}`}
          </p>
          <button
            type="button"
            aria-expanded={expanded}
            aria-controls={inventoryId}
            onClick={() => setExpanded((value) => !value)}
            className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          >
            <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', expanded && 'rotate-180')} />
            {`${t('sources.fields.inventory')} (${String(fields.length)})`}
          </button>
        </div>

        {/*
          The inventory stays in the document and is hidden by the `hidden`
          attribute: `aria-controls` therefore always names an element that
          exists, and whether it is disclosed stays readable through
          `aria-expanded`.
        */}
        <div id={inventoryId} hidden={!expanded}>
          {fields.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('sources.fields.none')}</p>
          ) : (
            <Table>
              <TableCaption className="sr-only">{`${t('sources.fields.inventory')} — ${address}`}</TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">{t('sources.fields.path')}</TableHead>
                  <TableHead scope="col">{t('sources.fields.type')}</TableHead>
                  <TableHead scope="col">{t('sources.fields.status')}</TableHead>
                  <TableHead scope="col">{t('sources.fields.why')}</TableHead>
                  <TableHead scope="col">{t('sources.fields.capability')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {fields.map((field) => {
                  const status = STATUS_BY_ID[field.status]
                  return (
                    <TableRow key={field.path}>
                      <TableCell className="font-mono text-xs break-all">{field.path}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{field.type}</TableCell>
                      <TableCell>
                        <Badge variant="outline" className={status?.badge}>
                          {status ? t(status.label) : field.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{field.why ? field.why : '—'}</TableCell>
                      <TableCell>
                        {field.capability ? (
                          <CapabilityChip id={field.capability} capabilities={capabilities} />
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

/**
 * Counters of the catalogue.
 *
 * They come from the payload, never from a recount on the client: the backend
 * summarises exactly the list it returns, so a divergence between the two is a
 * bug on one side rather than something the page hides.
 */
function SummaryStrip({ summary }: { summary: SourceSummary }) {
  const { t } = useT()

  return (
    <section aria-labelledby="sources-summary-heading" className="space-y-4">
      <h2 id="sources-summary-heading" className="sr-only">
        {t('sources.summary.title')}
      </h2>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <KpiCard
          label={t('sources.summary.sources')}
          value={{ value: String(summary.sources), unit: '' }}
          hint={t('sources.summary.sourcesHint')}
        />
        {Object.values(STATE_BY_ID).map((entry) => (
          <KpiCard
            key={entry.label}
            label={t(entry.label)}
            value={{ value: String(summary[entry.count]), unit: '' }}
            hint={t(entry.description)}
          />
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardDescription>{t('sources.summary.fields')}</CardDescription>
          <CardTitle className="flex flex-wrap items-baseline gap-2 text-2xl tabular-nums">
            <span>{summary.fields}</span>
            <span className="text-xs font-normal text-muted-foreground">{t('sources.summary.fieldsHint')}</span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="flex flex-wrap gap-x-6 gap-y-2 text-xs">
            {Object.entries(FIELD_COUNT_KEYS).map(([status, count]) => {
              const entry = STATUS_BY_ID[status]
              return (
                <div key={status} className="flex items-baseline gap-2">
                  <dt className="text-muted-foreground">{entry ? t(entry.label) : status}</dt>
                  <dd className="tabular-nums">{summary[count]}</dd>
                </div>
              )
            })}
          </dl>
        </CardContent>
      </Card>
    </section>
  )
}

/**
 * The page's own vocabulary.
 *
 * It is not decoration: `ignored` (a decision) and `unverified` (an absence of
 * evidence) are two different states of knowledge, and the four source states
 * say what this project does — or does not do — with an endpoint. Without this
 * block the badges would be a private code.
 */
function Legend() {
  const { t } = useT()

  return (
    <Card role="region" aria-labelledby="sources-legend-heading">
      <CardHeader>
        <CardTitle id="sources-legend-heading">{t('sources.legend.title')}</CardTitle>
        <CardDescription>{t('sources.legend.intro')}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6 md:grid-cols-2">
        <div>
          <h3 className="text-xs font-medium text-muted-foreground">{t('sources.legend.states')}</h3>
          <dl className="mt-2 space-y-2">
            {Object.values(STATE_BY_ID).map((entry) => (
              <div key={entry.label} className="flex flex-wrap items-baseline gap-2">
                <dt>
                  <Badge variant="outline" className={entry.badge}>
                    {t(entry.label)}
                  </Badge>
                </dt>
                <dd className="text-xs text-muted-foreground">{t(entry.description)}</dd>
              </div>
            ))}
          </dl>
        </div>
        <div>
          <h3 className="text-xs font-medium text-muted-foreground">{t('sources.legend.statuses')}</h3>
          <dl className="mt-2 space-y-2">
            {Object.values(STATUS_BY_ID).map((entry) => (
              <div key={entry.label} className="flex flex-wrap items-baseline gap-2">
                <dt>
                  <Badge variant="outline" className={entry.badge}>
                    {t(entry.label)}
                  </Badge>
                </dt>
                <dd className="text-xs text-muted-foreground">{t(entry.description)}</dd>
              </div>
            ))}
          </dl>
        </div>
      </CardContent>
    </Card>
  )
}

/**
 * Data sources: the catalogue of what the inverter answers, what this project
 * does with each field, and what is still unmapped.
 *
 * The capabilities endpoint is joined in, not duplicated: a source capability
 * that exists shows its label (and its reason when it is unavailable), while a
 * `would_unlock` id is by construction absent from that list and is displayed
 * as the candidate it is.
 */
export function SourcesPage() {
  const { t } = useT()
  const sources = useSources()
  const capabilities = useCapabilities()

  const [transport, setTransport] = useState(ALL)
  const [state, setState] = useState(ALL)
  const [needle, setNeedle] = useState('')

  const catalogue: SourceCatalogue | undefined = sources.data?.data
  const capabilityIndex: Record<string, Capability> = useMemo(
    () =>
      Object.fromEntries(
        (capabilities.data?.data ?? []).map((capability): [string, Capability] => [capability.id, capability]),
      ),
    [capabilities.data],
  )

  /**
   * Sources kept by the three filters.
   *
   * The free text matches the address (path and query), what the source
   * provides, and the path of every field it carries: the two questions a
   * reader arrives with are "which endpoint answers X?" and "where does field Y
   * come from?".
   */
  const visible = useMemo(() => {
    const search = needle.trim().toLowerCase()
    return (catalogue?.sources ?? []).filter((source) => {
      if (transport !== ALL && source.transport !== transport) {
        return false
      }
      if (state !== ALL && source.state !== state) {
        return false
      }
      if (search === '') {
        return true
      }
      const haystack = [
        source.path,
        source.query ?? '',
        source.provides,
        ...(source.fields ?? []).map((field) => field.path),
      ]
      return haystack.some((value) => value.toLowerCase().includes(search))
    })
  }, [catalogue, transport, state, needle])

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-lg font-semibold">{t('sources.title')}</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">{t('sources.intro')}</p>
      </div>

      {sources.isLoading ? <Skeleton className="h-64 w-full" /> : null}

      {sources.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={sources.error instanceof Error ? sources.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void sources.refetch()}
        />
      ) : null}

      {catalogue ? (
        <>
          <SummaryStrip summary={catalogue.summary} />

          <div className="flex flex-wrap items-end gap-4">
            <div className="space-y-1">
              <label htmlFor="sources-transport" className="text-xs text-muted-foreground">
                {t('sources.filter.transport')}
              </label>
              <select
                id="sources-transport"
                className={CONTROL_CLASS}
                value={transport}
                onChange={(event) => setTransport(event.target.value)}
              >
                <option value={ALL}>{t('sources.filter.allTransports')}</option>
                {Object.entries(TRANSPORT_BY_ID).map(([id, label]) => (
                  <option key={id} value={id}>
                    {t(label)}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1">
              <label htmlFor="sources-state" className="text-xs text-muted-foreground">
                {t('sources.filter.state')}
              </label>
              <select
                id="sources-state"
                className={CONTROL_CLASS}
                value={state}
                onChange={(event) => setState(event.target.value)}
              >
                <option value={ALL}>{t('sources.filter.allStates')}</option>
                {Object.entries(STATE_BY_ID).map(([id, entry]) => (
                  <option key={id} value={id}>
                    {t(entry.label)}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1">
              <label htmlFor="sources-search" className="text-xs text-muted-foreground">
                {t('sources.filter.search')}
              </label>
              <input
                id="sources-search"
                type="search"
                value={needle}
                onChange={(event) => setNeedle(event.target.value)}
                placeholder={t('sources.filter.searchPlaceholder')}
                className={cn(CONTROL_CLASS, 'w-64')}
              />
            </div>

            <p className="pb-2 text-xs text-muted-foreground">
              {`${t('sources.filter.visible')}: ${String(visible.length)} / ${String(catalogue.sources.length)}`}
            </p>
          </div>

          {visible.length === 0 ? (
            <EmptyState title={t('sources.filter.none')} hint={t('sources.filter.noneHint')} />
          ) : (
            <div className="space-y-4">
              {visible.map((source) => (
                <SourceBlock key={source.id} source={source} capabilities={capabilityIndex} />
              ))}
            </div>
          )}

          <Legend />
        </>
      ) : null}
    </div>
  )
}
