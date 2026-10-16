import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { DevicePanel } from './DevicePanel'
import type { Detection, Device } from '@/lib/schema'

const device: Device = {
  model: 'Fronius Primo GEN24 3.6 Plus',
  device_name: 'Primo GEN24 3.6 Plus',
  article_number: '4,210,141,002',
  firmware: '1.24.3',
  serial_number: 'SIMULATED-0001',
  source: '/status/version',
  read_at_utc: '2026-10-16T07:00:00.000Z',
}

const detection: Detection = {
  read_at_utc: '2026-10-16T07:00:00.000Z',
  meter: { state: 'present', source: '/components/PowerMeter/readable', reason: '' },
  battery: { state: 'absent', source: '/components/BatteryManagementSystem/readable', reason: 'no component indexed' },
  internal_api: { state: 'present', source: '/status/version', reason: '' },
  device_events: { state: 'present', source: '/status/activeEvents', reason: '' },
  clock: {
    state: 'present',
    source: '/commands/GetUTCTime',
    reason: '',
    device_utc: '2026-10-16T07:00:00.000Z',
    drift_s: 0,
  },
  service: { state: 'present', source: '/commands/GetServiceState', reason: '', value: 'initialized' },
  last_error: '',
}

function resourceRow(label: string): HTMLElement {
  const cell = screen.getByText(label)
  const row = cell.closest('div[data-state]')
  expect(row).not.toBeNull()
  return row as HTMLElement
}

