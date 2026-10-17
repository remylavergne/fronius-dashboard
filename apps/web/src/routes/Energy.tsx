import { useMemo, useState } from 'react'
import type { ComponentProps } from 'react'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { ApiAlert } from '@/components/ApiAlert'
import { EmptyState } from '@/components/EmptyState'
import { KpiCard } from '@/components/KpiCard'
import { Badge } from '@/components/ui/badge'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import type { ChartConfig } from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { summarizeRollups, toDailyPoints } from '@/lib/energy'
import { csvDownloadUrl } from '@/lib/export'
import type { DailyPoint, EnergySource, RollupPeriod } from '@/lib/energy'
import { formatDateTime, formatKWh } from '@/lib/format'
import { useEnergyDaily, useMinuteTick } from '@/lib/queries'
import type { DailyEnergy } from '@/lib/schema'

/** Dash of a day nothing could be established for: never a zero, never a blank. */
const DASH = '—'

/**
 * Shared empty array: without a stable reference, `?? []` would recreate an
 * array on every render and invalidate the memo of the points and the rollups.
 */
const NO_DAYS: DailyEnergy[] = []

const chartConfig = {
  wh: { label: 'Energy', color: 'var(--chart-1)' },
} satisfies ChartConfig

/** Titles of the three rollups of the page. */
const ROLLUP_LABELS: Record<RollupPeriod, TranslationKey> = {
  week: 'energy.rollup.week',
  month: 'energy.rollup.month',
  year: 'energy.rollup.year',
}

/**
 * Marker of the counter a day's value comes from.
 *
 * The two markers do not look the same and both carry their word: a value
 * measured from our own stored total is not the counter the device reports, and
 * that difference belongs on the screen, not in a comment.
 */
const SOURCE_BADGES: Record<EnergySource, { label: TranslationKey; variant: ComponentProps<typeof Badge>['variant'] }> = {
  measured: { label: 'energy.source.measured', variant: 'secondary' },
  derived: { label: 'energy.source.derived', variant: 'outline' },
}

const CONTROL_CLASS =
  'h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring'

/** ALL_DAYS asks the table for every day of the fetched window. */
const ALL_DAYS = 'all' as const

/** The windows the table offers, and the label of each. */
const SHOWN_CHOICES: readonly { value: number | typeof ALL_DAYS; label: TranslationKey }[] = [
  { value: 7, label: 'energy.shown.7d' },
  { value: 30, label: 'energy.shown.30d' },
  { value: 90, label: 'energy.shown.90d' },
  { value: ALL_DAYS, label: 'energy.shown.all' },
]

/**
 * The window the page asks for: the current year, up to tomorrow's local
 * midnight.
 *
 * The bounds are RFC3339 **instants in UTC** — the server parses them with
 * `time.RFC3339` and refuses a bare date — while the days the answer carries are
 * the *installation's* local days. Going back to 1 January is what makes the
 * three rollups mean something: on a sliding seven- or thirty-day window, "this
 * year" would rest on a fraction of its days. The span stays well inside the 366
 * days the API accepts.
 */
function yearWindow(now: Date): { from: string; to: string } {
  return {
    from: new Date(now.getFullYear(), 0, 1).toISOString(),
    to: new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1).toISOString(),
  }
}

/**
 * Daily energies, as a bar chart.
 *
 * A day without a value is a point of its own, with `wh: null`: it keeps its
 * slot on the axis and no bar is drawn there. That is the point of materialising
 * the days — were the day simply absent from the series, its neighbours would
 * close the hole and the chart would read as if production had continued. No
 * value is ever interpolated, and an absence is never a zero.
 *
 * The summary under the figure says the same thing in words, for the reader who
 * cannot see the bars.
 */
