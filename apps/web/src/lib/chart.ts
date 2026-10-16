/**
 * Time series formatting for the chart.
 *
 * Central rule: a bucket without a measurement keeps a gap. We never
 * interpolate between two points and never replace an absence with 0.
 *
 * Watch out for the trap: Recharts only knows the points we give it. If the API
 * does not return a bucket (because it is empty), two neighbouring buckets
 * become adjacent in the array and the curve is **drawn across the gap** — this
 * is precisely the interpolation we want to avoid. The missing buckets must
 * therefore be materialised as `null` points, which `connectNulls={false}`
 * turns into a visible break.
 */
import type { Bucket } from './schema'

export type PowerPoint = {
  /** Start timestamp of the bucket, in milliseconds (X axis). */
  t: number
  /** Readable label of the bucket. */
  label: string
  /** Average power of the bucket, or `null` if no measurement exists there. */
  powerW: number | null
  /** Number of aggregated samples (0 for a missing bucket). */
  count: number
  /** Number of samples in the bucket without a power value. */
  missing: number
}

/** Window requested from the API, used to fill in the missing buckets. */
export type TimeWindow = {
  from: string
  to: string
}

/**
 * Maximum number of points for which the grid is filled in.
 *
 * `step=auto` already bounds the response to 1500 points; this limit protects
 * the case of a custom range with a fine step over a long duration, where
 * filling the grid would blow up the number of returned points. Beyond that,
 * only the buckets actually returned are displayed.
 */
export const MAX_FILLED_POINTS = 5000

const timeFormat = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' })
const dayTimeFormat = new Intl.DateTimeFormat('en-GB', {
  day: '2-digit',
  month: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
})
const dateFormat = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: '2-digit' })

function labelFor(date: Date, stepSeconds: number): string {
  if (stepSeconds >= 86400) {
    return dateFormat.format(date)
  }
  if (stepSeconds >= 3600) {
    return dayTimeFormat.format(date)
  }
  return timeFormat.format(date)
}

function pointFromBucket(bucket: Bucket, stepSeconds: number): PowerPoint {
  const date = new Date(bucket.start)
  return {
    t: date.getTime(),
    label: labelFor(date, stepSeconds),
    powerW: bucket.power_pv_avg_w,
    count: bucket.count,
    missing: bucket.missing,
  }
}

/** Point without a measurement: materialises a bucket absent from the response. */
function blankPoint(t: number, stepSeconds: number): PowerPoint {
  return { t, label: labelFor(new Date(t), stepSeconds), powerW: null, count: 0, missing: 0 }
}

function mapBuckets(buckets: Bucket[], stepSeconds: number): PowerPoint[] {
  return buckets.map((bucket) => pointFromBucket(bucket, stepSeconds))
}

/**
 * Converts API buckets into chart points.
 *
 * @param buckets non-empty buckets returned by the API
 * @param stepSeconds bucket step (inferred by `inferStepSeconds`)
 * @param window requested window; when provided, it makes the missing buckets
 *   appear as points without a measurement
 */
export function toPowerPoints(buckets: Bucket[], stepSeconds = 300, window?: TimeWindow): PowerPoint[] {
  if (!window || stepSeconds <= 0) {
    return mapBuckets(buckets, stepSeconds)
  }

  const fromMs = Date.parse(window.from)
  const toMs = Date.parse(window.to)
  const stepMs = stepSeconds * 1000
  if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || toMs <= fromMs) {
    return mapBuckets(buckets, stepSeconds)
  }

  // Buckets are aligned on the epoch server-side: the grid must be too.
  const firstMs = Math.ceil(fromMs / stepMs) * stepMs
  const expected = Math.ceil((toMs - firstMs) / stepMs)
  if (expected <= 0 || expected > MAX_FILLED_POINTS) {
    return mapBuckets(buckets, stepSeconds)
  }

  const known = new Map<number, Bucket>()
  for (const bucket of buckets) {
    known.set(Date.parse(bucket.start), bucket)
  }

  const points: PowerPoint[] = []
  for (let t = firstMs; t < toMs; t += stepMs) {
    const bucket = known.get(t)
    points.push(bucket ? pointFromBucket(bucket, stepSeconds) : blankPoint(t, stepSeconds))
  }
  return points
}

/**
 * Infers the real step of a series from its first two buckets.
 *
 * The API does not return the resolved step for `step=auto`; the gap between
 * two consecutive buckets gives it. The fallback applies when the series
 * contains only one point — the label format then does not matter.
 */
export function inferStepSeconds(buckets: Bucket[], fallbackSeconds = 300): number {
  const [first, second] = buckets
  if (!first || !second) {
    return fallbackSeconds
  }
  const delta = (new Date(second.start).getTime() - new Date(first.start).getTime()) / 1000
  return delta > 0 ? delta : fallbackSeconds
}

export type SeriesSummary = {
  /** Buckets containing at least one measurement. */
  measured: number
  /** Buckets without any power measurement: the gaps in the curve. */
  withoutMeasure: number
  /** Aggregated samples, all values combined. */
  samples: number
  /** Samples lacking a power value. */
  missingSamples: number
  minW: number | null
  maxW: number | null
  avgW: number | null
}

/**
 * Textual summary of a series: serves as an accessible description to the chart
 * and as a safeguard against an inexplicable blank screen.
 */
export function summarize(points: PowerPoint[]): SeriesSummary {
  let measured = 0
  let withoutMeasure = 0
  let samples = 0
  let missingSamples = 0
  let minW: number | null = null
  let maxW: number | null = null
  let sum = 0

  for (const point of points) {
    samples += point.count
    missingSamples += point.missing
    if (point.powerW === null) {
      withoutMeasure += 1
      continue
    }
    measured += 1
    sum += point.powerW
    minW = minW === null ? point.powerW : Math.min(minW, point.powerW)
    maxW = maxW === null ? point.powerW : Math.max(maxW, point.powerW)
  }

  return {
    measured,
    withoutMeasure,
    samples,
    missingSamples,
    minW,
    maxW,
    avgW: measured > 0 ? sum / measured : null,
  }
}
