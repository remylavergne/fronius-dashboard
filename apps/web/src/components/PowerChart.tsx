import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import type { ChartConfig } from '@/components/ui/chart'
import { EmptyState } from '@/components/EmptyState'
import { useT } from '@/i18n'
import { summarize } from '@/lib/chart'
import type { PowerPoint } from '@/lib/chart'
import { formatW } from '@/lib/format'

const chartConfig = {
  powerW: { label: 'Puissance', color: 'var(--chart-1)' },
} satisfies ChartConfig

export type PowerChartProps = {
  /** Points de la série, trous compris (`powerW === null`). */
  points: PowerPoint[]
}

/**
 * Courbe de puissance produite.
 *
 * `connectNulls={false}` : un bucket sans mesure laisse un trou dans la courbe.
 * Aucune interpolation n'est faite entre deux points, et une absence n'est
 * jamais remplacée par zéro.
 *
 * Accessibilité : le conteneur porte un `role="img"` avec un intitulé, et un
 * résumé textuel décrivant les valeurs est fourni aux lecteurs d'écran — la
 * courbe seule ne dit rien à une synthèse vocale.
 */
export function PowerChart({ points }: PowerChartProps) {
  const { t } = useT()
  const summary = summarize(points)

  if (summary.measured === 0) {
    return <EmptyState title={t('chart.empty')} hint={t('chart.noPower')} />
  }

  // Une seule unité pour tout l'axe : kW dès que le maximum dépasse 1 kW.
  const useKilowatts = summary.maxW !== null && summary.maxW >= 1000
  const axisValue = (watts: number) => (useKilowatts ? watts / 1000 : watts)
  const fmtAxis = new Intl.NumberFormat('fr-BE', { maximumFractionDigits: useKilowatts ? 2 : 0 })

  const summaryText = buildSummaryText({
    measured: summary.measured,
    withoutMeasure: summary.withoutMeasure,
    minW: summary.minW,
    maxW: summary.maxW,
    avgW: summary.avgW,
  })

  return (
    <figure className="space-y-2">
      <figcaption className="text-sm text-muted-foreground">{t('chart.title')}</figcaption>
      <div role="img" aria-label={`${t('chart.title')}. ${summaryText}`}>
        <ChartContainer config={chartConfig} className="h-64 w-full" initialDimension={{ width: 720, height: 256 }}>
          <AreaChart data={points} margin={{ left: 4, right: 8, top: 8, bottom: 0 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={28} />
            <YAxis
              tickLine={false}
              axisLine={false}
              width={56}
              tickFormatter={(value: number) => `${fmtAxis.format(axisValue(value))} ${useKilowatts ? 'kW' : 'W'}`}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(label) => String(label)}
                  formatter={(value) => {
                    const formatted = formatW(typeof value === 'number' ? value : null)
                    return `${formatted.value} ${formatted.unit}`.trim()
                  }}
                />
              }
            />
            <Area
              dataKey="powerW"
              type="monotone"
              stroke="var(--color-powerW)"
              fill="var(--color-powerW)"
              fillOpacity={0.25}
              strokeWidth={2}
              connectNulls={false}
              dot={false}
            />
          </AreaChart>
        </ChartContainer>
      </div>
      <p className="sr-only">{summaryText}</p>
      <p className="text-xs text-muted-foreground" data-testid="chart-summary">
        {summaryText}
      </p>
    </figure>
  )
}

type SummaryInput = {
  measured: number
  withoutMeasure: number
  minW: number | null
  maxW: number | null
  avgW: number | null
}

/**
 * Résumé lisible d'une série : sert de description accessible et d'explication
 * lorsque la courbe est difficile à lire (trous, valeurs extrêmes).
 */
function buildSummaryText({ measured, withoutMeasure, minW, maxW, avgW }: SummaryInput): string {
  const parts = [`${measured} point(s) de mesure`]
  if (withoutMeasure > 0) {
    parts.push(`${withoutMeasure} point(s) sans mesure (trou dans la courbe)`)
  }
  if (minW !== null && maxW !== null) {
    const min = formatW(minW)
    const max = formatW(maxW)
    parts.push(`minimum ${min.value} ${min.unit}, maximum ${max.value} ${max.unit}`)
  }
  if (avgW !== null) {
    const avg = formatW(avgW)
    parts.push(`moyenne ${avg.value} ${avg.unit}`)
  }
  return parts.join(' · ')
}
