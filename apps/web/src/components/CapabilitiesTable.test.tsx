import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { CapabilitiesTable } from './CapabilitiesTable'
import type { Capability } from '@/lib/schema'

const capabilities: Capability[] = [
  {
    id: 'production.power',
    label: 'Production instantanée',
    available: true,
    quality: 'measured',
    requires: [],
    read_only: true,
    verified: false,
    evidence: ['conversation', 'simulator'],
    reason: 'Schéma non validé sur firmware 1.19.7-1',
  },
  {
    id: 'battery.soc',
    label: 'État de charge de la batterie',
    available: false,
    quality: 'missing',
    requires: ['battery'],
    read_only: true,
    verified: false,
    evidence: ['conversation'],
    reason: 'Aucune batterie installée',
  },
  {
    id: 'grid.import_export',
    label: 'Import / export réseau',
    available: false,
    quality: 'missing',
    requires: ['smart_meter'],
    read_only: true,
    verified: false,
    evidence: ['conversation'],
    reason: 'Aucun Fronius Smart Meter détecté',
  },
]

function rowFor(label: string): HTMLElement {
  const cell = screen.getByText(label)
  const row = cell.closest('tr')
  expect(row).not.toBeNull()
  return row as HTMLElement
}

describe('CapabilitiesTable', () => {
  it('liste toutes les capacités, triées par identifiant', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    // Les identifiants sont les seules chaînes contenant un point.
    expect(screen.getAllByText(/^[a-z_]+\.[a-z_.]+$/).map((node) => node.textContent)).toEqual([
      'battery.soc',
      'grid.import_export',
      'production.power',
    ])
  })

  it('affiche le motif d’une capacité indisponible', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('État de charge de la batterie')
    expect(within(row).getByText('indisponible')).toBeInTheDocument()
    expect(within(row).getByText('Aucune batterie installée')).toBeInTheDocument()
    expect(within(row).getByText('battery')).toBeInTheDocument()
    expect(row).toHaveAttribute('data-available', 'false')
  })

  it('n’affiche aucune valeur numérique nulle pour une capacité indisponible', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    // Invariant « pas de zéro inventé » : la ligne d'une capacité indisponible
    // ne doit contenir aucun nombre, donc aucun 0 de remplissage.
    for (const label of ['État de charge de la batterie', 'Import / export réseau']) {
      expect(rowFor(label).textContent).not.toMatch(/\d/)
    }
  })

  it('distingue une capacité disponible non vérifiée', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('Production instantanée')
    expect(within(row).getByText('disponible')).toBeInTheDocument()
    expect(within(row).getByText('non vérifié')).toBeInTheDocument()
    expect(within(row).getByText('mesuré')).toBeInTheDocument()
    expect(row).toHaveAttribute('data-available', 'true')
  })

  it('rend un tiret plutôt qu’une cellule vide', () => {
    render(<CapabilitiesTable capabilities={capabilities} />)

    const row = rowFor('Production instantanée')
    // `requires` est vide : tiret. `evidence` est renseigné : liste jointe.
    expect(within(row).getByText('—')).toBeInTheDocument()
    expect(within(row).getByText('conversation, simulator')).toBeInTheDocument()
  })

  it('accepte une liste vide', () => {
    render(<CapabilitiesTable capabilities={[]} />)
    expect(screen.queryAllByRole('row')).toHaveLength(1)
  })
})
