import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { EmptyState } from './EmptyState'

describe('EmptyState', () => {
  it('affiche un titre et une explication', () => {
    render(<EmptyState title="Aucune donnée" hint="Aucune mesure n’a encore été enregistrée." />)

    expect(screen.getByText('Aucune donnée')).toBeInTheDocument()
    expect(screen.getByText('Aucune mesure n’a encore été enregistrée.')).toBeInTheDocument()
  })

  it('n’affiche aucune valeur numérique', () => {
    const { container } = render(<EmptyState title="Aucune donnée" hint="En attente du premier cycle de collecte." />)
    // Un état vide ne doit proposer ni « 0 W » ni « 0 kWh ».
    expect(container.textContent).not.toMatch(/\d/)
  })
})
