import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StaleBadge } from './StaleBadge'

const sampledAt = '2026-10-16T09:59:40.000Z'

// Horloge figée : `formatAge` lit l'heure courante, l'âge affiché doit donc être
// déterministe pour être vérifié.
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-16T10:00:00.000Z'))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('StaleBadge', () => {
  it('signale des données périmées avec le seuil', () => {
    render(<StaleBadge stale staleAfterS={30} sampledAt={sampledAt} />)

    expect(screen.getByText(/Données périmées/)).toBeInTheDocument()
    expect(screen.getByText(/seuil 30 s/)).toBeInTheDocument()
    expect(screen.getByText(/il y a 20 s/)).toBeInTheDocument()
    expect(screen.getByText(/Données périmées/).closest('[data-stale]')).toHaveAttribute('data-stale', 'true')
  })

  it('signale des données à jour', () => {
    render(<StaleBadge stale={false} staleAfterS={30} sampledAt={sampledAt} />)

    expect(screen.getByText(/Données à jour/)).toBeInTheDocument()
    expect(screen.queryByText(/Données périmées/)).not.toBeInTheDocument()
  })

  it('ne rend rien tant que l’état de fraîcheur est inconnu', () => {
    const { container } = render(<StaleBadge stale={undefined} staleAfterS={undefined} sampledAt={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('tolère un horodatage absent tout en conservant l’état', () => {
    render(<StaleBadge stale={false} staleAfterS={30} sampledAt={undefined} />)
    expect(screen.getByText(/Données à jour — —/)).toBeInTheDocument()
  })
})
