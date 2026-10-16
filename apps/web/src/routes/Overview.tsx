import { useMemo, useState } from 'react'
import { ApiAlert } from '@/components/ApiAlert'
import { EmptyState } from '@/components/EmptyState'
import { KpiCard } from '@/components/KpiCard'
import { PowerChart } from '@/components/PowerChart'
import { StaleBadge } from '@/components/StaleBadge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n'
import { ApiError } from '@/lib/api'
import { inferStepSeconds, toPowerPoints } from '@/lib/chart'
import { formatAge, formatKWh, formatW } from '@/lib/format'
import { resolveEnergy } from '@/lib/energy'
import { resolveStale } from '@/lib/freshness'
import {
  RANGES,
  STEPS,
  isoToLocalInput,
  localInputToISO,
  rangeToQuery,
  useCapabilities,
  useHealth,
  useLive,
  useMinuteTick,
  useTimeseries,
} from '@/lib/queries'
import type { RangeKey, StepKey } from '@/lib/queries'
import { CODE_NO_DATA } from '@/lib/schema'
import type { Bucket } from '@/lib/schema'

/**
 * Shared empty array: without a stable reference, `?? []` would recreate an
 * array on every render and invalidate the memo of the chart points.
 */
const NO_BUCKETS: Bucket[] = []

/** "custom" denotes a range entered by hand. */
type Selection = RangeKey | 'custom'

const STEP_LABELS: Record<StepKey, string> = {
  auto: 'auto',
  '1m': '1 minute',
  '5m': '5 minutes',
  '15m': '15 minutes',
  '1h': '1 hour',
  '1d': '1 day',
}

/** Value of an energy KPI plus whether it comes from the derivation. */
/**
 * Overview: production, energies, collector status and curve.
 *
 * No consumption, import/export or battery card is hard-coded: these quantities
 * depend on a Smart Meter or a battery, and the Capabilities page (source of
 * truth) decides what exists. As long as they are unavailable, the page merely
 * states it with their reason.
 */
