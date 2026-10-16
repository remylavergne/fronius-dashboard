import { useId, useMemo, useState } from 'react'
import type { ComponentProps } from 'react'
import { ChevronDown } from 'lucide-react'
import { ApiAlert } from '@/components/ApiAlert'
import { EmptyState } from '@/components/EmptyState'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { formatDateTime } from '@/lib/format'
import { EVENT_LIMITS, EVENT_RANGES, eventsRangeToQuery, useEvents, useMinuteTick } from '@/lib/queries'
import type { EventRangeKey } from '@/lib/queries'
import type { Event } from '@/lib/schema'
import { cn } from '@/lib/utils'

/** Value of a filter that keeps every event. */
const ALL = 'all'

/** Native controls, styled with the same tokens as the rest of the interface. */
const CONTROL_CLASS =
  'h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring'

/**
 * Shared empty array: without a stable reference, `?? []` would recreate an
 * array on every render and invalidate the memos of the filters below.
 */
const NO_EVENTS: Event[] = []

/**
 * The three severities the backend emits (`internal/model/event.go`), each with
 * its badge.
 *
 * The vocabulary is closed server-side, but it is read as a plain string: a
 * severity this build does not know yet is displayed as its raw token with the
 * neutral badge, rather than blanking the column. The badge always carries the
 * word — colour only speeds up scanning.
 */
const SEVERITY_BY_ID: Record<string, { label: TranslationKey; variant: ComponentProps<typeof Badge>['variant'] }> = {
  info: { label: 'events.severity.info', variant: 'secondary' },
  warning: { label: 'events.severity.warning', variant: 'outline' },
  error: { label: 'events.severity.error', variant: 'destructive' },
}

/** Display order of the severities; an unknown one comes after the three. */
const SEVERITY_ORDER = ['info', 'warning', 'error']

/** Rank of a severity in the filter's options: the three known ones first, then the rest. */
function severityRank(severity: string): number {
  const index = SEVERITY_ORDER.indexOf(severity)
  return index === -1 ? SEVERITY_ORDER.length : index
}

/**
 * The kinds the backend produces (`internal/model/event.go`), translated. Any
 * other kind — the device reports its own events — keeps its raw machine word:
 * a blank kind would hide which component spoke.
 */
const KIND_LABELS: Record<string, TranslationKey> = {
  collector_error: 'events.kind.collectorError',
  collector_recovered: 'events.kind.collectorRecovered',
  collector_started: 'events.kind.collectorStarted',
  retention_purge: 'events.kind.retentionPurge',
  device_event: 'events.kind.deviceEvent',
}

/** The kind of one event: its label when it has one, its raw word otherwise. */
function KindLabel({ kind }: { kind: string }) {
  const { t } = useT()
  const key = KIND_LABELS[kind]
  return <span>{key ? t(key) : kind}</span>
}

/** The severity of one event, as a badge that carries the word itself. */
function SeverityBadge({ severity }: { severity: string }) {
  const { t } = useT()
  const entry = SEVERITY_BY_ID[severity]
  return <Badge variant={entry?.variant ?? 'outline'}>{entry ? t(entry.label) : severity}</Badge>
}

/**
 * The free-text payload of one event, in a disclosure of its own.
 *
 * It is frequently raw JSON, up to 2 KB: closed by default so the table stays
 * readable, monospace so its shape is visible, and rendered as **text** — a
 * `pre` escapes whatever arrives, and nothing of an event payload may ever
 * become markup.
 *
 * The panel stays in the document, hidden by the `hidden` attribute: the button
 * therefore always names an element that exists, `aria-expanded` says whether it
 * is open, and being a real button it is reachable with the keyboard.
 */
function EventDetail({ detail }: { detail: string }) {
  const { t } = useT()
  const [expanded, setExpanded] = useState(false)
  const panelId = useId()

  if (detail === '') {
    return <span className="text-muted-foreground">{t('events.detail.none')}</span>
  }

  return (
    <div className="space-y-1">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={panelId}
        onClick={() => setExpanded((value) => !value)}
        className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
      >
        <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', expanded && 'rotate-180')} />
        {expanded ? t('events.detail.hide') : t('events.detail.show')}
      </button>
      <div id={panelId} hidden={!expanded}>
        <pre className="max-w-xl overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs break-all whitespace-pre-wrap">
          {detail}
        </pre>
      </div>
    </div>
  )
}

/**
 * Events: the journal the collector keeps, and the events it republishes from
 * the inverter.
 *
 * The endpoint takes a window and a limit, nothing else: it has no severity and
 * no kind parameter, so those two filters are applied here, over the page that
 * was fetched — which is why the count below always states how many events are
 * shown out of how many were fetched. Narrowing the screen therefore never
 * fetches more: it is the range and the limit that decide what the screen can
 * possibly show.
 *
 * An empty window is a state of its own, named once; a page the filters emptied
 * is another, and the two are told apart.
 */
