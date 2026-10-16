import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { sampleSchema, type Sample } from '@/lib/schema'
import { resolveEnergy } from '@/lib/energy'
import { Overview } from './Overview'

// `vi.mock` is hoisted above the imports: the live query result must live in a
// hoisted binding to be referenced by the factory.
const { liveMock } = vi.hoisted(() => ({ liveMock: vi.fn() }))

vi.mock('@/lib/queries', () => ({
  RANGES: ['24h', '7d', '30d'],
  STEPS: ['auto', '1m', '5m', '15m', '1h', '1d'],
  isoToLocalInput: () => '2026-10-16T10:00',
  localInputToISO: () => '2026-10-16T10:00:00.000Z',
  rangeToQuery: () => ({ from: '2026-10-15T10:00:00.000Z', to: '2026-10-16T10:00:00.000Z', step: 'auto' }),
  useLive: () => liveMock(),
  useHealth: () => ({ data: undefined }),
  useCapabilities: () => ({ data: undefined }),
  useMinuteTick: () => new Date('2026-10-16T10:00:00.000Z'),
  useTimeseries: () => ({ data: undefined }),
}))

/** A sample with every optional quantity absent: each test fills what it needs. */
const baseSample: Sample = {
  ts: '2026-10-16T09:59:40.000Z',
  ts_source: 'measured',
  source: 'solarapi',
  quality: 'measured',
  mode: 'normal',
  power_pv_w: 1200,
  energy_day_wh: null,
  energy_year_wh: null,
  energy_total_wh: 15089627.58,
  power_grid_w: null,
  power_load_w: null,
  power_battery_w: null,
  rel_autonomy_pct: null,
  rel_self_consumption_pct: null,
  inverters: [],
  energy_derived: null,
}

const derived = {
  from: 'energy_total_wh',
  day_wh: 139.2,
  day_baseline_utc: '2026-10-16T00:00:00+02:00',
  day_reason: '',
  year_wh: 512345.6,
  year_baseline_utc: '2026-01-01T00:00:00+01:00',
  year_reason: '',
}

function mockLive(sample: Sample) {
  liveMock.mockReturnValue({
    data: { data: sample, meta: { generated_at: '2026-10-16T09:59:40.000Z' } },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
}

describe('resolveEnergy', () => {
  it('prefers the measured counter and does not flag it', () => {
    expect(resolveEnergy(8747.4, 139.2)).toEqual({ wh: 8747.4, estimated: false })
  })

  it('falls back to the derived value and flags it', () => {
    expect(resolveEnergy(null, 139.2)).toEqual({ wh: 139.2, estimated: true })
    expect(resolveEnergy(undefined, 139.2)).toEqual({ wh: 139.2, estimated: true })
  })

  it('yields null when neither is available', () => {
    expect(resolveEnergy(null, null)).toEqual({ wh: null, estimated: false })
    expect(resolveEnergy(undefined, undefined)).toEqual({ wh: null, estimated: false })
  })

  it('never lets a null derived value masquerade as a measurement', () => {
    expect(resolveEnergy(null, undefined)).toEqual({ wh: null, estimated: false })
  })
})

describe('Overview energy cards', () => {
  beforeEach(() => {
    liveMock.mockReset()
  })

  it('renders measured day and year energies without the estimated marker', () => {
    mockLive({
      ...baseSample,
      energy_day_wh: 8747.4,
      energy_year_wh: 512345.6,
      energy_derived: derived,
    })

    render(<Overview />)

    expect(screen.getByText('8.75')).toBeInTheDocument()
    expect(screen.getByText('512.35')).toBeInTheDocument()
    // The measured counter wins: the derived 139 Wh must not surface.
    expect(screen.queryByText('139')).not.toBeInTheDocument()
    expect(screen.queryByText('estimated from the total')).not.toBeInTheDocument()
  })

  it('renders the derived energies with the estimated marker when measured are null', () => {
    mockLive({
      ...baseSample,
      energy_day_wh: null,
      energy_year_wh: null,
      energy_derived: derived,
    })

    render(<Overview />)

    expect(screen.getByText('139')).toBeInTheDocument()
    expect(screen.getByText('512.35')).toBeInTheDocument()
    expect(screen.getAllByText('estimated from the total')).toHaveLength(2)
  })

  it('renders the dash placeholder when neither measured nor derived is available', () => {
    mockLive({ ...baseSample, energy_day_wh: null, energy_year_wh: null, energy_derived: null })

    render(<Overview />)

    // Two energy cards (plus the collector card, itself absent) show a dash.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
    expect(screen.queryByText('estimated from the total')).not.toBeInTheDocument()
    expect(screen.queryByText('0')).not.toBeInTheDocument()
  })
})

describe('sampleSchema energy_derived', () => {
  it('accepts a payload without energy_derived', () => {
    const without: Record<string, unknown> = { ...baseSample }
    delete without.energy_derived
    expect(sampleSchema.safeParse(without).success).toBe(true)
  })

  it('accepts a null energy_derived', () => {
    expect(sampleSchema.safeParse({ ...baseSample, energy_derived: null }).success).toBe(true)
  })

  it('accepts null day_wh / year_wh inside the derived object', () => {
    const result = sampleSchema.safeParse({
      ...baseSample,
      energy_derived: { ...derived, day_wh: null, year_wh: null },
    })
    expect(result.success).toBe(true)
  })

  it('parses the full derived object', () => {
    const result = sampleSchema.safeParse({ ...baseSample, energy_derived: derived })
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.energy_derived?.day_wh).toBe(139.2)
      expect(result.data.energy_derived?.from).toBe('energy_total_wh')
    }
  })
})
