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
  it('préserve les trous : un bucket sans mesure reste null', () => {
    const points = toPowerPoints([
      bucket('2026-10-16T10:00:00.000Z', 100),
      bucket('2026-10-16T10:05:00.000Z', null, 1, 1),
      bucket('2026-10-16T10:10:00.000Z', 300),
    ], 300)

    expect(points.map((p) => p.powerW)).toEqual([100, null, 300])
    expect(points[1]?.missing).toBe(1)
    // Aucune interpolation : les valeurs voisines ne déteignent pas sur le trou.
    expect(points[1]?.powerW).not.toBe(0)
  })

  it('conserve l’ordre et l’instant de chaque bucket', () => {
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100), bucket('2026-10-16T10:05:00.000Z', 200)], 300)
    expect(points).toHaveLength(2)
    expect(points[0]?.t).toBe(Date.parse('2026-10-16T10:00:00.000Z'))
    expect(points[1]?.t).toBe(Date.parse('2026-10-16T10:05:00.000Z'))
  })

  it('accepte une série vide', () => {
    expect(toPowerPoints([])).toEqual([])
  })
})

describe('toPowerPoints avec fenêtre : les trous deviennent visibles', () => {
  it('matérialise les buckets manquants par des points sans mesure', () => {
    // Sans ces points, Recharts rendrait 10:00 et 10:03 adjacents et tracerait
    // la courbe en travers du trou : c'est l'interpolation à interdire.
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

  it('aligne la grille sur l’epoch et exclut la borne haute', () => {
    const window = { from: '2026-10-16T10:00:30.000Z', to: '2026-10-16T10:02:00.000Z' }
    const points = toPowerPoints([], 60, window)

    expect(points.map((point) => point.t)).toEqual([Date.parse('2026-10-16T10:01:00.000Z')])
  })

  it('laisse un bucket renvoyé sans valeur en trou de courbe', () => {
    const window = { from: '2026-10-16T10:00:00.000Z', to: '2026-10-16T10:02:00.000Z' }
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', null, 1, 1)], 60, window)

    expect(points.map((point) => point.powerW)).toEqual([null, null])
    expect(points[0]?.count).toBe(1)
    expect(summarize(points).missingSamples).toBe(1)
  })

  it('renonce à compléter la grille au-delà de la borne de sécurité', () => {
    // Une année au pas d'une minute ferait 525 600 points : la grille n'est pas
    // complétée, seuls les buckets renvoyés sont affichés.
    const window = { from: '2025-10-16T00:00:00.000Z', to: '2026-10-16T00:00:00.000Z' }
    const buckets = [bucket('2026-10-16T00:00:00.000Z', 100)]
    expect((window.to ? (Date.parse(window.to) - Date.parse(window.from)) / 60_000 : 0) > MAX_FILLED_POINTS).toBe(true)
    expect(toPowerPoints(buckets, 60, window)).toHaveLength(1)
  })

  it('sans fenêtre, seuls les buckets fournis sont rendus', () => {
    expect(toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100)], 60)).toHaveLength(1)
  })

  it('ignore une fenêtre incohérente', () => {
    const points = toPowerPoints([bucket('2026-10-16T10:00:00.000Z', 100)], 60, {
      from: '2026-10-16T11:00:00.000Z',
      to: '2026-10-16T10:00:00.000Z',
    })
    expect(points).toHaveLength(1)
  })
})

describe('inferStepSeconds', () => {
  it('déduit le pas de deux buckets consécutifs', () => {
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T10:05:00.000Z', 2)])).toBe(300)
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T11:00:00.000Z', 2)])).toBe(3600)
  })

  it('retombe sur la valeur fournie quand la série est trop courte ou incohérente', () => {
    expect(inferStepSeconds([], 60)).toBe(60)
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1)], 60)).toBe(60)
    // Deux buckets identiques ne donnent aucun pas exploitable.
    expect(inferStepSeconds([bucket('2026-10-16T10:00:00.000Z', 1), bucket('2026-10-16T10:00:00.000Z', 2)], 60)).toBe(60)
  })
})

describe('summarize', () => {
  it('compte les points mesurés, les trous et les bornes', () => {
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

  it('retourne des bornes nulles quand aucune mesure n’existe', () => {
    const summary = summarize(toPowerPoints([bucket('2026-10-16T10:00:00.000Z', null, 1, 1)]))
    expect(summary.measured).toBe(0)
    expect(summary.minW).toBeNull()
    expect(summary.maxW).toBeNull()
    expect(summary.avgW).toBeNull()
  })

  it('traite une série vide sans erreur', () => {
    const summary = summarize([])
    expect(summary).toMatchObject({ measured: 0, withoutMeasure: 0, samples: 0, minW: null, maxW: null, avgW: null })
  })
})
