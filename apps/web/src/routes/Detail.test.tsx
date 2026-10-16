import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { formatDateTime } from '@/lib/format'
import { detailEnvelopeSchema } from '@/lib/schema'
import type { Detail, DetailFamily } from '@/lib/schema'
import { DetailPage } from './Detail'

// `vi.mock` is hoisted above the imports: the query result must live in a
// hoisted binding to be referenced by the factory.
const { detailMock } = vi.hoisted(() => ({ detailMock: vi.fn() }))

vi.mock('@/lib/queries', () => ({
  useDetail: () => detailMock(),
}))

const META = { generated_at: '2026-10-16T19:31:00Z' }

/** One distinct instant per family: a block must show its own, not the payload's. */
const READ_AT = '2026-10-16T19:30:00Z'
const SYSTEM_READ_AT = '2026-10-16T19:28:00Z'
const NETWORK_READ_AT = '2026-10-16T19:26:00Z'
const DEVICES_READ_AT = '2026-10-16T19:24:00Z'
const EMRS_READ_AT = '2026-10-16T19:22:00Z'

function mockDetail(payload: Detail | undefined) {
  detailMock.mockReturnValue({
    data: payload === undefined ? undefined : { data: payload, meta: META },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
}

const inverterFamily: DetailFamily = {
  family: 'inverter',
  available: true,
  reason: '',
  read_at: READ_AT,
  data: {
    state: { inverter_state: 'Sleeping', status_code: 13, error_code: 1175 },
    measurements: [
      { key: 'PAC', label: 'AC power', unit: 'W', value: 0 },
      { key: 'DAY_ENERGY', label: 'Energy of the day', unit: 'Wh', value: null },
    ],
    energies: { day_wh: null, year_wh: null, total_wh: 15089627.579722222 },
  },
}

const systemFamily: DetailFamily = {
  family: 'system',
  available: true,
  reason: '',
  read_at: SYSTEM_READ_AT,
  data: {
    system_name: 'GEN24-01',
    service_state: 'Running',
    update_running: false,
    support_enabled: false,
    connected_via_ap: false,
    accounts: { customer: true, technician: false },
    interfaces: [{ name: 'eth0', type: 'ethernet' }],
  },
}

const networkFamily: DetailFamily = {
  family: 'network',
  available: true,
  reason: '',
  read_at: NETWORK_READ_AT,
  data: {
    fdmp_connected: false,
    interfaces: [
      {
        name: 'eth0',
        ip: '192.168.1.10',
        mask: '255.255.255.0',
        gateway: '192.168.1.1',
        dns: ['1.1.1.1', '8.8.8.8'],
        configured: 'static',
        interface_mode: 'ethernet',
        status: 'connected',
        ssid: '',
        wps_state: '',
      },
    ],
  },
}

const devicesFamily: DetailFamily = {
  family: 'devices',
  available: true,
  reason: '',
  read_at: DEVICES_READ_AT,
  data: {
    devices: [
      { type: 'SmartMeter', status: 0, status_message: 'OK' },
      { type: 'Battery', status: 2, status_message: 'Not connected' },
    ],
  },
}

const emrsFamily: DetailFamily = {
  family: 'emrs',
  available: true,
  reason: '',
  read_at: EMRS_READ_AT,
  data: {
    entries: [
      { path: 'pin1.state', value: true },
      { path: 'pin1.reason', value: 'grid lost' },
      { path: 'pin1.count', value: 4 },
      { path: 'pin1.note', value: null },
    ],
  },
}

function fullPayload(): Detail {
  return {
    read_at: READ_AT,
    families: [inverterFamily, systemFamily, networkFamily, devicesFamily, emrsFamily],
  }
}

describe('DetailPage available families', () => {
  it('renders every family with the instant that family was read', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    expect(
      within(screen.getByRole('article', { name: 'Inverter' })).getByText(
        `Last read: ${formatDateTime(READ_AT)}`,
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('article', { name: 'System' })).getByText(
        `Last read: ${formatDateTime(SYSTEM_READ_AT)}`,
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('article', { name: 'Network' })).getByText(
        `Last read: ${formatDateTime(NETWORK_READ_AT)}`,
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('article', { name: 'Devices' })).getByText(
        `Last read: ${formatDateTime(DEVICES_READ_AT)}`,
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('article', { name: 'EMRS' })).getByText(
        `Last read: ${formatDateTime(EMRS_READ_AT)}`,
      ),
    ).toBeInTheDocument()
  })

  it('renders the inverter state, its codes and its measurements in the given order', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const inverter = screen.getByRole('article', { name: 'Inverter' })
    expect(within(inverter).getByText('Sleeping')).toBeInTheDocument()
    expect(within(inverter).getByText('13')).toBeInTheDocument()
    expect(within(inverter).getByText('1175')).toBeInTheDocument()
    expect(within(inverter).getByText('AC power')).toBeInTheDocument()
    expect(within(inverter).getByText('0 W')).toBeInTheDocument()

    // The backend decides the order of `measurements`: this page must not sort.
    const keys = within(inverter).getAllByText(/^(PAC|DAY_ENERGY)$/)
    expect(keys.map((cell) => cell.textContent)).toEqual(['PAC', 'DAY_ENERGY'])
  })

  it('renders a dash — never a zero — for every null of the inverter family', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const inverter = screen.getByRole('article', { name: 'Inverter' })
    // DAY_ENERGY (1) + the day and year energies (2), the total being present.
    expect(within(inverter).getAllByText('—')).toHaveLength(3)
    expect(within(inverter).getByText('15,089.63 kWh')).toBeInTheDocument()

    const dayRow = within(inverter).getByRole('row', { name: /DAY_ENERGY/ })
    expect(within(dayRow).getByText('—')).toBeInTheDocument()
    expect(within(dayRow).queryByText('0')).not.toBeInTheDocument()
  })

  it('renders the system flags, the declared accounts and the interfaces', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const system = screen.getByRole('article', { name: 'System' })
    expect(within(system).getByText('GEN24-01')).toBeInTheDocument()
    expect(within(system).getByText('Running')).toBeInTheDocument()
    // customer: true is the only account declared; the three flags are false —
    // a reported `false` stays a "no", it is not an absence.
    expect(within(system).getAllByText('yes')).toHaveLength(1)
    expect(within(system).getAllByText('no')).toHaveLength(4)
    expect(within(system).getByText('eth0')).toBeInTheDocument()
    expect(within(system).getByText('ethernet')).toBeInTheDocument()
  })

  it('renders one network row per interface, with the joined DNS list and the FDMP flag', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const network = screen.getByRole('article', { name: 'Network' })
    const row = within(network).getByRole('row', { name: /eth0/ })
    expect(within(row).getByText('192.168.1.10')).toBeInTheDocument()
    expect(within(row).getByText('255.255.255.0')).toBeInTheDocument()
    expect(within(row).getByText('192.168.1.1')).toBeInTheDocument()
    expect(within(row).getByText('1.1.1.1, 8.8.8.8')).toBeInTheDocument()
    expect(within(row).getByText('connected')).toBeInTheDocument()
    expect(within(network).getByText('no')).toBeInTheDocument()
  })

  it('renders a device status code of 0 as 0', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const devices = screen.getByRole('article', { name: 'Devices' })
    const meterRow = within(devices).getByRole('row', { name: /SmartMeter/ })
    // 0 is a code the device really reported: the dash rule is about absence.
    expect(within(meterRow).getByText('0')).toBeInTheDocument()
    expect(within(meterRow).getByText('OK')).toBeInTheDocument()
    const batteryRow = within(devices).getByRole('row', { name: /Battery/ })
    expect(within(batteryRow).getByText('2')).toBeInTheDocument()
    expect(within(batteryRow).getByText('Not connected')).toBeInTheDocument()
  })

  it('renders the generic entries as path and text, booleans as yes/no and null as a dash', () => {
    mockDetail(fullPayload())

    render(<DetailPage />)

    const emrs = screen.getByRole('article', { name: 'EMRS' })
    expect(within(within(emrs).getByRole('row', { name: /pin1\.state/ })).getByText('yes')).toBeInTheDocument()
    expect(within(emrs).getByText('grid lost')).toBeInTheDocument()
    expect(within(within(emrs).getByRole('row', { name: /pin1\.count/ })).getByText('4')).toBeInTheDocument()
    // `null` — the device reported no value — is a dash, never an empty cell that
    // could be read as a zero.
    expect(within(within(emrs).getByRole('row', { name: /pin1\.note/ })).getByText('—')).toBeInTheDocument()
  })
})

