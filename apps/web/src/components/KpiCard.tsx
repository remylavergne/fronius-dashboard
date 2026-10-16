import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { Formatted } from '@/lib/format'

export type KpiCardProps = {
  /** Intitulé de la grandeur. */
  label: string
  /** Valeur déjà mise en forme, avec son unité (W, kW, Wh, kWh, %). */
  value: Formatted
  /** Précision secondaire : horodatage, qualité, mention d’absence. */
  hint: string
}

/**
 * Carte d'indicateur.
 *
 * L'unité est affichée à côté de la valeur, jamais déduite : une valeur absente
 * arrive sous forme de tiret avec une unité vide, ce qui empêche toute lecture
 * « 0 W ».
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
