/**
 * Staleness decision displayed to the user.
 *
 * In nominal operation, it is the server that decides (`meta.stale`), because
 * it alone knows its threshold and its clock. But when the API is unreachable,
 * it can no longer say anything: keeping its last verdict would display "Fresh
 * data" on data that has not been fresh for a long time. We then apply the same
 * rule locally — age of the last measurement compared to the threshold.
 */
import type { Meta } from './schema'

/** Fallback threshold if the API has not communicated its own. */
const FALLBACK_STALE_AFTER_S = 30

export function resolveStale(meta: Meta | undefined, apiUnreachable: boolean, now: number = Date.now()): boolean | undefined {
  if (!apiUnreachable) {
    return meta?.stale
  }

  const sampledAt = meta?.sampled_at
  if (!sampledAt) {
    // No reference instant: impossible to assert anything, but the absence of
    // an API response is already reported by the banner.
    return undefined
  }
  const sampledMs = Date.parse(sampledAt)
  if (Number.isNaN(sampledMs)) {
    return true
  }
  const thresholdMs = (meta?.stale_after_s ?? FALLBACK_STALE_AFTER_S) * 1000
  return now - sampledMs > thresholdMs
}
