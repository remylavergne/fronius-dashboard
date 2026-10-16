import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { KpiCard } from './KpiCard'
import { formatKWh, formatW } from '@/lib/format'

describe('KpiCard', () => {
  it('affiche la valeur en watts avec son unité', () => {
    render(<KpiCard label="Production instantanée" value={formatW(450)} hint="il y a 2 s" />)

    expect(screen.getByText('Production instantanée')).toBeInTheDocument()
    expect(screen.getByText('450')).toBeInTheDocument()
    expect(screen.getByText('W')).toBeInTheDocument()
    expect(screen.getByText('il y a 2 s')).toBeInTheDocument()
  })

  it('bascule en kilowatts au-delà de 1 kW', () => {
    render(<KpiCard label="Production instantanée" value={formatW(3406.9)} hint="il y a 1 s" />)

    expect(screen.getByText('3,41')).toBeInTheDocument()
    expect(screen.getByText('kW')).toBeInTheDocument()
    // La valeur brute en watts ne doit pas apparaître : pas de double échelle.
    expect(screen.queryByText('3406,9')).not.toBeInTheDocument()
  })

  it('affiche les énergies en kWh', () => {
    render(<KpiCard label="Énergie du jour" value={formatKWh(8747.4)} hint="—" />)

    expect(screen.getByText('8,75')).toBeInTheDocument()
    expect(screen.getByText('kWh')).toBeInTheDocument()
  })

  it('n’affiche jamais « 0 » pour une mesure absente', () => {
    const { container } = render(<KpiCard label="Production instantanée" value={formatW(null)} hint="valeur absente" />)

    expect(screen.getByText('—')).toBeInTheDocument()
    expect(container.textContent).not.toContain('0')
  })
})
