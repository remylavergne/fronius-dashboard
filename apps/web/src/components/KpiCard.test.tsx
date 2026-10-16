import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { KpiCard } from './KpiCard'
import { formatKWh, formatW } from '@/lib/format'

describe('KpiCard', () => {
  it('shows the value in watts with its unit', () => {
    render(<KpiCard label="Instantaneous production" value={formatW(450)} hint="2 s ago" />)

    expect(screen.getByText('Instantaneous production')).toBeInTheDocument()
    expect(screen.getByText('450')).toBeInTheDocument()
    expect(screen.getByText('W')).toBeInTheDocument()
    expect(screen.getByText('2 s ago')).toBeInTheDocument()
  })

  it('switches to kilowatts beyond 1 kW', () => {
    render(<KpiCard label="Instantaneous production" value={formatW(3406.9)} hint="1 s ago" />)

    expect(screen.getByText('3.41')).toBeInTheDocument()
    expect(screen.getByText('kW')).toBeInTheDocument()
    // The raw value in watts must not appear: no double scale.
    expect(screen.queryByText('3406.9')).not.toBeInTheDocument()
  })

  it('shows energies in kWh', () => {
    render(<KpiCard label="Energy of the day" value={formatKWh(8747.4)} hint="—" />)

    expect(screen.getByText('8.75')).toBeInTheDocument()
    expect(screen.getByText('kWh')).toBeInTheDocument()
  })

  it('never shows "0" for an absent measurement', () => {
    const { container } = render(<KpiCard label="Instantaneous production" value={formatW(null)} hint="absent value" />)

    expect(screen.getByText('—')).toBeInTheDocument()
    expect(container.textContent).not.toContain('0')
  })
})
