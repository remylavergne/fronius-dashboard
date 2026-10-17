import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { resolveDayEnergy, summarizeRollups, toDailyPoints } from '@/lib/energy'
import type { EnergyDailyParams } from '@/lib/queries'
import type { DailyEnergy } from '@/lib/schema'
import { EnergyPage } from './Energy'

/**
 * The clock is frozen on a local 16 October 2026, 20:00 — a Friday. The three
 * rollups are then exactly 5, 16 and 289 elapsed days, and the window the page
 * asks for is the local 1 January (as a UTC instant) to tomorrow's midnight,
 * which the assertions below compute with the same calendar.
 */
const NOW = new Date(2026, 9, 16, 20, 0, 0)

const { dailyMock, askedParams } = vi.hoisted(() => ({
  dailyMock: vi.fn(),
  askedParams: [] as EnergyDailyParams[],
}))

vi.mock('@/lib/queries', async () => {
  const actual = (await vi.importActual('@/lib/queries')) as Record<string, unknown>
  return {
    ...actual,
    useMinuteTick: () => NOW,
    useEnergyDaily: (params: EnergyDailyParams) => {
      askedParams.push(params)
      return dailyMock()
    },
  }
})

/** One day of the payload; `energy_wh` absent means "the device reported none". */
function day(overrides: Partial<DailyEnergy> & Pick<DailyEnergy, 'date'>): DailyEnergy {
  return { energy_wh: null, ...overrides }
}

const MEASURED = day({ date: '2026-10-12', energy_wh: 1500 })
/** 13 October is deliberately absent: a day nothing was recorded on. */
const DERIVED = day({ date: '2026-10-14', derived_wh: 2000 })
const REFUSED = day({
  date: '2026-10-15',
  derived_wh: null,
  derived_reason: 'no sample carries a total energy at or before 2026-10-15T00:00:00+02:00',
})
const PARTIAL = day({ date: '2026-10-16', derived_wh: 500, partial: true })

const RANGE = [MEASURED, DERIVED, REFUSED, PARTIAL]

