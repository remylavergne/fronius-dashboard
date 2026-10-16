import { describe, expect, it } from 'vitest'
import { resolveStale } from './freshness'
import type { Meta } from './schema'

const NOW = Date.parse('2026-10-16T10:00:00.000Z')

function meta(overrides: Partial<Meta> = {}): Meta {
  return { generated_at: '2026-10-16T09:59:59.000Z', ...overrides }
}

describe('resolveStale', () => {
  it('fait confiance au verdict du serveur quand l’API répond', () => {
    expect(resolveStale(meta({ stale: true, sampled_at: '2026-10-16T09:59:59.000Z' }), false, NOW)).toBe(true)
    expect(resolveStale(meta({ stale: false, sampled_at: '2026-10-16T09:00:00.000Z' }), false, NOW)).toBe(false)
  })

  it('ne décide rien tant que l’état de fraîcheur est inconnu', () => {
    expect(resolveStale(undefined, false, NOW)).toBeUndefined()
    expect(resolveStale(meta(), false, NOW)).toBeUndefined()
  })

  it('juge localement quand l’API est injoignable', () => {
    // Le serveur disait « à jour » 30 s avant la panne : ce n'est plus vrai.
    const stale = meta({ stale: false, stale_after_s: 30, sampled_at: '2026-10-16T09:57:00.000Z' })
    expect(resolveStale(stale, true, NOW)).toBe(true)

    // Mesure récente : toujours fraîche malgré l'API muette.
    const fresh = meta({ stale: false, stale_after_s: 30, sampled_at: '2026-10-16T09:59:50.000Z' })
    expect(resolveStale(fresh, true, NOW)).toBe(false)
  })

  it('applique un seuil de repli en l’absence de seuil serveur', () => {
    expect(resolveStale(meta({ sampled_at: '2026-10-16T09:59:50.000Z' }), true, NOW)).toBe(false)
    expect(resolveStale(meta({ sampled_at: '2026-10-16T09:59:00.000Z' }), true, NOW)).toBe(true)
  })

  it('reste indécis sans instant de mesure, et prudent sur un instant illisible', () => {
    expect(resolveStale(meta(), true, NOW)).toBeUndefined()
    expect(resolveStale(meta({ sampled_at: 'hier' }), true, NOW)).toBe(true)
  })
})