export function Overview() {
  const { t } = useT()
  const live = useLive()
  const health = useHealth()
  const capabilities = useCapabilities()
  const now = useMinuteTick()

  const [selection, setSelection] = useState<Selection>('24h')
  const [customFrom, setCustomFrom] = useState(() => isoToLocalInput(new Date(Date.now() - 6 * 3600 * 1000)))
  const [customTo, setCustomTo] = useState(() => isoToLocalInput(new Date()))
  const [customStep, setCustomStep] = useState<StepKey>('auto')

  const isCustom = selection === 'custom'
  const presetParams = useMemo(() => rangeToQuery(isCustom ? '24h' : selection, now), [isCustom, selection, now])

  const custom = useMemo(() => {
    const from = localInputToISO(customFrom)
    const to = localInputToISO(customTo)
    const valid = from !== null && to !== null && new Date(from) < new Date(to)
    return { params: { from: from ?? '', to: to ?? '', step: customStep }, valid }
  }, [customFrom, customTo, customStep])

  const params = isCustom ? custom.params : presetParams
  const series = useTimeseries(params, isCustom ? custom.valid : true)

  const sample = live.data?.data ?? null
  const meta = live.data?.meta
  const dayEnergy = resolveEnergy(sample?.energy_day_wh, sample?.energy_derived?.day_wh)
  const yearEnergy = resolveEnergy(sample?.energy_year_wh, sample?.energy_derived?.year_wh)
  const noData = live.data?.error?.code === CODE_NO_DATA
  // "Unreachable" covers two situations indistinguishable to the user: the API
  // does not answer (`network`) or it answers with a server error — which the
  // development proxy also does when `froniusd` is stopped (502/500).
  const offline =
    live.error instanceof ApiError &&
    (live.error.kind === 'network' || (live.error.status ?? 0) >= 500)

  const buckets = series.data?.data ?? NO_BUCKETS
  // The window is passed so that the missing buckets become `null` points:
  // without it, the curve would be drawn across the gaps. The step comes from
  // the server: inferring it from the gap between two returned buckets is wrong
  // as soon as one bucket is missing, and the gap would disappear from the
  // graph.
  const reportedStep = series.data?.meta.step_s
  const stepSeconds = reportedStep !== undefined && reportedStep > 0 ? reportedStep : inferStepSeconds(buckets)
  const points = useMemo(
    () => toPowerPoints(buckets, stepSeconds, { from: params.from, to: params.to }),
    [buckets, stepSeconds, params.from, params.to],
  )

  const healthData = health.data?.data
  const unavailable = (capabilities.data?.data ?? []).filter((capability) => !capability.available)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">{t('nav.overview')}</h1>
        <StaleBadge stale={resolveStale(meta, offline)} staleAfterS={meta?.stale_after_s} sampledAt={meta?.sampled_at} />
      </div>

      {offline ? (
        <ApiAlert
          title={t('state.offline')}
          message={live.error instanceof Error ? live.error.message : t('state.error')}
          hint={t('state.offlineHint')}
          retryLabel={t('state.retry')}
          onRetry={() => void live.refetch()}
        />
      ) : null}

      {!offline && live.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={live.error instanceof Error ? live.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void live.refetch()}
        />
      ) : null}

      {live.isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((key) => (
            <Skeleton key={key} className="h-28 w-full" />
          ))}
        </div>
      ) : null}

      {!live.isLoading && (noData || sample === null) && !live.isError ? (
        <EmptyState title={t('state.empty')} hint={t('state.emptyHint')} />
      ) : null}

      {sample ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <KpiCard
            label={t('kpi.production')}
            value={formatW(sample.power_pv_w)}
            hint={`${t('kpi.lastSample')}: ${formatAge(sample.ts)}`}
          />
          <KpiCard
            label={t('kpi.energyToday')}
            value={formatKWh(dayEnergy.wh)}
            hint={
              dayEnergy.estimated
                ? t('kpi.energyDerived')
                : `${t('kpi.lastSample')}: ${formatAge(sample.ts)}`
            }
          />
          <KpiCard
            label={t('kpi.energyYear')}
            value={formatKWh(yearEnergy.wh)}
            hint={
              yearEnergy.estimated
                ? t('kpi.energyDerived')
                : `${t('kpi.energyTotal')}: ${formatKWh(sample.energy_total_wh).value} ${formatKWh(sample.energy_total_wh).unit}`.trim()
            }
          />
          <KpiCard
            label={t('kpi.collector')}
            value={{ value: healthData ? t(`health.${healthData.status}` as 'health.ok') : '—', unit: '' }}
            hint={
              healthData
                ? `${t('kpi.samples')}: ${healthData.collector.samples_total} · ${t('kpi.runDuration')}: ${healthData.collector.last_run_ms} ms`
                : t('state.loading')
            }
          />
        </div>
      ) : null}

      <section className="space-y-4" aria-labelledby="chart-heading">
        <h2 id="chart-heading" className="sr-only">
          {t('chart.title')}
        </h2>

        <div className="flex flex-wrap items-end gap-4">
          <div className="space-y-1">
            <label htmlFor="range-select" className="text-xs text-muted-foreground">
              {t('chart.ariaRange')}
            </label>
            <Select value={selection} onValueChange={(value) => setSelection(value as Selection)}>
              <SelectTrigger id="range-select" className="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RANGES.map((range) => (
                  <SelectItem key={range} value={range}>
                    {t(`chart.range.${range}` as 'chart.range.24h')}
                  </SelectItem>
                ))}
                <SelectItem value="custom">{t('chart.range.custom')}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {isCustom ? (
            <>
              <div className="space-y-1">
                <label htmlFor="range-from" className="text-xs text-muted-foreground">
                  {t('chart.from')}
                </label>
                <input
                  id="range-from"
                  type="datetime-local"
                  value={customFrom}
                  onChange={(event) => setCustomFrom(event.target.value)}
                  className="h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="range-to" className="text-xs text-muted-foreground">
                  {t('chart.to')}
                </label>
                <input
                  id="range-to"
                  type="datetime-local"
                  value={customTo}
                  onChange={(event) => setCustomTo(event.target.value)}
                  className="h-9 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="range-step" className="text-xs text-muted-foreground">
                  {t('chart.step')}
                </label>
                <Select value={customStep} onValueChange={(value) => setCustomStep(value as StepKey)}>
                  <SelectTrigger id="range-step" className="w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {STEPS.map((step) => (
                      <SelectItem key={step} value={step}>
                        {STEP_LABELS[step]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </>
          ) : null}
        </div>

        {isCustom ? <p className="text-xs text-muted-foreground">{t('chart.stepHelp')}</p> : null}

        {isCustom && !custom.valid ? (
          <p role="alert" className="text-sm text-destructive">
            {t('chart.invalidRange')}
          </p>
        ) : null}

        {series.isLoading && !isCustom ? <Skeleton className="h-64 w-full" /> : null}

        {series.isError ? (
          <ApiAlert
            title={t('state.error')}
            message={series.error instanceof Error ? series.error.message : t('state.error')}
            retryLabel={t('state.retry')}
            onRetry={() => void series.refetch()}
          />
        ) : null}

        {series.data && (!isCustom || custom.valid) ? <PowerChart points={points} /> : null}
      </section>

      {unavailable.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{t('capabilities.title')}</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
              {unavailable.map((capability) => (
                <li key={capability.id}>
                  <span className="font-medium text-foreground">{capability.label}</span> — {capability.reason}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}
