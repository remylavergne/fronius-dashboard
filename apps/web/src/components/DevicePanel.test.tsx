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
    for (const label of ['Meter', 'Battery', 'Internal API', 'Events']) {
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
})
