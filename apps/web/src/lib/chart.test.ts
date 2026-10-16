import { describe, expect, it } from 'vitest'
import { MAX_FILLED_POINTS, inferStepSeconds, summarize, toPowerPoints } from './chart'
import type { Bucket } from './schema'

function bucket(start: string, avg: number | null, count = 2, missing = 0): Bucket {
  return {
    start,
    count,
    missing,
    power_pv_avg_w: avg,
    power_pv_min_w: avg,
    power_pv_max_w: avg,
  }
}

describe('toPowerPoints', () => {
  it('preserves gaps: a bucket without a measurement stays null', () => {
    const points = toPowerPoints([
      bucket('2026-10-16T10:00:00.000Z', 100),
      bucket('2026-10-16T10:05:00.000Z', null, 1, 1),
      bucket('2026-10-16T10:10:00.000Z', 300),
    ], 300)

    expect(points.map((p) => p.powerW)).toEqual([100, null, 300])
    expect(points[1]?.missing).toBe(1)
    // No interpolation: neighbouring values do not bleed into the gap.
    expect(points[1]?.powerW).not.toBe(0)
  })

  it('keeps the order and the instant of each bucket', () => {
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100), bucket('2026-10-16T10:05:00.000Z', 200)], 300)
    expect(points).toHaveLength(2)
    expect(points[0]?.t).toBe(Date.parse('2026-10-16T10:00:00.000Z'))
    expect(points[1]?.t).toBe(Date.parse('2026-10-16T10:05:00.000Z'))
  })

  it('accepts an empty series', () => {
    expect(toPowerPoints([])).toEqual([])
  })
})

describe('toPowerPoints with a window: the gaps become visible', () => {
  it('materialises the missing buckets as points without a measurement', () => {
    // Without these points, Recharts would render 10:00 and 10:03 adjacent and
    // draw the curve across the gap: this is the interpolation to forbid.
    const window = { from: '2026-10-16T10:00:00.000Z', to: '2026-10-16T10:05:00.000Z' }
    const points = toPowerPoints(
      [bucket('2026-10-16T10:00:00.000Z', 100), bucket('2026-10-16T10:03:00.000Z', 300)],
      60,
      window,
    )

    expect(points).toHaveLength(5)
    expect(points.map((point) => point.powerW)).toEqual([100, null, null, 300, null])
    expect(points[1]?.count).toBe(0)
    expect(points[1]?.missing).toBe(0)
    expect(summarize(points).withoutMeasure).toBe(3)
  })

  it('aligns the grid on the epoch and excludes the upper bound', () => {
    const window = { from: '2026-10-16T10:00:30.000Z', to: '2026-10-16T10:02:00.000Z' }
    const points = toPowerPoints([], 60, window)

    expect(points.map((point) => point.t)).toEqual([Date.parse('2026-10-16T10:01:00.000Z')])
  })

  it('leaves a returned bucket without a value as a curve gap', () => {
    const window = { from: '2026-10-16T10:00:00.000Z', to: '2026-10-16T10:02:00.000Z' }
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', null, 1, 1)], 60, window)

    expect(points.map((point) => point.powerW)).toEqual([null, null])
    expect(points[0]?.count).toBe(1)
    expect(summarize(points).missingSamples).toBe(1)
  })

  it('gives up filling the grid beyond the safety bound', () => {
    // A year at a one-minute step would be 525,600 points: the grid is not
    // filled in, only the returned buckets are displayed.
    const window = { from: '2025-10-16T00:00:00.000Z', to: '2026-10-16T00:00:00.000Z' }
    const buckets = [bucket('2026-10-16T00:00:00.000Z', 100)]
    expect((window.to ? (Date.parse(window.to) - Date.parse(window.from)) / 60_000 : 0) > MAX_FILLED_POINTS).toBe(true)
    expect(toPowerPoints(buckets, 60, window)).toHaveLength(1)
  })

  it('without a window, only the buckets provided are rendered', () => {
    expect(toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100)], 60)).toHaveLength(1)
  })

  it('ignores an inconsistent window', () => {
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100)], 60, {
      from: '2026-10-16T11:00:00.000Z',
      to: '2026-10-16T10:00:00.000Z',
    })
    expect(points).toHaveLength(1)
  })
})

describe('inferStepSeconds', () => {
  it('infers the step from two consecutive buckets', () => {
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T10:05:00.000Z', 2)])).toBe(300)
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T11:00:00.000Z', 2)])).toBe(3600)
  })

  it('falls back to the provided value when the series is too short or inconsistent', () => {
    expect(inferStepSeconds([], 60)).toBe(60)
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1)], 60)).toBe(60)
    // Two identical buckets give no usable step.
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T10:00:00.000Z', 2)], 60)).toBe(60)
  })
})

describe('summarize', () => {
  it('counts the measured points, the gaps and the bounds', () => {
    const points = toPowerPoints([
      bucket('2026-10-16T10:00:00.000Z', 100, 3, 0),
      bucket('2026-10-16T10:05:00.000Z', null, 2, 2),
      bucket('2026-10-16T10:10:00.000Z', 300, 1, 0),
    ])

    const summary = summarize(points)
    expect(summary.measured).toBe(2)
    expect(summary.withoutMeasure).toBe(1)
    expect(summary.samples).toBe(6)
    expect(summary.missingSamples).toBe(2)
    expect(summary.minW).toBe(100)
    expect(summary.maxW).toBe(300)
    expect(summary.avgW).toBe(200)
  })

  it('returns null bounds when no measurement exists', () => {
    const summary = summarize(toPowerPoints([bucket('2026-10-16T10:00:00.000Z', null, 1, 1)]))
    expect(summary.measured).toBe(0)
    expect(summary.minW).toBeNull()
    expect(summary.maxW).toBeNull()
    expect(summary.avgW).toBeNull()
  })

  it('handles an empty series without error', () => {
    const summary = summarize([])
    expect(summary).toMatchObject({ measured: 0, withoutMeasure: 0, samples: 0, minW: null, maxW: null, avgW: null })
  })
})
