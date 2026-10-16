/**
 * Formatting of values for display.
 *
 * The API exposes base SI units (W, Wh, %); conversion to kW and kWh is done
 * here, at the display boundary, never in the model nor in the database.
 * Locale: en-GB (decimal point, comma as thousands separator, 24-hour clock).
 */

const decimal0 = new Intl.NumberFormat('en-GB', { maximumFractionDigits: 0 })
const decimal1 = new Intl.NumberFormat('en-GB', { maximumFractionDigits: 1 })
const decimal2 = new Intl.NumberFormat('en-GB', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Formatted value plus its unit, to avoid any scale ambiguity. */
export type Formatted = { value: string; unit: string }

/**
 * Power: kW beyond 1 kW, otherwise W.
 *
 * An absent value (`null` or `undefined`) is never rendered as "0": the caller
 * receives a dash, which preserves the "no invented zero" invariant all the way
 * to the screen.
 */
export function formatW(watts: number | null | undefined): Formatted {
  if (watts === null || watts === undefined || !Number.isFinite(watts)) {
    return { value: '—', unit: '' }
  }
  if (Math.abs(watts) >= 1000) {
    return { value: decimal2.format(watts / 1000), unit: 'kW' }
  }
  return { value: decimal0.format(watts), unit: 'W' }
}

/** Energy: kWh beyond 1 kWh, otherwise Wh. */
export function formatKWh(wattHours: number | null | undefined): Formatted {
  if (wattHours === null || wattHours === undefined || !Number.isFinite(wattHours)) {
    return { value: '—', unit: '' }
  }
  if (Math.abs(wattHours) >= 1000) {
    return { value: decimal2.format(wattHours / 1000), unit: 'kWh' }
  }
  return { value: decimal0.format(wattHours), unit: 'Wh' }
}

/** Percentage. */
export function formatPct(pct: number | null | undefined): string {
  if (pct === null || pct === undefined || !Number.isFinite(pct)) {
    return '—'
  }
  return `${decimal1.format(pct)} %`
}

/**
 * Age of a timestamp.
 *
 * An empty string (last success never recorded) or an unreadable one gives "—".
 */
export function formatAge(isoTimestamp: string | null | undefined, now: Date = new Date()): string {
  if (!isoTimestamp) {
    return '—'
  }
  const then = new Date(isoTimestamp)
  const time = then.getTime()
  if (Number.isNaN(time)) {
    return '—'
  }

  const age = now.getTime() - time
  if (age < 0) {
    // Local clock behind the device: do not display a negative age.
    return 'just now'
  }
  if (age < 5 * SECOND) {
    return 'just now'
  }
  if (age < MINUTE) {
    return `${Math.round(age / SECOND)} s ago`
  }
  if (age < HOUR) {
    return `${Math.round(age / MINUTE)} min ago`
  }
  if (age < DAY) {
    return `${Math.round(age / HOUR)} h ago`
  }
  return `${Math.round(age / DAY)} d ago`
}

/** Full local timestamp, for tooltips and detail pages. */
export function formatDateTime(isoTimestamp: string | null | undefined): string {
  if (!isoTimestamp) {
    return '—'
  }
  const date = new Date(isoTimestamp)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }
  return new Intl.DateTimeFormat('en-GB', {
    dateStyle: 'short',
    timeStyle: 'medium',
  }).format(date)
}

/** Duration in milliseconds, in short form. */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms)) {
    return '—'
  }
  if (ms < 1000) {
    return `${decimal0.format(ms)} ms`
  }
  return `${decimal2.format(ms / 1000)} s`
}