function DailyEnergyChart({ points }: { points: DailyPoint[] }) {
  const { t } = useT()
  const valued = points.filter((point) => point.wh !== null)
  if (valued.length === 0) {
    return <EmptyState title={t('energy.chart.empty')} hint={t('energy.chart.emptyHint')} />
  }

  const total = valued.reduce((sum, point) => sum + (point.wh ?? 0), 0)
  const peak = valued.reduce((highest, point) => Math.max(highest, point.wh ?? 0), 0)
  const useKilowatts = peak >= 1000
  const axis = new Intl.NumberFormat('en-GB', { maximumFractionDigits: useKilowatts ? 2 : 0 })
  const totalText = formatKWh(total)
  const summary = [
    `${String(valued.length)} ${t('energy.chart.valued')}`,
    `${String(points.length - valued.length)} ${t('energy.chart.gaps')}`,
    `${t('energy.chart.total')} ${totalText.value} ${totalText.unit}`.trim(),
  ].join(' · ')

  return (
    <figure className="space-y-2">
      <figcaption className="text-sm text-muted-foreground">{t('energy.chart.title')}</figcaption>
      <div role="img" aria-label={`${t('energy.chart.title')}. ${summary}`}>
        <ChartContainer config={chartConfig} className="h-64 w-full" initialDimension={{ width: 720, height: 256 }}>
          <BarChart data={points} margin={{ left: 4, right: 8, top: 8, bottom: 0 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={28} />
            <YAxis
              tickLine={false}
              axisLine={false}
              width={64}
              tickFormatter={(value: number) =>
                `${axis.format(useKilowatts ? value / 1000 : value)} ${useKilowatts ? 'kWh' : 'Wh'}`
              }
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(label) => String(label)}
                  formatter={(value) => {
                    const formatted = formatKWh(typeof value === 'number' ? value : null)
                    return `${formatted.value} ${formatted.unit}`.trim()
                  }}
                />
              }
            />
            <Bar dataKey="wh" fill="var(--color-wh)" radius={[2, 2, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </div>
      <p className="sr-only">{summary}</p>
      <p className="text-xs text-muted-foreground" data-testid="energy-chart-summary">
        {summary}
      </p>
    </figure>
  )
}

/** One row per local day: what it is worth, where that comes from, and what to add. */
function DailyEnergyTable({ points }: { points: DailyPoint[] }) {
  const { t } = useT()

  return (
    <Table>
      <TableCaption className="sr-only">{t('energy.table.caption')}</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead scope="col">{t('energy.col.date')}</TableHead>
          <TableHead scope="col" className="text-right">
            {t('energy.col.value')}
          </TableHead>
          <TableHead scope="col">{t('energy.col.source')}</TableHead>
          <TableHead scope="col">{t('energy.col.note')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {/* The order is the API's: oldest day first. This page never reorders it. */}
        {points.map((point) => {
          const formatted = formatKWh(point.wh)
          const source = point.source ? SOURCE_BADGES[point.source] : undefined
          return (
            <TableRow key={point.date}>
              {/* The day is the installation's own local day: shown as it arrived,
                  never through `new Date(date)`, which would shift it. */}
              <TableCell className="font-mono text-xs">{point.date}</TableCell>
              <TableCell className="text-right tabular-nums">
                {`${formatted.value} ${formatted.unit}`.trim()}
              </TableCell>
              <TableCell>
                {source ? (
                  <Badge variant={source.variant}>{t(source.label)}</Badge>
                ) : (
                  <span className="text-muted-foreground">{DASH}</span>
                )}
              </TableCell>
              <TableCell className="whitespace-normal">
                <div className="flex flex-wrap items-baseline gap-1.5">
                  {point.partial ? (
                    <Badge variant="outline" title={t('energy.partialHint')}>
                      {t('energy.partial')}
                    </Badge>
                  ) : null}
                  {/* A dash without its reason leaves the reader to guess whether
                      the application is broken: the backend's sentence is what
                      makes an empty day actionable. */}
                  {point.reason === '' ? (
                    point.partial ? null : (
                      <span className="text-muted-foreground">{DASH}</span>
                    )
                  ) : (
                    <span>{point.reason}</span>
                  )}
                </div>
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

/**
 * Energy: the energy of each day, where each day's value comes from, and the
 * three rollups that sum them.
 *
 * Nothing here invents a number. The device of this installation publishes no
 * day counter, so most days carry a value derived from our own stored total, and
 * each row says which of the two it shows; a day nothing could be established
 * for keeps its dash and the backend's reason. A range that was never recorded
 * at all is a single empty state — never a table of dashes.
 */
export function EnergyPage() {
  const { t } = useT()
  const now = useMinuteTick()
  const bounds = useMemo(() => yearWindow(now), [now])
  const energy = useEnergyDaily(bounds)
  const days = energy.data?.data ?? NO_DAYS
  // The rollups read the whole year — that is what makes "this year" mean
  // something — but the table and the chart show the tail of it: a year of days
  // with nothing behind them is a wall of dashes, not a reading.
  const [shown, setShown] = useState<number | typeof ALL_DAYS>(30)
  const points = useMemo(() => toDailyPoints(days), [days])
  const visible = useMemo(
    () => (shown === ALL_DAYS ? points : points.slice(-shown)),
    [points, shown],
  )
  const rollups = useMemo(() => summarizeRollups(days, now), [days, now])

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-lg font-semibold">{t('energy.title')}</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">{t('energy.intro')}</p>
        <p className="text-xs text-muted-foreground">
          {`${t('energy.window')}: ${formatDateTime(bounds.from)} → ${formatDateTime(bounds.to)}. ${t('energy.windowHint')}`}
        </p>
        {/* The same bounds the page fetched with: the browser downloads the file. */}
        <a
          href={csvDownloadUrl('/api/v1/energy/daily', bounds)}
          download
          className="inline-block text-sm font-medium text-primary underline underline-offset-4"
        >
          {t('energy.export')}
        </a>
      </div>

      {energy.isLoading ? <Skeleton className="h-64 w-full" /> : null}

      {energy.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={energy.error instanceof Error ? energy.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void energy.refetch()}
        />
      ) : null}

      {energy.data && days.length === 0 ? (
        <EmptyState title={t('energy.empty')} hint={t('energy.emptyHint')} />
      ) : null}

      {days.length > 0 ? (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            {rollups.map((rollup) => (
              <KpiCard
                key={rollup.period}
                label={t(ROLLUP_LABELS[rollup.period])}
                value={formatKWh(rollup.wh)}
                hint={[
                  `${String(rollup.contributed)} ${t('energy.rollup.contributedDays')}`,
                  `${String(rollup.excluded)} ${t('energy.rollup.excludedDays')}`,
                  t('energy.rollup.lowerBound'),
                ].join(' · ')}
              />
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="energy-shown" className="text-xs text-muted-foreground">
              {t('energy.shown')}
            </label>
            <select
              id="energy-shown"
              className={CONTROL_CLASS}
              value={String(shown)}
              onChange={(event) => {
                setShown(event.target.value === 'all' ? ALL_DAYS : Number(event.target.value))
              }}
            >
              {SHOWN_CHOICES.map((choice) => (
                <option key={String(choice.value)} value={String(choice.value)}>
                  {t(choice.label)}
                </option>
              ))}
            </select>
            <span className="text-xs text-muted-foreground">
              {`${String(visible.length)} / ${String(points.length)} ${t('energy.shownDays')}`}
            </span>
          </div>

          <DailyEnergyChart points={visible} />
          <DailyEnergyTable points={visible} />
        </>
      ) : null}
    </div>
  )
}
