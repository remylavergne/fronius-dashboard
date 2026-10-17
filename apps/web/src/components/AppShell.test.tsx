import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { AppShell } from './AppShell'

function renderShell(initialPath = '/') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <AppShell>
        <p>content</p>
      </AppShell>
    </MemoryRouter>,
  )
}

function mainNavigation() {
  return screen.getByRole('navigation', { name: 'Main navigation' })
}

describe('AppShell navigation', () => {
  it('shows the three groups and the eight links in the contract order', () => {
    renderShell()

    for (const label of ['Monitoring', 'Device', 'Application']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }

    const links = within(mainNavigation()).getAllByRole('link')
    expect(links.map((link) => link.textContent)).toEqual([
      'Overview',
      'Energy',
      'Events',
      'Device detail',
      'System',
      'Capabilities',
      'Data sources',
      'Settings',
    ])
  })

  it('marks the active link current, and only that one', () => {
    renderShell('/energy')

    const sidebar = within(mainNavigation())
    expect(sidebar.getByRole('link', { name: 'Energy' })).toHaveAttribute('aria-current', 'page')
    expect(sidebar.getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current')
    expect(sidebar.getByRole('link', { name: 'Events' })).not.toHaveAttribute('aria-current')
  })

  it('marks Overview current on the root path', () => {
    renderShell()

    expect(within(mainNavigation()).getByRole('link', { name: 'Overview' })).toHaveAttribute(
      'aria-current',
      'page',
    )
  })
})

describe('AppShell breadcrumb', () => {
  it('links to Overview and marks the current page on /energy', () => {
    renderShell('/energy')

    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(breadcrumb).getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '/')
    expect(within(breadcrumb).getByText('Energy')).toHaveAttribute('aria-current', 'page')
    expect(within(breadcrumb).queryByRole('link', { name: 'Energy' })).not.toBeInTheDocument()
  })

  it('shows a single current item without a self-link on the root path', () => {
    renderShell()

    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(breadcrumb).getAllByRole('listitem')).toHaveLength(1)
    expect(within(breadcrumb).getByText('Overview')).toHaveAttribute('aria-current', 'page')
    expect(within(breadcrumb).queryAllByRole('link')).toHaveLength(0)
  })

  it('renders no breadcrumb on an unknown path', () => {
    renderShell('/nope')

    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).not.toBeInTheDocument()
  })
})

describe('AppShell skip link', () => {
  it('is the first link and targets the focusable main landmark', () => {
    renderShell()

    const firstLink = screen.getAllByRole('link').at(0)
    expect(firstLink).toHaveTextContent('Skip to content')
    expect(firstLink).toHaveAttribute('href', '#main')
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main')
    expect(screen.getByRole('main')).toHaveAttribute('tabindex', '-1')
  })
})

describe('AppShell mobile drawer', () => {
  it('opens on the trigger with focus on the close button, and Escape returns focus', async () => {
    const user = userEvent.setup()
    renderShell('/energy')

    const trigger = screen.getByRole('button', { name: 'Open navigation' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')

    await user.click(trigger)

    const dialog = screen.getByRole('dialog', { name: 'Main navigation' })
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    const closeButton = within(dialog).getByRole('button', { name: 'Close navigation' })
    expect(closeButton).toHaveFocus()

    await user.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveFocus()
  })

  it('closes when a destination is chosen', async () => {
    const user = userEvent.setup()
    renderShell('/energy')

    await user.click(screen.getByRole('button', { name: 'Open navigation' }))
    const dialog = screen.getByRole('dialog', { name: 'Main navigation' })
    await user.click(within(dialog).getByRole('link', { name: 'Events' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('closes on an overlay click', async () => {
    const user = userEvent.setup()
    renderShell()

    await user.click(screen.getByRole('button', { name: 'Open navigation' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.click(screen.getByTestId('mobile-nav-overlay'))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('locks the body scroll while open and restores it on close', async () => {
    const user = userEvent.setup()
    renderShell()

    await user.click(screen.getByRole('button', { name: 'Open navigation' }))
    expect(document.body.style.overflow).toBe('hidden')

    await user.keyboard('{Escape}')
    expect(document.body.style.overflow).toBe('')
  })

  it('restores the body scroll when unmounted while open', async () => {
    const user = userEvent.setup()
    const { unmount } = renderShell()

    await user.click(screen.getByRole('button', { name: 'Open navigation' }))
    expect(document.body.style.overflow).toBe('hidden')

    unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('keeps a single main navigation landmark while open', async () => {
    const user = userEvent.setup()
    renderShell()

    await user.click(screen.getByRole('button', { name: 'Open navigation' }))

    expect(screen.getAllByRole('navigation', { name: 'Main navigation' })).toHaveLength(1)
    expect(within(screen.getByRole('dialog')).queryByRole('navigation')).not.toBeInTheDocument()
  })
})