function mockDaily(data: DailyEnergy[] | undefined) {
  dailyMock.mockReturnValue({
    data: data === undefined ? undefined : { data, meta: { generated_at: NOW.toISOString() } },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
}

describe('EnergyPage table window', () => {
  /** A year of days, most of them with nothing behind them. */
  function yearOfDays(): DailyEnergy[] {
    const out: DailyEnergy[] = []
    const start = new Date(Date.UTC(2026, 0, 1, 12, 0, 0))
    for (let i = 0; i < 200; i++) {
      const d = new Date(start.getTime() + i * 24 * 3600 * 1000)
      const date = d.toISOString().slice(0, 10)
      out.push(day({ date, energy_wh: i % 5 === 0 ? 1000 : null }))
    }
    return out
  }

  it('shows the tail of the window, not a wall of dashes', async () => {
    mockDaily(yearOfDays())

    render(<EnergyPage />)

    // Thirty days by default, while the rollups read the whole fetched window.
    expect(screen.getAllByRole('row').length - 1).toBe(30)
    expect(screen.getByText(/30 \/ 200 day\(s\) of the window fetched/)).toBeInTheDocument()

    await userEvent.selectOptions(screen.getByLabelText('Days shown'), '7')
    expect(screen.getAllByRole('row').length - 1).toBe(7)

    await userEvent.selectOptions(screen.getByLabelText('Days shown'), 'all')
    expect(screen.getAllByRole('row').length - 1).toBe(200)
  })
})

describe('resolveDayEnergy', () => {
  it('prefers the device counter and marks it as measured', () => {
    expect(resolveDayEnergy(day({ date: '2026-10-16', energy_wh: 900, derived_wh: 2000 }))).toEqual({
      wh: 900,
      source: 'measured',
      reason: '',
    })
  })

  it('falls back to the derived value and marks it as derived', () => {
    // The reference firmware publishes no E_Day at all: `energy_wh` is null on
    // every day, and the derived value is the value that exists.
    expect(resolveDayEnergy(day({ date: '2026-10-16', energy_wh: null, derived_wh: 2000 }))).toEqual({
      wh: 2000,
      source: 'derived',
      reason: '',
    })
  })

  it('yields a dash and keeps the backend reason when neither counter has a value', () => {
    expect(resolveDayEnergy(REFUSED)).toEqual({
      wh: null,
      source: null,
      reason: 'no sample carries a total energy at or before 2026-10-15T00:00:00+02:00',
    })
  })

  it('never invents a zero for a day nothing is known about', () => {
    const resolved = resolveDayEnergy(day({ date: '2026-10-16' }))
    expect(resolved.wh).toBeNull()
    expect(resolved.wh).not.toBe(0)
  })
})

describe('toDailyPoints', () => {
  it('materialises a day the payload does not carry, so the gap stays a gap', () => {
    // Without this point, 12 and 14 October would become neighbours in the chart
    // and the hole would be drawn over: this is the interpolation to forbid.
    const points = toDailyPoints([MEASURED, DERIVED])

    expect(points.map((point) => point.date)).toEqual(['2026-10-12', '2026-10-13', '2026-10-14'])
    expect(points.map((point) => point.wh)).toEqual([1500, null, 2000])
    expect(points[1]?.wh).not.toBe(0)
    expect(points[1]?.source).toBeNull()
    expect(points[1]?.reason).toBe('')
  })

  it('keeps a day the backend refused as a point without a value, with its reason', () => {
    const points = toDailyPoints([REFUSED])
    expect(points).toHaveLength(1)
    expect(points[0]?.wh).toBeNull()
    expect(points[0]?.reason).toBe('no sample carries a total energy at or before 2026-10-15T00:00:00+02:00')
  })

  it('keeps the derivation reason for the dash alone', () => {
    // `derived_reason` says why the DERIVED value is absent. A day with a value
    // must not carry it: an excuse printed beside a number reads as a defect.
    const withReason = {
      date: '2026-10-14',
      energy_wh: null,
      derived_wh: 500,
      derived_reason: 'the closest sample before the day\'s start is 10h0m0s old',
      partial: false,
    }
    expect(resolveDayEnergy(withReason).reason).toBe('')
    expect(resolveDayEnergy({ ...withReason, energy_wh: 300 }).reason).toBe('')
    expect(resolveDayEnergy({ ...withReason, derived_wh: null }).reason).toBe(withReason.derived_reason)
  })

  it('labels a day from its own string and carries the partial flag', () => {
    const points = toDailyPoints([PARTIAL, MEASURED])
    // Every day between the first and the last is materialised, the gap
    // included: the chart needs the slot to exist, so that a missing day stays a
    // hole instead of being closed over by its neighbours.
    expect(points.map((point) => point.label)).toEqual(['12/10', '13/10', '14/10', '15/10', '16/10'])
    expect(points.map((point) => point.wh)).toEqual([1500, null, null, null, 500])
    // The partial flag travels with the day it belongs to, not with the range.
    expect(points[4]?.partial).toBe(true)
    expect(points[0]?.partial).toBe(false)
  })

  it('has no point for an empty payload', () => {
    expect(toDailyPoints([])).toEqual([])
  })
})

describe('summarizeRollups', () => {
  const rollups = summarizeRollups(RANGE, NOW)

  it('sums this week, this month and this year from the resolved days', () => {
    expect(rollups.map((rollup) => rollup.period)).toEqual(['week', 'month', 'year'])
    // 12 October (1500 Wh) + 14 October (2000) + 16 October's partial day (500).
    // 13 and 15 October carry nothing and are therefore not in the sum.
    expect(rollups.map((rollup) => rollup.wh)).toEqual([4000, 4000, 4000])
  })

  it('states how many days contributed and how many were excluded', () => {
    expect(rollups[0]).toMatchObject({ contributed: 3, excluded: 2, days: 5 })
    expect(rollups[1]).toMatchObject({ contributed: 3, excluded: 13, days: 16 })
    expect(rollups[2]).toMatchObject({ contributed: 3, excluded: 286, days: 289 })
  })

  it('leaves a total null rather than zero when not a day of the period carries a value', () => {
    const [week] = summarizeRollups([], NOW)
    expect(week).toMatchObject({ wh: null, contributed: 0, excluded: 5, days: 5 })
  })

  it('counts a day the payload does not hold as excluded', () => {
    // A day recorded without a value and a day never recorded are both days the
    // total does not rest on: the reader is told how many there are.
    const [week] = summarizeRollups([MEASURED], NOW)
    expect(week).toMatchObject({ contributed: 1, excluded: 4, days: 5 })
  })
})

describe('EnergyPage days', () => {
  beforeEach(() => {
    dailyMock.mockReset()
    askedParams.length = 0
    mockDaily(RANGE)
  })

  it('lists every day of the range, with date, value, source marker and note', () => {
    render(<EnergyPage />)

    const table = screen.getByRole('table')
    for (const column of ['Day', 'Energy', 'Source', 'Note']) {
      expect(within(table).getByRole('columnheader', { name: column })).toBeInTheDocument()
    }
    // Five days: the four days of the payload, plus 13 October, which the
    // payload does not carry — a gap is a row, not a hole in the list.
    expect(within(table).getAllByRole('row')).toHaveLength(6)
  })

  it('resolves a day as measured, as derived, or as a dash with its reason', () => {
    render(<EnergyPage />)

    const rowOf = (date: string) => {
      const table = screen.getByRole('table')
      return within(table).getByRole('row', { name: new RegExp(date) })
    }

    const measured = rowOf('2026-10-12')
    // The value and its unit are one text, as `formatKWh` renders them.
    expect(within(measured).getByText('1.50 kWh')).toBeInTheDocument()
    expect(within(measured).getByText('device counter')).toBeInTheDocument()

    const derived = rowOf('2026-10-14')
    expect(within(derived).getByText('2.00 kWh')).toBeInTheDocument()
    expect(within(derived).getByText('derived from total')).toBeInTheDocument()

    const refused = rowOf('2026-10-15')
    // Two dashes, never a zero: the value and the source of a value that does
    // not exist — plus the backend's sentence, which explains both.
    expect(within(refused).getAllByText('—')).toHaveLength(2)
    expect(
      within(refused).getByText('no sample carries a total energy at or before 2026-10-15T00:00:00+02:00'),
    ).toBeInTheDocument()
  })

  it('renders the local day exactly as it arrived', () => {
    render(<EnergyPage />)

    // `2026-10-15`, never reformatted through `new Date(date)`: the browser
    // would read that string as UTC midnight and could shift the day.
    expect(screen.getByText('2026-10-15')).toBeInTheDocument()
    expect(screen.queryByText('15/10/2026')).not.toBeInTheDocument()
  })

  it('labels the day in progress', () => {
    render(<EnergyPage />)

    const partial = screen.getByRole('row', { name: /2026-10-16/ })
    expect(within(partial).getByText('day in progress')).toBeInTheDocument()
    // The day is labelled, never mistaken for a closed one.
    expect(within(partial).getByText('500 Wh')).toBeInTheDocument()
  })
})

describe('EnergyPage chart', () => {
  beforeEach(() => {
    dailyMock.mockReset()
    askedParams.length = 0
    mockDaily(RANGE)
  })

  it('charts the days and states the days it leaves as gaps', () => {
    render(<EnergyPage />)

    const summary = screen.getByTestId('energy-chart-summary')
    // Three days carry a value, two do not: those two keep a gap, and nothing is
    // interpolated across them.
    expect(summary).toHaveTextContent(/3 days with a value/)
    expect(summary).toHaveTextContent(/2 days without a value \(a gap, never interpolated\)/)
    expect(summary).toHaveTextContent(/total 4\.00 kWh/)
    expect(screen.getByRole('img', { name: /3 days with a value/ })).toBeInTheDocument()
  })

  it('says there is no bar to draw when no day carries a value', () => {
    mockDaily([REFUSED])

    render(<EnergyPage />)

    expect(screen.getByText('No day carries a value')).toBeInTheDocument()
    expect(screen.queryByTestId('energy-chart-summary')).not.toBeInTheDocument()
  })
})

describe('EnergyPage rollups', () => {
  beforeEach(() => {
    dailyMock.mockReset()
    askedParams.length = 0
    mockDaily(RANGE)
  })

  it('states the days each total rests on, and that it is a lower bound', () => {
    render(<EnergyPage />)

    const cardOf = (title: string) => {
      const card = screen.getByText(title).closest('[data-slot="card"]')
      expect(card).not.toBeNull()
      return card as HTMLElement
    }

    const week = cardOf('This week')
    expect(within(week).getByText('4.00')).toBeInTheDocument()
    expect(
      within(week).getByText('3 days contributed · 2 days excluded · includes the day in progress: a lower bound'),
    ).toBeInTheDocument()

    // The month and the year hold the same three days, but many more days that
    // contributed nothing: the count is what makes the total readable.
    const month = cardOf('This month')
    expect(
      within(month).getByText('3 days contributed · 13 days excluded · includes the day in progress: a lower bound'),
    ).toBeInTheDocument()
    const year = cardOf('This year')
    expect(
      within(year).getByText('3 days contributed · 286 days excluded · includes the day in progress: a lower bound'),
    ).toBeInTheDocument()
  })
})

describe('EnergyPage empty range', () => {
  beforeEach(() => {
    dailyMock.mockReset()
    askedParams.length = 0
  })

  it('states an empty range once, without a table of dashes', () => {
    mockDaily([])

    render(<EnergyPage />)

    expect(screen.getByText('Nothing was recorded in this range')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('This week')).not.toBeInTheDocument()
  })

  it('asks for the whole year as UTC instants', () => {
    mockDaily(RANGE)

    render(<EnergyPage />)

    // Local midnight on 1 January and on the day after today, converted to the
    // instants the server parses with time.RFC3339.
    expect(askedParams.at(-1)).toEqual({
      from: new Date(2026, 0, 1).toISOString(),
      to: new Date(2026, 9, 17).toISOString(),
    })
  })

  it('states an API failure and offers a retry', () => {
    dailyMock.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error('network failure'),
      refetch: vi.fn(),
    })

    render(<EnergyPage />)

    expect(screen.getByTestId('api-error')).toBeInTheDocument()
    expect(screen.getByText('network failure')).toBeInTheDocument()
  })
})

describe('EnergyPage export', () => {
  beforeEach(() => {
    dailyMock.mockReset()
    askedParams.length = 0
    mockDaily(RANGE)
  })

  it('offers the fetched year as a plain CSV download', () => {
    render(<EnergyPage />)

    const asked = askedParams.at(-1) as EnergyDailyParams
    const search = new URLSearchParams({
      from: String(asked.from),
      to: String(asked.to),
      format: 'csv',
    })
    const link = screen.getByRole('link', { name: 'Download this year of daily energy as CSV' })

    // A plain anchor, built from the boundaries the page asked for.
    expect(link.tagName).toBe('A')
    expect(link).toHaveAttribute('download')
    expect(link.getAttribute('href')).toBe(`/api/v1/energy/daily?${search.toString()}`)
  })
})
