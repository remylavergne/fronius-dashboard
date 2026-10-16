import { Badge } from '@/components/ui/badge'
import { formatAge } from '@/lib/format'

export type StaleBadgeProps = {
  /** `undefined` until the API has provided a freshness state. */
  stale: boolean | undefined
  /** Staleness threshold configured server-side, in seconds. */
  staleAfterS: number | undefined
  /** Time of the measurement, to date the note. */
  sampledAt: string | undefined
}

/**
 * Indicates whether the last measurement is stale.
 *
 * "Stale" does not mean "wrong": the measurement is still displayed, but the
 * user must know that the monitoring has received nothing for longer than the
 * threshold (`FRONIUS_STALE_AFTER`).
 */
export function StaleBadge({ stale, staleAfterS, sampledAt }: StaleBadgeProps) {
  if (stale === undefined) {
    return null
  }

  const age = formatAge(sampledAt)
  const threshold = staleAfterS !== undefined ? ` (threshold ${staleAfterS} s)` : ''
  const label = stale
    ? `Stale data${threshold} — ${age}`
    : `Fresh data — ${age}`

  return (
    <Badge variant={stale ? 'destructive' : 'secondary'} data-stale={stale ? 'true' : 'false'}>
      {label}
    </Badge>
  )
}
