import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from '@/components/AppShell'
import { ThemeProvider } from './theme.tsx'
import { THEME_COLORS, applyTheme, resolveTheme, useTheme } from './theme'

type ChangeListener = (event: MediaQueryListEvent) => void

const originalMatchMedia = window.matchMedia
const hadMatchMedia = 'matchMedia' in window

/**
 * jsdom has no `matchMedia`. The stub records listeners so a test can flip the
 * OS preference; `afterEach` removes it again.
 */
function stubMatchMedia(matches: boolean) {
  const listeners = new Set<ChangeListener>()
  const query = {
    matches,
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: (_type: string, listener: ChangeListener) => {
      listeners.add(listener)
    },
    removeEventListener: (_type: string, listener: ChangeListener) => {
      listeners.delete(listener)
    },
    addListener: (listener: ChangeListener) => {
      listeners.add(listener)
    },
    removeListener: (listener: ChangeListener) => {
      listeners.delete(listener)
    },
    dispatchEvent: () => false,
  }
  window.matchMedia = vi.fn(() => query) as unknown as typeof window.matchMedia

  return {
    setMatches(next: boolean) {
      query.matches = next
      for (const listener of listeners) {
        listener({ matches: next } as MediaQueryListEvent)
      }
    },
  }
}

function ThemeProbe() {
  const { mode, resolved } = useTheme()
  return <output data-testid="theme-state">{`${mode}:${resolved}`}</output>
}

function renderProbe() {
  return render(
    <ThemeProvider>
      <ThemeProbe />
    </ThemeProvider>,
  )
}

function renderShell() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <AppShell>
        <p>content</p>
      </AppShell>
    </MemoryRouter>,
  )
}

function themeColorMeta() {
  return document.querySelector('meta[name="theme-color"]')
}

let meta: HTMLMetaElement

beforeEach(() => {
  localStorage.clear()
  meta = document.createElement('meta')
  meta.setAttribute('name', 'theme-color')
  meta.setAttribute('content', '#ffffff')
  document.head.append(meta)
})

afterEach(() => {
  meta.remove()
  localStorage.clear()
  if (hadMatchMedia) {
    window.matchMedia = originalMatchMedia
  } else {
    Reflect.deleteProperty(window, 'matchMedia')
  }
  document.documentElement.classList.remove('dark')
  document.documentElement.style.colorScheme = ''
})

describe('resolveTheme', () => {
  it('lets system follow the preference, and explicit modes win over it', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })
})

describe('theme application', () => {
  it('sets the class, the color-scheme and the theme-color meta', () => {
    applyTheme('dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(document.documentElement.style.colorScheme).toBe('dark')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.dark)

    applyTheme('light')
    expect(document.documentElement).not.toHaveClass('dark')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.light)
  })
})

describe('ThemeProvider with system mode', () => {
  it('resolves system to dark when the OS query matches', () => {
    stubMatchMedia(true)
    renderProbe()

    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(document.documentElement.style.colorScheme).toBe('dark')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.dark)
  })

  it('resolves system to light when the OS query does not match', () => {
    stubMatchMedia(false)
    renderProbe()

    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')
    expect(document.documentElement).not.toHaveClass('dark')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.light)
  })

  it('follows the OS setting live while in system mode', () => {
    const media = stubMatchMedia(false)
    renderProbe()
    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')

    act(() => media.setMatches(true))

    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.dark)
  })

  it('ignores the OS setting when an explicit mode is stored', () => {
    stubMatchMedia(true)
    localStorage.setItem('theme', 'light')
    renderProbe()

    expect(screen.getByTestId('theme-state')).toHaveTextContent('light:light')
    expect(document.documentElement).not.toHaveClass('dark')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.light)
  })

  it('treats an invalid stored value as system', () => {
    stubMatchMedia(false)
    localStorage.setItem('theme', 'sepia')
    renderProbe()

    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')
  })

  it('switches another tab on a storage event', () => {
    stubMatchMedia(false)
    renderProbe()
    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')

    act(() => {
      localStorage.setItem('theme', 'dark')
      window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark' }))
    })

    expect(screen.getByTestId('theme-state')).toHaveTextContent('dark:dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(themeColorMeta()).toHaveAttribute('content', THEME_COLORS.dark)
  })

  it('returns to system when another tab clears or invalidates the key', () => {
    stubMatchMedia(false)
    localStorage.setItem('theme', 'dark')
    renderProbe()
    expect(screen.getByTestId('theme-state')).toHaveTextContent('dark:dark')

    act(() => {
      localStorage.setItem('theme', 'sepia')
      window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'sepia' }))
    })

    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')
  })
})

describe('header theme toggle', () => {
  it('cycles system → light → dark → system with the current mode visible and announced', async () => {
    const user = userEvent.setup()
    stubMatchMedia(true)
    renderShell()

    const system = screen.getByRole('button', { name: 'Theme: System' })
    expect(system).toHaveTextContent('System')
    expect(system).toHaveAttribute('title', 'Switch theme: system, light or dark')
    expect(document.documentElement).toHaveClass('dark')

    await user.click(system)
    const light = screen.getByRole('button', { name: 'Theme: Light' })
    expect(light).toHaveTextContent('Light')
    expect(document.documentElement).not.toHaveClass('dark')
    expect(localStorage.getItem('theme')).toBe('light')
    expect(themeColorMeta()).toHaveAttribute('content', '#ffffff')

    await user.click(light)
    const dark = screen.getByRole('button', { name: 'Theme: Dark' })
    expect(dark).toHaveTextContent('Dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(themeColorMeta()).toHaveAttribute('content', '#171717')

    await user.click(dark)
    expect(screen.getByRole('button', { name: 'Theme: System' })).toHaveTextContent('System')
    expect(localStorage.getItem('theme')).toBe('system')
  })

  it('restores an explicit stored mode on the next mount', async () => {
    const user = userEvent.setup()
    stubMatchMedia(true)
    const first = renderShell()
    await user.click(screen.getByRole('button', { name: 'Theme: System' }))
    expect(localStorage.getItem('theme')).toBe('light')
    first.unmount()

    renderShell()

    expect(screen.getByRole('button', { name: 'Theme: Light' })).toBeInTheDocument()
    expect(document.documentElement).not.toHaveClass('dark')
  })
})
