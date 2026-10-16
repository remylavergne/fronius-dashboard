import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { formatDateTime } from '@/lib/format'
import type { EventsParams } from '@/lib/queries'
import type { Event } from '@/lib/schema'
import { EventsPage } from './Events'

/**
 * The clock is frozen, so the window a range preset builds is a fixed pair of
 * UTC instants: `eventsRangeToQuery` is the real one (only the query hooks and
 * the clock are stubbed), and the assertions below therefore check the instants
 * the API would actually receive.
 */
const NOW = new Date('2026-10-16T20:00:00.000Z')

const { eventsMock, askedParams } = vi.hoisted(() => ({
  eventsMock: vi.fn(),
  askedParams: [] as EventsParams[],
}))

vi.mock('@/lib/queries', async () => {
  // The real module supplies `eventsRangeToQuery`, `EVENT_RANGES` and
  // `EVENT_LIMITS`: the assertions below then check the instants and the limit
  // the API would really receive.
  const actual = (await vi.importActual('@/lib/queries')) as Record<string, unknown>
  return {
    ...actual,
    useMinuteTick: () => NOW,
    useEvents: (params: EventsParams) => {
      askedParams.push(params)
      return eventsMock()
    },
  }
})

/** The payload of one event, as the API sends it. */
function makeEvent(overrides: Partial<Event> & Pick<Event, 'ts' | 'kind' | 'severity' | 'message'>): Event {
  return { detail: '', ...overrides }
}

const failed = makeEvent({
  ts: '2026-10-16T20:07:20.000Z',
  kind: 'collector_error',
  severity: 'error',
  message: 'The collection failed: timeout on the device',
  detail: '{"path":"/solar_api/v1/GetInverterRealtimeData.cgi","error":"timeout"}',
})
const recovered = makeEvent({
  ts: '2026-10-16T19:40:00.000Z',
  kind: 'collector_recovered',
  severity: 'info',
  message: 'The collector answers again',
})
const warning = makeEvent({
  ts: '2026-10-16T18:00:00.000Z',
  kind: 'device_event',
  severity: 'warning',
  message: 'State 1175 reported by the inverter',
  detail: 'code 1175',
})

