import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { EmptyState } from './EmptyState'

describe('EmptyState', () => {
  it('shows a title and an explanation', () => {
    render(<EmptyState title="No data" hint="No measurement has been recorded yet." />)

    expect(screen.getByText('No data')).toBeInTheDocument()
    expect(screen.getByText('No measurement has been recorded yet.')).toBeInTheDocument()
  })

  it('shows no numeric value', () => {
    const { container } = render(<EmptyState title="No data" hint="Waiting for the first collection cycle." />)
    // An empty state must offer neither "0 W" nor "0 kWh".
    expect(container.textContent).not.toMatch(/\d/)
  })
})