export function EventsPage() {
  const { t } = useT()
  const now = useMinuteTick()

  const [range, setRange] = useState<EventRangeKey>('24h')
  const [limit, setLimit] = useState<number>(100)
  const [severity, setSeverity] = useState<string>(ALL)
  const [kind, setKind] = useState<string>(ALL)

  const params = useMemo(() => eventsRangeToQuery(range, limit, now), [range, limit, now])
  const events = useEvents(params)
  const fetched = events.data?.data ?? NO_EVENTS

  // The two filters offer exactly what the fetched page holds: a fixed list would
  // pretend to know the vocabulary, and a severity or a kind that appeared since
  // would be unselectable. The choice in force is kept in the list even when the
  // last fetch no longer carries it — a `<select>` whose value has no option
  // renders blank, and the reader would lose the filter they are looking at.
  const severities = useMemo(() => {
    const seen = [...new Set(fetched.map((event) => event.severity))]
    if (severity !== ALL && !seen.includes(severity)) {
      seen.push(severity)
    }
    return seen.sort((left, right) => {
      const byRank = severityRank(left) - severityRank(right)
      return byRank === 0 ? (left < right ? -1 : left > right ? 1 : 0) : byRank
    })
  }, [fetched, severity])

  const kinds = useMemo(() => {
    const seen = [...new Set(fetched.map((event) => event.kind))]
    if (kind !== ALL && !seen.includes(kind)) {
      seen.push(kind)
    }
    return seen.sort()
  }, [fetched, kind])

  const visible = useMemo(
    () =>
      fetched.filter(
        (event) =>
          (severity === ALL || event.severity === severity) && (kind === ALL || event.kind === kind),
      ),
    [fetched, severity, kind],
  )

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-lg font-semibold">{t('events.title')}</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">{t('events.intro')}</p>
      </div>

      <div className="flex flex-wrap items-end gap-4">
        <div className="space-y-1">
          <label htmlFor="events-range" className="text-xs text-muted-foreground">
            {t('events.range')}
          </label>
          <select
            id="events-range"
            className={CONTROL_CLASS}
            value={range}
            onChange={(event) => setRange(event.target.value as EventRangeKey)}
          >
            {EVENT_RANGES.map((key) => (
              <option key={key} value={key}>
                {t(`events.range.${key}` as 'events.range.24h')}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label htmlFor="events-limit" className="text-xs text-muted-foreground">
            {t('events.limit')}
          </label>
          <select
            id="events-limit"
            className={CONTROL_CLASS}
            value={String(limit)}
            onChange={(event) => setLimit(Number(event.target.value))}
          >
            {EVENT_LIMITS.map((value) => (
              <option key={value} value={String(value)}>
                {String(value)}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label htmlFor="events-severity" className="text-xs text-muted-foreground">
            {t('events.filter.severity')}
          </label>
          <select
            id="events-severity"
            className={CONTROL_CLASS}
            value={severity}
            onChange={(event) => setSeverity(event.target.value)}
          >
            <option value={ALL}>{t('events.filter.allSeverities')}</option>
            {severities.map((value) => {
              const entry = SEVERITY_BY_ID[value]
              return (
                <option key={value} value={value}>
                  {entry ? t(entry.label) : value}
                </option>
              )
            })}
          </select>
        </div>

        <div className="space-y-1">
          <label htmlFor="events-kind" className="text-xs text-muted-foreground">
            {t('events.filter.kind')}
          </label>
          <select id="events-kind" className={CONTROL_CLASS} value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value={ALL}>{t('events.filter.allKinds')}</option>
            {kinds.map((value) => {
              const key = KIND_LABELS[value]
              return (
                <option key={value} value={value}>
                  {key ? t(key) : value}
                </option>
              )
            })}
          </select>
        </div>

        <p className="pb-2 text-xs text-muted-foreground">
          {`${t('events.filter.visible')}: ${String(visible.length)} / ${String(fetched.length)} ${t('events.fetched')}`}
        </p>
      </div>

      <p className="text-xs text-muted-foreground">{t('events.limitHint')}</p>

      {events.isLoading ? <Skeleton className="h-64 w-full" /> : null}

      {events.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={events.error instanceof Error ? events.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void events.refetch()}
        />
      ) : null}

      {events.data && fetched.length === 0 ? (
        <EmptyState title={t('events.empty')} hint={t('events.emptyHint')} />
      ) : null}

      {events.data && fetched.length > 0 && visible.length === 0 ? (
        <EmptyState title={t('events.filter.none')} hint={t('events.filter.noneHint')} />
      ) : null}

      {visible.length > 0 ? (
        <Table>
          <TableCaption className="sr-only">{t('events.table.caption')}</TableCaption>
          <TableHeader>
            <TableRow>
              <TableHead scope="col">{t('events.col.time')}</TableHead>
              <TableHead scope="col">{t('events.col.severity')}</TableHead>
              <TableHead scope="col">{t('events.col.kind')}</TableHead>
              <TableHead scope="col">{t('events.col.message')}</TableHead>
              <TableHead scope="col">{t('events.col.detail')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {/* The order is the API's: newest first. This page never reorders it. */}
            {visible.map((event, index) => (
              <TableRow key={`${event.ts}-${String(index)}`}>
                <TableCell className="tabular-nums" title={event.ts}>
                  {formatDateTime(event.ts)}
                </TableCell>
                <TableCell>
                  <SeverityBadge severity={event.severity} />
                </TableCell>
                <TableCell>
                  <KindLabel kind={event.kind} />
                </TableCell>
                <TableCell className="whitespace-normal">{event.message}</TableCell>
                <TableCell className="align-top">
                  <EventDetail detail={event.detail} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : null}
    </div>
  )
}