describe('DevicePanel', () => {
  it('shows the identity when a read succeeded', () => {
    render(<DevicePanel device={device} detection={detection} />)

    expect(screen.getByText('Fronius Primo GEN24 3.6 Plus')).toBeInTheDocument()
    expect(screen.getByText('Primo GEN24 3.6 Plus')).toBeInTheDocument()
    expect(screen.getByText('4,210,141,002')).toBeInTheDocument()
    expect(screen.getByText('1.24.3')).toBeInTheDocument()
    expect(screen.getByText('SIMULATED-0001')).toBeInTheDocument()
    // The provenance and the age of the read are visible.
    expect(screen.getByText('/status/version')).toBeInTheDocument()
    expect(within(screen.getByTestId('device-read-at')).getByText(/ago|just now/)).toBeInTheDocument()
  })

  it('translates the three detection states', () => {
    render(<DevicePanel device={device} detection={detection} />)

    const meter = resourceRow('Meter')
    expect(meter).toHaveAttribute('data-state', 'present')
    expect(within(meter).getByText('present')).toBeInTheDocument()

    const battery = resourceRow('Battery')
    expect(battery).toHaveAttribute('data-state', 'absent')
    expect(within(battery).getByText('absent')).toBeInTheDocument()
    // A state that is not "present" always carries its reason.
    expect(within(battery).getByText('no component indexed')).toBeInTheDocument()
  })

  it('distinguishes "absent" from "unknown"', () => {
    const unknown: Detection = {
      ...detection,
      read_at_utc: null,
      meter: { state: 'unknown', source: '/components/PowerMeter/readable', reason: 'access denied (401)' },
      internal_api: { state: 'unknown', source: '/status/version', reason: 'access denied (401)' },
      last_error: '/status/version: access denied (401)',
    }
    render(<DevicePanel device={null} detection={unknown} />)

    const meter = resourceRow('Meter')
    expect(meter).toHaveAttribute('data-state', 'unknown')
    expect(within(meter).getByText('unknown')).toBeInTheDocument()
    expect(within(meter).getByText('access denied (401)')).toBeInTheDocument()

    // The last detection error is highlighted.
    const lastError = screen.getByTestId('detection-last-error')
    expect(lastError).toHaveClass('text-destructive')
    expect(lastError.textContent).toContain('access denied (401)')
  })

  it('shows no digit when the identity is undetermined', () => {
    // "No invented zero" invariant: without a read, the whole identity is
    // rendered "—" and the block contains no padding number.
    render(<DevicePanel device={null} detection={null} />)

    const identity = screen.getByTestId('device-identity')
    expect(within(identity).getAllByText('—')).toHaveLength(5)
    expect(identity.textContent).not.toMatch(/\d/)

    // Without detection, each resource is "unknown".
    for (const label of ['Meter', 'Battery', 'Internal API', 'Events', 'Clock', 'Service state']) {
      const row = resourceRow(label)
      expect(row).toHaveAttribute('data-state', 'unknown')
      expect(within(row).getByText('unknown')).toBeInTheDocument()
    }
  })

  it('renders a dash for an absent identity field, never an empty string', () => {
    // The schemas forbid '': a read identity can only carry null.
    const absent: Device = {
      ...device,
      model: null,
      device_name: null,
      article_number: null,
      firmware: null,
      serial_number: null,
    }
    render(<DevicePanel device={absent} detection={detection} />)

    const identity = screen.getByTestId('device-identity')
    // Every absent field, including the device name and the article number,
    // shows the same em-dash placeholder as its neighbours.
    expect(within(identity).getAllByText('—')).toHaveLength(5)
    expect(identity.textContent).not.toContain('unknown')
    expect(identity.textContent).not.toContain('null')
  })

  it('shows the clock reading and its signed drift when the clock was read', () => {
    const read: Detection = {
      ...detection,
      clock: {
        state: 'present',
        source: '/commands/GetUTCTime',
        reason: '',
        device_utc: '2026-10-16T09:00:00.000Z',
        drift_s: 2.5,
      },
    }
    render(<DevicePanel device={null} detection={read} />)

    const clock = resourceRow('Clock')
    expect(clock).toHaveAttribute('data-state', 'present')
    // The drift keeps its sign and the device's own time is shown.
    expect(within(clock).getByText(/\+2\.5 s/)).toBeInTheDocument()
    expect(within(clock).getByText(/2026/)).toBeInTheDocument()
    // A small drift is not a warning.
    expect(within(clock).queryByText(/warning/i)).not.toBeInTheDocument()
    expect(within(clock).queryByText(/beyond/)).not.toBeInTheDocument()
  })

  it('treats a drift of exactly 60 s as still tolerable', () => {
    const read: Detection = {
      ...detection,
      clock: {
        state: 'present',
        source: '/commands/GetUTCTime',
        reason: '',
        device_utc: '2026-10-16T09:00:00.000Z',
        drift_s: 60,
      },
    }
    render(<DevicePanel device={null} detection={read} />)

    const clock = resourceRow('Clock')
    expect(within(clock).getByText(/\+60 s/)).toBeInTheDocument()
    expect(within(clock).queryByText(/warning/i)).not.toBeInTheDocument()
    expect(within(clock).queryByText(/beyond/)).not.toBeInTheDocument()
  })

  it('warns once the drift passes 60 s, the words carrying the meaning', () => {
    const read: Detection = {
      ...detection,
      clock: {
        state: 'present',
        source: '/commands/GetUTCTime',
        reason: '',
        device_utc: '2026-10-16T09:00:00.000Z',
        drift_s: 61,
      },
    }
    render(<DevicePanel device={null} detection={read} />)

    const clock = resourceRow('Clock')
    expect(within(clock).getByText(/\+61 s/)).toBeInTheDocument()
    // The badge and the wording say it: colour alone would say nothing.
    expect(within(clock).getByText(/Clock drift warning/)).toBeInTheDocument()
    expect(within(clock).getByText(/beyond the 60 s tolerance/)).toBeInTheDocument()
  })

  it('warns on a large negative drift too, the threshold being absolute', () => {
    const read: Detection = {
      ...detection,
      clock: {
        state: 'present',
        source: '/commands/GetUTCTime',
        reason: '',
        device_utc: '2026-10-16T09:00:00.000Z',
        drift_s: -61,
      },
    }
    render(<DevicePanel device={null} detection={read} />)

    const clock = resourceRow('Clock')
    expect(within(clock).getByText(/-61 s/)).toBeInTheDocument()
    expect(within(clock).getByText(/Clock drift warning/)).toBeInTheDocument()
  })

  it('shows no drift at all when the clock was never read', () => {
    const unread: Detection = {
      ...detection,
      clock: { state: 'unknown', source: '/commands/GetUTCTime', reason: 'access denied (401)' },
    }
    render(<DevicePanel device={null} detection={unread} />)

    const clock = resourceRow('Clock')
    expect(clock).toHaveAttribute('data-state', 'unknown')
    expect(within(clock).getByText('unknown')).toBeInTheDocument()
    expect(within(clock).getByText('access denied (401)')).toBeInTheDocument()
    // An unread clock must not render as a zero drift nor as a 1970 timestamp.
    expect(within(clock).queryByText(/drift/)).not.toBeInTheDocument()
    expect(within(clock).queryByText(/1970/)).not.toBeInTheDocument()
  })

  it('shows the state the device reported for the service', () => {
    const read: Detection = {
      ...detection,
      service: {
        state: 'present',
        source: '/commands/GetServiceState',
        reason: '',
        value: 'GridlimitsAutotest initialized',
      },
    }
    render(<DevicePanel device={null} detection={read} />)

    const service = resourceRow('Service state')
    expect(service).toHaveAttribute('data-state', 'present')
    expect(within(service).getByText(/GridlimitsAutotest initialized/)).toBeInTheDocument()
  })

  it('shows only the reason for a service that was never read', () => {
    const unread: Detection = {
      ...detection,
      service: { state: 'unknown', source: '/commands/GetServiceState', reason: 'timeout' },
    }
    render(<DevicePanel device={null} detection={unread} />)

    const service = resourceRow('Service state')
    expect(within(service).getByText('unknown')).toBeInTheDocument()
    expect(within(service).getByText('timeout')).toBeInTheDocument()
    expect(within(service).queryByText(/value/)).not.toBeInTheDocument()
  })
})
