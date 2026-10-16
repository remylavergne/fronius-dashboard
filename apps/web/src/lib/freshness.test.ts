import { describe, expect, it } from 'vitest'
import { resolveStale } from './freshness'
import type { Meta } from './schema'

const NOW = Date.parse('2026-10-16T10:00:00.000Z')

function meta(overrides: Partial<Meta> = {}): Meta {
  return { generated_at: '2026-10-16T09:59:59.000Z', ...overrides }
}

describe('resolveStale', () => {
  it('trusts the server verdict when the API answers', () => {
    expect(resolveStale(meta({ stale: true, sampled_at: '2026-10-16T09:59:59.000Z' }), false, NOW)).toBe(true)
    expect(resolveStale(meta({ stale: false, sampled_at: '2026-10-16T09:00:00.000Z' }), false, NOW)).toBe(false)
  })

  it('decides nothing as long as the freshness state is unknown', () => {
    expect(resolveStale(undefined, false, NOW)).toBeUndefined()
    expect(resolveStale(meta(), false, NOW)).toBeUndefined()
  })

  it('judges locally when the API is unreachable', () => {
    // The server said "fresh" 30 s before the failure: that is no longer true.
    const stale = meta({ stale: false, stale_after_s: 30, sampled_at: '2026-10-16T09:57:00.000Z' })
    expect(resolveStale(stale, true, NOW)).toBe(true)

    // Recent measurement: still fresh despite the silent API.
    const fresh = meta({ stale: false, stale_after_s: 30, sampled_at: '2026-10-16T09:59:50.000Z' })
    expect(resolveStale(fresh, true, NOW)).toBe(false)
  })

  it('applies a fallback threshold in the absence of a server threshold', () => {
    expect(resolveStale(meta({ sampled_at: '2026-10-16T09:59:50.000Z' }), true, NOW)).toBe(false)
    expect(resolveStale(meta({ sampled_at: '2026-10-16T09:59:00.000Z' }), true, NOW)).toBe(true)
  })

  it('stays undecided without a measurement instant, and cautious on an unreadable instant', () => {
    expect(resolveStale(meta(), true, NOW)).toBeUndefined()
    expect(resolveStale(meta({ sampled_at: 'yesterday' }), true, NOW)).toBe(true)
  })
})