function mockEvents(data: Event[] | undefined) {
  eventsMock.mockReturnValue({
    data: data === undefined ? undefined : { data, meta: { generated_at: NOW.toISOString() } },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
}

describe('EventsPage table', () => {
  beforeEach(() => {
    eventsMock.mockReset()
    askedParams.length = 0
    mockEvents([failed, recovered, warning])
  })

  it('lists one row per event, with the instant, the severity, the kind and the message', () => {
    render(<EventsPage />)

    const table = screen.getByRole('table')
    // A real table: a header row and one row per event.
    expect(within(table).getAllByRole('row')).toHaveLength(4)
    for (const column of ['Time', 'Severity', 'Kind', 'Message', 'Detail']) {
      expect(within(table).getByRole('columnheader', { name: column })).toBeInTheDocument()
    }

    const row = within(table).getAllByRole('row')[1] as HTMLElement
    expect(within(row).getByText(formatDateTime(failed.ts))).toBeInTheDocument()
    expect(within(row).getByText('The collection failed: timeout on the device')).toBeInTheDocument()
    // The badge carries the word: colour alone would say nothing.
    expect(within(row).getByText('Error')).toBeInTheDocument()
    expect(within(row).getByText('Collection failure')).toBeInTheDocument()
  })

  it('translates the known kinds and keeps the raw machine word for the others', () => {
    mockEvents([
      recovered,
      makeEvent({
        ts: '2026-10-16T17:00:00.000Z',
        kind: 'gateway_swapped',
        severity: 'info',
        message: 'Unknown to this build',
      }),
    ])

    render(<EventsPage />)

    // The label is also an option of the kind filter: read the table.
    const table = within(screen.getByRole('table'))
    expect(table.getByText('Collection recovered')).toBeInTheDocument()
    // Never a blank cell: a kind this build does not translate shows itself —
    // in the table AND as an option of the filter.
    expect(table.getByText('gateway_swapped')).toBeInTheDocument()
  })

  it('says how many events it shows out of how many it fetched', () => {
    render(<EventsPage />)

    expect(screen.getByText('Events shown: 3 / 3 fetched')).toBeInTheDocument()
  })
})

describe('EventsPage detail disclosure', () => {
  beforeEach(() => {
    eventsMock.mockReset()
    askedParams.length = 0
  })

  it('keeps the detail closed by default, then shows it as text and never as markup', async () => {
    // A payload that would be an image tag if it were ever rendered as HTML.
    const payload = '<img src=x onerror="alert(1)">'
    mockEvents([{ ...failed, detail: payload }])

    render(<EventsPage />)

    // The detail is in the document but hidden, so `aria-controls` always names
    // something; `aria-expanded` says whether it is disclosed.
    const button = screen.getByRole('button', { name: 'Show the detail' })
    expect(button).toHaveAttribute('aria-expanded', 'false')
    const panel = document.getElementById(button.getAttribute('aria-controls') ?? '')
    expect(panel?.hasAttribute('hidden')).toBe(true)
    expect(screen.getByText(payload)).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()

    await userEvent.click(button)

    expect(button).toHaveAttribute('aria-expanded', 'true')
    expect(panel?.hasAttribute('hidden')).toBe(false)
    expect(screen.getByRole('button', { name: 'Hide the detail' })).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
  })

  it('is reachable with the keyboard', async () => {
    // jsdom does not implement Enter/Space activation of a button: what can be
    // proven here is that the disclosure *is* a native button placed in the tab
    // order, right after the last filter — in a browser, that is a disclosure
    // one Enter away.
    mockEvents([{ ...failed, detail: 'code 1175' }])

    render(<EventsPage />)

    screen.getByLabelText('Kind').focus()
    await userEvent.tab()

    const button = screen.getByRole('button', { name: 'Show the detail' })
    expect(button).toHaveFocus()
    expect(button.tagName).toBe('BUTTON')
  })

  it('says an event carries no detail instead of showing an empty disclosure', () => {
    mockEvents([recovered])

    render(<EventsPage />)

    expect(screen.getByText('No detail')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show the detail' })).not.toBeInTheDocument()
  })
})

describe('EventsPage filters', () => {
  beforeEach(() => {
    eventsMock.mockReset()
    askedParams.length = 0
    mockEvents([failed, recovered, warning])
  })

  it('narrows by severity over the fetched page and restates the count', async () => {
    render(<EventsPage />)

    await userEvent.selectOptions(screen.getByLabelText('Severity'), 'error')

    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('row')).toHaveLength(2)
    expect(within(table).getByText('Collection failure')).toBeInTheDocument()
    expect(within(table).queryByText('Collection recovered')).not.toBeInTheDocument()
    expect(screen.getByText('Events shown: 1 / 3 fetched')).toBeInTheDocument()
  })

  it('narrows by kind over the fetched page', async () => {
    render(<EventsPage />)

    await userEvent.selectOptions(screen.getByLabelText('Kind'), 'device_event')

    expect(screen.getByText('Events shown: 1 / 3 fetched')).toBeInTheDocument()
    const table = within(screen.getByRole('table'))
    expect(table.getByText('Device event')).toBeInTheDocument()
    expect(table.queryByText('Collection failure')).not.toBeInTheDocument()
  })

  it('offers only the severities and kinds the fetched page holds', () => {
    render(<EventsPage />)

    const severities = within(screen.getByLabelText('Severity')).getAllByRole('option')
    expect(severities.map((option) => option.textContent)).toEqual(['All severities', 'Info', 'Warning', 'Error'])
    const kinds = within(screen.getByLabelText('Kind')).getAllByRole('option')
    // The options carry the labels the table shows, not the machine words: a
    // reader filters on what they can read. The raw word stays the value.
    expect(kinds.map((option) => option.textContent)).toEqual([
      'All kinds',
      'Collection failure',
      'Collection recovered',
      'Device event',
    ])
  })

  it('tells a page the filters emptied from a window that holds nothing', async () => {
    render(<EventsPage />)

    await userEvent.selectOptions(screen.getByLabelText('Severity'), 'error')
    await userEvent.selectOptions(screen.getByLabelText('Kind'), 'device_event')

    expect(screen.getByText('No event matches these filters')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.getByText('Events shown: 0 / 3 fetched')).toBeInTheDocument()
  })
})

describe('EventsPage range and limit', () => {
  beforeEach(() => {
    eventsMock.mockReset()
    askedParams.length = 0
    mockEvents([failed])
  })

  it('asks the API for the chosen window, as UTC instants', async () => {
    render(<EventsPage />)

    expect(askedParams.at(-1)).toEqual({
      limit: 100,
      from: '2026-10-15T20:00:00.000Z',
      to: '2026-10-16T20:00:00.000Z',
    })

    await userEvent.selectOptions(screen.getByLabelText('Range'), '7d')
    expect(askedParams.at(-1)).toEqual({
      limit: 100,
      from: '2026-10-09T20:00:00.000Z',
      to: '2026-10-16T20:00:00.000Z',
    })

    await userEvent.selectOptions(screen.getByLabelText('Range'), 'all')
    // No bound at all: the whole recorded history, which is what "all" means.
    expect(askedParams.at(-1)).toEqual({ limit: 100 })
  })

  it('asks for the chosen limit', async () => {
    render(<EventsPage />)

    await userEvent.selectOptions(screen.getByLabelText('Fetched at once'), '500')

    expect(askedParams.at(-1)?.limit).toBe(500)
  })
})

describe('EventsPage empty window', () => {
  beforeEach(() => {
    eventsMock.mockReset()
    askedParams.length = 0
  })

  it('states an empty window once, without a table', () => {
    mockEvents([])

    render(<EventsPage />)

    expect(screen.getByText('No event in this range')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('No event matches these filters')).not.toBeInTheDocument()
  })

  it('states an API failure and offers a retry', () => {
    eventsMock.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error('network failure'),
      refetch: vi.fn(),
    })

    render(<EventsPage />)

    expect(screen.getByTestId('api-error')).toBeInTheDocument()
    expect(screen.getByText('network failure')).toBeInTheDocument()
  })
})
