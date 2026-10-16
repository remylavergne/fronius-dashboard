import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StaleBadge } from './StaleBadge'

const sampledAt = '2026-10-16T09:59:40.000Z'

// Frozen clock: `formatAge` reads the current time, so the displayed age must be
// deterministic to be checked.
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-16T10:00:00.000Z'))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('StaleBadge', () => {
  it('flags stale data with the threshold', () => {
    render(<StaleBadge stale staleAfterS={30} sampledAt={sampledAt} />)

    expect(screen.getByText(/Stale data/)).toBeInTheDocument()
    expect(screen.getByText(/threshold 30 s/)).toBeInTheDocument()
    expect(screen.getByText(/20 s ago/)).toBeInTheDocument()
    expect(screen.getByText(/Stale data/).closest('[data-stale]')).toHaveAttribute('data-stale', 'true')
  })

  it('flags fresh data', () => {
    render(<StaleBadge stale={false} staleAfterS={30} sampledAt={sampledAt} />)

    expect(screen.getByText(/Fresh data/)).toBeInTheDocument()
    expect(screen.queryByText(/Stale data/)).not.toBeInTheDocument()
  })

  it('renders nothing as long as the freshness state is unknown', () => {
    const { container } = render(<StaleBadge stale={undefined} staleAfterS={undefined} sampledAt={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('tolerates an absent timestamp while keeping the state', () => {
    render(<StaleBadge stale={false} staleAfterS={30} sampledAt={undefined} />)
    expect(screen.getByText(/Fresh data — —/)).toBeInTheDocument()
  })
})
