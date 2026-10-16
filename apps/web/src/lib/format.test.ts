import { describe, expect, it } from 'vitest'
import { formatAge, formatDateTime, formatDuration, formatKWh, formatPct, formatW } from './format'

describe('formatW', () => {
  it('displays in watts below 1 kW', () => {
    expect(formatW(450)).toEqual({ value: '450', unit: 'W' })
    expect(formatW(0)).toEqual({ value: '0', unit: 'W' })
  })

  it('switches to kilowatts from 1 kW', () => {
    expect(formatW(1000)).toEqual({ value: '1.00', unit: 'kW' })
    expect(formatW(3406.9)).toEqual({ value: '3.41', unit: 'kW' })
  })

  it('respects the sign (negative import, positive export)', () => {
    expect(formatW(-2485.1)).toEqual({ value: '-2.49', unit: 'kW' })
    expect(formatW(-120)).toEqual({ value: '-120', unit: 'W' })
  })

  it('renders a dash without a unit for an absent value', () => {
    // "No invented zero" invariant: an absence never becomes 0 W.
    expect(formatW(null)).toEqual({ value: '—', unit: '' })
    expect(formatW(undefined)).toEqual({ value: '—', unit: '' })
    expect(formatW(Number.NaN)).toEqual({ value: '—', unit: '' })
  })
})

describe('formatKWh', () => {
  it('switches to kilowatt-hours from 1 kWh', () => {
    expect(formatKWh(8747.4)).toEqual({ value: '8.75', unit: 'kWh' })
    expect(formatKWh(999)).toEqual({ value: '999', unit: 'Wh' })
  })

  it('renders a dash for an absent value', () => {
    expect(formatKWh(null)).toEqual({ value: '—', unit: '' })
  })
})

describe('formatPct', () => {
  it('formats with one decimal and the symbol', () => {
    expect(formatPct(23.6)).toBe('23.6 %')
    // No padding zero: a round value stays round.
    expect(formatPct(100)).toBe('100 %')
  })

  it('renders a dash for an absent value', () => {
    expect(formatPct(null)).toBe('—')
    expect(formatPct(undefined)).toBe('—')
  })
})

describe('formatAge', () => {
  const now = new Date('2026-10-16T10:00:00Z')

  it('expresses the age in seconds, minutes, hours then days', () => {
    expect(formatAge('2026-10-16T09:59:58Z', now)).toBe('just now')
    expect(formatAge('2026-10-16T09:59:30Z', now)).toBe('30 s ago')
    expect(formatAge('2026-10-16T09:55:00Z', now)).toBe('5 min ago')
    expect(formatAge('2026-10-16T07:00:00Z', now)).toBe('3 h ago')
    expect(formatAge('2026-10-14T10:00:00Z', now)).toBe('2 d ago')
  })

  it('never produces a negative age if the local clock lags', () => {
    expect(formatAge('2026-10-16T10:00:30Z', now)).toBe('just now')
  })

  it('renders a dash for an absent or unreadable value', () => {
    expect(formatAge('', now)).toBe('—')
    expect(formatAge(undefined, now)).toBe('—')
    expect(formatAge('yesterday', now)).toBe('—')
  })
})

describe('formatDateTime and formatDuration', () => {
  it('render a dash for an absent input', () => {
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
  })

  it('express short durations in milliseconds', () => {
    expect(formatDuration(12)).toBe('12 ms')
    expect(formatDuration(5001)).toMatch(/^5\.00 s$/)
  })
})
