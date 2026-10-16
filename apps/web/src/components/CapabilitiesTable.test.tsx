import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { CapabilitiesTable } from './CapabilitiesTable'
import type { Capability } from '@/lib/schema'

const capabilities: Capability[] = [
  {
    id: 'production.power',
    label: 'Instantaneous production',
    available: true,
    quality: 'measured',
    requires: [],
    read_only: true,
    verified: false,
    evidence: ['conversation', 'simulator'],
    reason: 'Schema not validated on firmware 1.19.7-1',
  },
  {
    id: 'battery.soc',
    label: 'Battery state of charge',
    available: false,
    quality: 'missing',
    requires: ['battery'],
    read_only: true,
    verified: false,
    evidence: ['conversation'],
    reason: 'No battery installed',
  },
  {
    id: 'grid.import_export',
    label: 'Grid import / export',
    available: false,
    quality: 'missing',
    requires: ['smart_meter'],
    read_only: true,
    verified: false,
    evidence: ['conversation'],
    reason: 'No Fronius Smart Meter detected',
  },
]

function rowFor(label: string): HTMLElement {
  const cell = screen.getByText(label)
  const row = cell.closest('tr')
  expect(row).not.toBeNull()
  return row as HTMLElement
}

describe('CapabilitiesTable', () => {
  it('lists all capabilities, sorted by identifier', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    // Identifiers are the only strings containing a dot.
    expect(screen.getAllByText(/^[a-z_]+\.[a-z_.]+$/).map((node) => node.textContent)).toEqual([
      'battery.soc',
      'grid.import_export',
      'production.power',
    ])
  })

  it('shows the reason of an unavailable capability', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('Battery state of charge')
    expect(within(row).getByText('unavailable')).toBeInTheDocument()
    expect(within(row).getByText('No battery installed')).toBeInTheDocument()
    expect(within(row).getByText('battery')).toBeInTheDocument()
    expect(row).toHaveAttribute('data-available', 'false')
  })

  it('shows no zero numeric value for an unavailable capability', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    // "No invented zero" invariant: the row of an unavailable capability must
    // contain no number, therefore no padding 0.
    for (const label of ['Battery state of charge', 'Grid import / export']) {
      expect(rowFor(label).textContent).not.toMatch(/\d/)
    }
  })

  it('distinguishes an available capability that is not verified', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('Instantaneous production')
    expect(within(row).getByText('available')).toBeInTheDocument()
    expect(within(row).getByText('unverified')).toBeInTheDocument()
    expect(within(row).getByText('measured')).toBeInTheDocument()
    expect(row).toHaveAttribute('data-available', 'true')
  })

  it('renders a dash rather than an empty cell', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('Instantaneous production')
    // `requires` is empty: dash. `evidence` is set: joined list.
    expect(within(row).getByText('—')).toBeInTheDocument()
    expect(within(row).getByText('conversation, simulator')).toBeInTheDocument()
  })

  it('accepts an empty list', () => {
    render(<CapabilitiesTable capabilities={[]} />)
    expect(screen.queryAllByRole('row')).toHaveLength(1)
  })
})
