import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { Formatted } from '@/lib/format'

export type KpiCardProps = {
  /** Label of the quantity. */
  label: string
  /** Value already formatted, with its unit (W, kW, Wh, kWh, %). */
  value: Formatted
  /** Secondary detail: timestamp, quality, absence note. */
  hint: string
}

/**
 * Indicator card.
 *
 * The unit is displayed next to the value, never inferred: an absent value
 * comes as a dash with an empty unit, which prevents any "0 W" reading.
 */
export function KpiCard({ label, value, hint }: KpiCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className="flex items-baseline gap-1 text-2xl tabular-nums">
          <span>{value.value}</span>
          {value.unit ? (
            <span className="text-sm font-normal text-muted-foreground">{value.unit}</span>
          ) : null}
        </CardTitle>
      </CardHeader>
      <CardContent className="text-xs text-muted-foreground">{hint}</CardContent>
    </Card>
  )
}
