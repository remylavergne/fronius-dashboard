import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { formatDateTime } from '@/lib/format'
import { detailCacheDataSchema, detailEnvelopeSchema, parseDetailFamilyData } from '@/lib/schema'
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
const CACHE_READ_AT = '2026-10-16T19:20:00Z'

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

/**
 * The cache as the device answers it: the node and its attributes first, then
 * two named registers, then the bare identifier — the order the backend sends.
 */
const cacheFamily: DetailFamily = {
  family: 'cache',
  available: true,
  reason: '',
  read_at: CACHE_READ_AT,
  data: {
    nodes: [
      {
        id: '393216',
        attributes: [
          { key: 'createTS', value: '1603034125' },
          { key: 'inverter.devicetype', value: 'Primo' },
        ],
        channels: [
          {
            key: 'ACBRIDGE_POWERACTIVE_MEAN_01_F32',
            name: 'ACBRIDGE_POWERACTIVE_MEAN_01',
            kind: 'F32',
            type: 'float',
            value: 1234.5,
          },
          {
            key: 'PV_ENERGYACTIVE_ACTIVE_SUM_01_U64',
            name: 'PV_ENERGYACTIVE_ACTIVE_SUM_01',
            kind: 'U64',
            type: 'int',
            value: 15089627579,
          },
          { key: '<1090584584>', type: 'int', value: 0 },
        ],
      },
    ],
  },
}

function fullPayload(): Detail {
  return {
    read_at: READ_AT,
    families: [inverterFamily, systemFamily, networkFamily, devicesFamily, emrsFamily, cacheFamily],
  }
}

