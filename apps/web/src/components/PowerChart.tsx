import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import type { ChartConfig } from '@/components/ui/chart'
import { EmptyState } from '@/components/EmptyState'
import { useT } from '@/i18n'
import { summarize } from '@/lib/chart'
import type { PowerPoint } from '@/lib/chart'
import { formatW } from '@/lib/format'

const chartConfig = {
  powerW: { label: 'Power', color: 'var(--chart-1)' },
} satisfies ChartConfig

export type PowerChartProps = {
  /** Series points, gaps included (`powerW === null`). */
  points: PowerPoint[]
}

/**
 * Produced power curve.
 *
 * `connectNulls={false}`: a bucket without a measurement leaves a gap in the
 * curve. No interpolation is done between two points, and an absence is never
 * replaced by zero.
 *
 * Accessibility: the container carries a `role="img"` with a title, and a
 * textual summary describing the values is provided to screen readers — the
 * curve alone says nothing to a voice synthesis.
 */
export function PowerChart({ points }: PowerChartProps) {
  const { t } = useT()
  const summary = summarize(points)

  if (summary.measured === 0) {
    return <EmptyState title={t('chart.empty')} hint={t('chart.noPower')} />
  }

  // A single unit for the whole axis: kW as soon as the maximum exceeds 1 kW.
  const useKilowatts = summary.maxW !== null && summary.maxW >= 1000
  const axisValue = (watts: number) => (useKilowatts ? watts / 1000 : watts)
  const fmtAxis = new Intl.NumberFormat('en-GB', { maximumFractionDigits: useKilowatts ? 2 : 0 })

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
 * Readable summary of a series: serves as an accessible description and as an
 * explanation when the curve is hard to read (gaps, extreme values).
 */
function buildSummaryText({ measured, withoutMeasure, minW, maxW, avgW }: SummaryInput): string {
  const parts = [`${measured} measurement point(s)`]
  if (withoutMeasure > 0) {
    parts.push(`${withoutMeasure} point(s) without a measurement (gap in the curve)`)
  }
  if (minW !== null && maxW !== null) {
    const min = formatW(minW)
    const max = formatW(maxW)
    parts.push(`minimum ${min.value} ${min.unit}, maximum ${max.value} ${max.unit}`)
  }
  if (avgW !== null) {
    const avg = formatW(avgW)
    parts.push(`average ${avg.value} ${avg.unit}`)
  }
  return parts.join(' · ')
}