describe('DetailPage family states', () => {
  it('renders the reason of an unavailable family instead of a table', () => {
    mockDetail({
      read_at: READ_AT,
      families: [
        {
          family: 'inverter',
          available: false,
          reason: 'The device refused the read: no session could be opened.',
          read_at: READ_AT,
          data: null,
        },
      ],
    })

    render(<DetailPage />)

    const inverter = screen.getByRole('article', { name: 'Inverter' })
    expect(within(inverter).getByText('Unavailable')).toBeInTheDocument()
    expect(
      within(inverter).getByText('The device refused the read: no session could be opened.'),
    ).toBeInTheDocument()
    expect(within(inverter).queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a family absent from the payload as never read', () => {
    mockDetail({ read_at: READ_AT, families: [inverterFamily] })

    render(<DetailPage />)

    for (const title of ['System', 'Network', 'Devices', 'EMRS']) {
      const block = screen.getByRole('article', { name: title })
      expect(within(block).getByText('Never read by the collector')).toBeInTheDocument()
      expect(within(block).queryByRole('table')).not.toBeInTheDocument()
    }
    // The family that was read is not labelled as never read.
    expect(
      within(screen.getByRole('article', { name: 'Inverter' })).queryByText('Never read by the collector'),
    ).not.toBeInTheDocument()
  })

  it('renders a payload that was never read as such, without zeros', () => {
    mockDetail({ families: [] })

    render(<DetailPage />)

    expect(screen.getByText('Never read')).toBeInTheDocument()
    expect(screen.getAllByText('Never read by the collector')).toHaveLength(5)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('says an available family whose shape is unexpected could not be read', () => {
    mockDetail({
      read_at: READ_AT,
      families: [
        {
          family: 'inverter',
          available: true,
          reason: '',
          read_at: READ_AT,
          // A measurement without its identifying `key`: the typed shape rejects
          // the whole family rather than render a row this build cannot name.
          data: { measurements: [{ label: 'AC power', unit: 'W', value: 0 }] },
        },
      ],
    })

    render(<DetailPage />)

    const inverter = screen.getByRole('article', { name: 'Inverter' })
    expect(within(inverter).getByText(/could not be read/)).toBeInTheDocument()
    expect(within(inverter).queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a family this build does not know through the generic shape', () => {
    mockDetail({
      read_at: READ_AT,
      families: [
        {
          family: 'battery',
          available: true,
          reason: '',
          read_at: READ_AT,
          data: { entries: [{ path: 'soc', value: 42 }] },
        },
      ],
    })

    render(<DetailPage />)

    const battery = screen.getByRole('article', { name: 'battery' })
    expect(within(battery).getByText('soc')).toBeInTheDocument()
    expect(within(battery).getByText('42')).toBeInTheDocument()
  })

  it('renders a field that arrived as null as a dash, without failing the block', () => {
    mockDetail({
      read_at: READ_AT,
      families: [
        {
          family: 'system',
          available: true,
          // The Go DTO omits an empty reason: absent, null and "" mean the same.
          reason: null,
          read_at: SYSTEM_READ_AT,
          data: {
            system_name: null,
            service_state: null,
            update_running: null,
            support_enabled: true,
            connected_via_ap: null,
            accounts: { customer: null, technician: true },
            interfaces: [{ name: null, type: null }],
          },
        },
      ],
    })

    render(<DetailPage />)

    const system = screen.getByRole('article', { name: 'System' })
    expect(within(system).queryByText(/could not be read/)).not.toBeInTheDocument()
    // Every null of the block is a dash: the four scalars, the account the
    // device did not declare, and the two fields of the interface it did not name.
    expect(within(system).getAllByText('—')).toHaveLength(7)
    // A `true` stays a yes; a null is a dash, never a "no".
    expect(within(system).getAllByText('yes')).toHaveLength(2)
  })

  it('renders the API failure with a retry instead of a table', () => {
    detailMock.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error('unexpected API response'),
      refetch: vi.fn(),
    })

    render(<DetailPage />)

    expect(screen.getByTestId('api-error')).toBeInTheDocument()
    expect(screen.getByText('unexpected API response')).toBeInTheDocument()
  })
})

describe('detailEnvelopeSchema tolerance', () => {
  it('accepts a payload where nothing was ever read', () => {
    expect(detailEnvelopeSchema.safeParse({ data: { families: [] }, meta: META }).success).toBe(true)
  })

  it('accepts a missing and a null families list', () => {
    expect(detailEnvelopeSchema.safeParse({ data: {}, meta: META }).success).toBe(true)
    expect(detailEnvelopeSchema.safeParse({ data: { families: null }, meta: META }).success).toBe(true)
  })

  it('accepts a family without read_at, without data, and with null collections', () => {
    const result = detailEnvelopeSchema.safeParse({
      data: {
        families: [
          {
            family: 'inverter',
            available: true,
            reason: '',
            data: { measurements: null, energies: null, state: null },
          },
        ],
      },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('accepts an unknown family with an arbitrary data object', () => {
    const result = detailEnvelopeSchema.safeParse({
      data: { families: [{ family: 'battery', available: true, reason: '', data: { anything: [1, 2] } }] },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('accepts a family whose data carries keys this build does not know', () => {
    const result = detailEnvelopeSchema.safeParse({
      data: {
        families: [
          { family: 'system', available: true, reason: '', data: { system_name: 'GEN24-01', unknown: { nested: true } } },
        ],
      },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('still requires the identity keys of a family', () => {
    expect(detailEnvelopeSchema.safeParse({ data: { families: [{ family: 'inverter' }] }, meta: META }).success).toBe(
      false,
    )
  })
})
