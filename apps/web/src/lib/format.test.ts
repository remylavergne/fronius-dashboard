import { describe, expect, it } from 'vitest'
import { formatAge, formatDateTime, formatDuration, formatKWh, formatPct, formatW } from './format'

describe('formatW', () => {
  it('affiche en watts en dessous de 1 kW', () => {
    expect(formatW(450)).toEqual({ value: '450', unit: 'W' })
    expect(formatW(0)).toEqual({ value: '0', unit: 'W' })
  })

  it('bascule en kilowatts à partir de 1 kW', () => {
    expect(formatW(1000)).toEqual({ value: '1,00', unit: 'kW' })
    expect(formatW(3406.9)).toEqual({ value: '3,41', unit: 'kW' })
  })

  it('respecte le signe (import négatif, export positif)', () => {
    expect(formatW(-2485.1)).toEqual({ value: '-2,49', unit: 'kW' })
    expect(formatW(-120)).toEqual({ value: '-120', unit: 'W' })
  })

  it('rend un tiret sans unité pour une valeur absente', () => {
    // Invariant « pas de zéro inventé » : une absence ne devient jamais 0 W.
    expect(formatW(null)).toEqual({ value: '—', unit: '' })
    expect(formatW(undefined)).toEqual({ value: '—', unit: '' })
    expect(formatW(Number.NaN)).toEqual({ value: '—', unit: '' })
  })
})

describe('formatKWh', () => {
  it('bascule en kilowattheures à partir de 1 kWh', () => {
    expect(formatKWh(8747.4)).toEqual({ value: '8,75', unit: 'kWh' })
    expect(formatKWh(999)).toEqual({ value: '999', unit: 'Wh' })
  })

  it('rend un tiret pour une valeur absente', () => {
    expect(formatKWh(null)).toEqual({ value: '—', unit: '' })
  })
})

describe('formatPct', () => {
  it('formate avec une décimale et le symbole', () => {
    expect(formatPct(23.6)).toBe('23,6 %')
    // Pas de zéro de remplissage : une valeur ronde reste ronde.
    expect(formatPct(100)).toBe('100 %')
  })

  it('rend un tiret pour une valeur absente', () => {
    expect(formatPct(null)).toBe('—')
    expect(formatPct(undefined)).toBe('—')
  })
})

describe('formatAge', () => {
  const now = new Date('2026-10-16T10:00:00Z')

  it('exprime l’âge en secondes, minutes, heures puis jours', () => {
    expect(formatAge('2026-10-16T09:59:58Z', now)).toBe('à l’instant')
    expect(formatAge('2026-10-16T09:59:30Z', now)).toBe('il y a 30 s')
    expect(formatAge('2026-10-16T09:55:00Z', now)).toBe('il y a 5 min')
    expect(formatAge('2026-10-16T07:00:00Z', now)).toBe('il y a 3 h')
    expect(formatAge('2026-10-14T10:00:00Z', now)).toBe('il y a 2 j')
  })

  it('ne produit jamais d’âge négatif si l’horloge locale retarde', () => {
    expect(formatAge('2026-10-16T10:00:30Z', now)).toBe('à l’instant')
  })

  it('rend un tiret pour une valeur absente ou illisible', () => {
    expect(formatAge('', now)).toBe('—')
    expect(formatAge(undefined, now)).toBe('—')
    expect(formatAge('hier', now)).toBe('—')
  })
})

describe('formatDateTime et formatDuration', () => {
  it('rendent un tiret pour une entrée absente', () => {
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
  })

  it('expriment les durées courtes en millisecondes', () => {
    expect(formatDuration(12)).toBe('12 ms')
    expect(formatDuration(5001)).toMatch(/^5,00 s$/)
  })
})
