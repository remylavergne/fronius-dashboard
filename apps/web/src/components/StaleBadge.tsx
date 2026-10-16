import { Badge } from '@/components/ui/badge'
import { formatAge } from '@/lib/format'

export type StaleBadgeProps = {
  /** `undefined` tant que l'API n'a pas fourni d'état de fraîcheur. */
  stale: boolean | undefined
  /** Seuil de péremption configuré côté serveur, en secondes. */
  staleAfterS: number | undefined
  /** Instant de la mesure, pour dater la mention. */
  sampledAt: string | undefined
}

/**
 * Indique si la dernière mesure est périmée.
 *
 * « Périmé » ne veut pas dire « faux » : la mesure reste affichée, mais
 * l'utilisateur doit savoir que la supervision n'a rien reçu depuis plus que le
 * seuil (`FRONIUS_STALE_AFTER`).
 */
export function StaleBadge({ stale, staleAfterS, sampledAt }: StaleBadgeProps) {
  if (stale === undefined) {
    return null
  }

  const age = formatAge(sampledAt)
  const threshold = staleAfterS !== undefined ? ` (seuil ${staleAfterS} s)` : ''
  const label = stale
    ? `Données périmées${threshold} — ${age}`
    : `Données à jour — ${age}`

  return (
    <Badge variant={stale ? 'destructive' : 'secondary'} data-stale={stale ? 'true' : 'false'}>
      {label}
    </Badge>
  )
}