/** A payload carrying the cache family alone, with the `data` a test chooses. */
function cachePayload(data: unknown): Detail {
  return {
    read_at: CACHE_READ_AT,
    families: [{ family: 'cache', available: true, reason: '', read_at: CACHE_READ_AT, data }],
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
    expect(
      within(screen.getByRole('article', { name: 'Cache' })).getByText(
        `Last read: ${formatDateTime(CACHE_READ_AT)}`,
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

    for (const title of ['System', 'Network', 'Devices', 'EMRS', 'Cache']) {
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
    expect(screen.getAllByText('Never read by the collector')).toHaveLength(6)
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

describe('DetailPage cache family', () => {
  it('renders the node, its attributes and a disclosure collapsed by default', async () => {
    const user = userEvent.setup()
    mockDetail(fullPayload())

    render(<DetailPage />)

    const cache = screen.getByRole('article', { name: 'Cache' })
    // The device's own labels, and the values it answered, as they arrived.
    expect(within(cache).getByText('Node 393216')).toBeInTheDocument()
    expect(within(cache).getByText('createTS')).toBeInTheDocument()
    expect(within(cache).getByText('1603034125')).toBeInTheDocument()
    expect(within(cache).getByText('inverter.devicetype')).toBeInTheDocument()
    // The attributes are in the order the device sent them: this page sorts nothing.
    const labels = within(cache).getAllByText(/^(createTS|inverter\.devicetype)$/)
    expect(labels.map((cell) => cell.textContent)).toEqual(['createTS', 'inverter.devicetype'])
    // What a bare `<id>` is, and where the type marker comes from: said in words.
    expect(
      within(cache).getByText(/the <id> entries are registers it does not name/),
    ).toBeInTheDocument()

    const toggle = within(cache).getByRole('button', { name: '3 registers · 2 named' })
    const controls = toggle.getAttribute('aria-controls')

    // Keyboard-reachable, closed, and naming an element that exists.
    expect(toggle.tagName).toBe('BUTTON')
    toggle.focus()
    expect(toggle).toHaveFocus()
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(document.getElementById(controls ?? '')).not.toBeNull()
    expect(within(cache).queryByRole('table')).not.toBeInTheDocument()

    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const registers = document.getElementById(controls ?? '')
    expect(registers).not.toBeNull()
    // The registers are inside the element `aria-controls` names.
    const table = within(registers as HTMLElement).getByRole('table')
    expect(within(table).getByRole('columnheader', { name: 'Register' })).toBeInTheDocument()
    expect(within(table).getByRole('columnheader', { name: 'Type marker' })).toBeInTheDocument()
    expect(within(table).getByRole('columnheader', { name: 'Value' })).toBeInTheDocument()

    // The order is the backend's: named registers first, then the bare identifier.
    const names = within(table).getAllByText(
      /^(ACBRIDGE_POWERACTIVE_MEAN_01|PV_ENERGYACTIVE_ACTIVE_SUM_01|<1090584584>)$/,
    )
    expect(names.map((cell) => cell.textContent)).toEqual([
      'ACBRIDGE_POWERACTIVE_MEAN_01',
      'PV_ENERGYACTIVE_ACTIVE_SUM_01',
      '<1090584584>',
    ])
    // The suffix is the device's own marker, displayed as it arrived.
    expect(within(table).getByText('F32')).toBeInTheDocument()
    expect(within(table).getByText('U64')).toBeInTheDocument()
  })

  it('renders a bare identifier verbatim, inventing neither a name nor a marker', async () => {
    const user = userEvent.setup()
    mockDetail(fullPayload())

    render(<DetailPage />)

    const cache = screen.getByRole('article', { name: 'Cache' })
    await user.click(within(cache).getByRole('button', { name: '3 registers · 2 named' }))

    const bare = within(cache).getByRole('row', { name: /1090584584/ })
    expect(within(bare).getByText('<1090584584>')).toBeInTheDocument()
    // The device gives no type marker for it, so the cell carries a dash.
    expect(within(bare).getByText('—')).toBeInTheDocument()
    // A `0` the device really reported stays a 0, not a dash.
    expect(within(bare).getByText('0')).toBeInTheDocument()
    // The raw JSON type is never displayed: it only decides how a value reads.
    expect(within(bare).queryByText('int')).not.toBeInTheDocument()
  })

  it('renders each value by the type the device declared, with no invented unit', async () => {
    const user = userEvent.setup()
    mockDetail(
      cachePayload({
        nodes: [
          {
            id: '393216',
            attributes: null,
            channels: [
              {
                key: 'PV_ENERGYACTIVE_ACTIVE_SUM_01_U64',
                name: 'PV_ENERGYACTIVE_ACTIVE_SUM_01',
                kind: 'U64',
                type: 'int',
                value: 15089627579,
              },
              {
                key: 'ACBRIDGE_POWERACTIVE_MEAN_01_F32',
                name: 'ACBRIDGE_POWERACTIVE_MEAN_01',
                kind: 'F32',
                type: 'float',
                value: 1234.5,
              },
              { key: 'SOME_FLAG_U16', name: 'SOME_FLAG', kind: 'U16', type: 'bool', value: true },
              { key: 'SOME_TEXT_STR', name: 'SOME_TEXT', kind: 'STR', type: 'string', value: 'grid lost' },
              { key: 'SOME_EMPTY_U16', name: 'SOME_EMPTY', kind: 'U16', type: 'null', value: null },
              { key: 'ODD_ONE_XYZ', name: 'ODD_ONE', kind: 'XYZ', type: 'octal', value: '77' },
            ],
          },
        ],
      }),
    )

    render(<DetailPage />)

    const cache = screen.getByRole('article', { name: 'Cache' })
    await user.click(within(cache).getByRole('button', { name: '6 registers · 6 named' }))

    // `int`: the digits that arrived — a counter is exact, so it is neither
    // rounded nor grouped.
    expect(within(cache).getByText('15089627579')).toBeInTheDocument()
    expect(within(cache).queryByText('15,089,627,579')).not.toBeInTheDocument()
    // `float`: a readable decimal, and no unit — the cache carries none.
    expect(within(cache).getByText('1,234.5')).toBeInTheDocument()
    // `bool`: yes/no. `string`: as it stands.
    expect(within(cache).getByText('yes')).toBeInTheDocument()
    expect(within(cache).getByText('grid lost')).toBeInTheDocument()
    // `null`: a dash, and the only one — every other cell carries a real value.
    expect(within(cache).getAllByText('—')).toHaveLength(1)
    // A type this build does not know falls back to the value's own shape.
    expect(within(cache).getByText('77')).toBeInTheDocument()
  })

  it('renders a node with no attribute and no register without a table of blanks', async () => {
    const user = userEvent.setup()
    mockDetail(
      cachePayload({
        nodes: [
          // `null` and `[]` are the same absence, and render the same way.
          { id: '393216', attributes: null, channels: null },
          { id: '393217', attributes: [], channels: [] },
        ],
      }),
    )

    render(<DetailPage />)

    const cache = screen.getByRole('article', { name: 'Cache' })
    const toggle = within(cache).getByRole('button', { name: '0 registers · 0 named' })
    expect(within(cache).getByText('Node 393216')).toBeInTheDocument()
    expect(within(cache).getByText('Node 393217')).toBeInTheDocument()
    expect(within(cache).queryByText('Attributes')).not.toBeInTheDocument()
    expect(within(cache).queryByText('—')).not.toBeInTheDocument()

    await user.click(toggle)

    expect(within(cache).getByText('No register was read for this family.')).toBeInTheDocument()
    expect(within(cache).queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a family whose nodes are absent, null or empty as nothing to display', () => {
    for (const data of [{}, { nodes: null }, { nodes: [] }]) {
      mockDetail(cachePayload(data))
      const { unmount } = render(<DetailPage />)

      const cache = screen.getByRole('article', { name: 'Cache' })
      expect(within(cache).queryByText(/could not be read/)).not.toBeInTheDocument()
      expect(within(cache).getByText('Nothing to display for this family.')).toBeInTheDocument()
      expect(within(cache).queryByRole('button')).not.toBeInTheDocument()
      expect(within(cache).queryByRole('table')).not.toBeInTheDocument()

      unmount()
    }
  })

  it('renders an unavailable family with its reason and no disclosure', () => {
    mockDetail({
      read_at: CACHE_READ_AT,
      families: [
        {
          family: 'cache',
          available: false,
          reason: 'The device refused the read: no session could be opened.',
          read_at: CACHE_READ_AT,
          data: null,
        },
      ],
    })

    render(<DetailPage />)

    const cache = screen.getByRole('article', { name: 'Cache' })
    expect(within(cache).getByText('Unavailable')).toBeInTheDocument()
    expect(
      within(cache).getByText('The device refused the read: no session could be opened.'),
    ).toBeInTheDocument()
    expect(within(cache).queryByRole('button')).not.toBeInTheDocument()
    expect(within(cache).queryByRole('table')).not.toBeInTheDocument()
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

  it('accepts a cache family with null collections, a bare identifier and an unknown type', () => {
    const result = detailEnvelopeSchema.safeParse({
      data: {
        families: [
          {
            family: 'cache',
            available: true,
            reason: '',
            read_at: CACHE_READ_AT,
            data: {
              nodes: [
                { id: '393216', attributes: null, channels: null },
                {
                  id: '393217',
                  attributes: [],
                  channels: [
                    { key: '<1090584584>', type: 'int', value: 0 },
                    { key: 'ODD_ONE_XYZ', name: 'ODD_ONE', kind: 'XYZ', type: 'octal', value: '77' },
                  ],
                },
              ],
            },
          },
        ],
      },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('accepts a cache family whose nodes are absent or null', () => {
    for (const data of [{}, { nodes: null }, { nodes: [] }]) {
      const result = detailEnvelopeSchema.safeParse({
        data: { families: [{ family: 'cache', available: true, reason: '', data }] },
        meta: META,
      })
      expect(result.success).toBe(true)
    }
  })

  it('still requires the key of a cache register', () => {
    // The envelope leaves a family's `data` to the family: it is the per-family
    // schema that rejects a register without its key, and the page then says the
    // answer could not be read rather than rendering a nameless row.
    const family = {
      family: 'cache' as const,
      available: true,
      reason: '',
      read_at: CACHE_READ_AT,
      data: { nodes: [{ id: '393216', channels: [{ type: 'int', value: 1 }] }] },
    }

    expect(detailEnvelopeSchema.safeParse({ data: { families: [family] }, meta: META }).success).toBe(true)
    expect(parseDetailFamilyData(family, detailCacheDataSchema)).toBeNull()
  })
})
