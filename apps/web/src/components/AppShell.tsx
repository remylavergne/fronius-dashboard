import { Menu, Moon, Sun, SunMedium, SunMoon, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { Breadcrumb } from '@/components/Breadcrumb'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { NAV_GROUPS, NAV_ITEMS } from '@/lib/navigation'
import { ThemeProvider } from '@/lib/theme.tsx'
import { useTheme } from '@/lib/theme'
import type { ThemeMode } from '@/lib/theme'
import { cn } from '@/lib/utils'

export type AppShellProps = {
  children: ReactNode
}

const FOCUS_RING =
  'outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background'

const THEME_LABEL_KEYS: Record<ThemeMode, TranslationKey> = {
  system: 'theme.mode.system',
  light: 'theme.mode.light',
  dark: 'theme.mode.dark',
}

const THEME_ICONS: Record<ThemeMode, typeof Sun> = {
  system: SunMoon,
  light: Sun,
  dark: Moon,
}

/** Header control: the visible text and the accessible name are the current mode. */
function ThemeToggle() {
  const { t } = useT()
  const { mode, cycleMode } = useTheme()
  const label = t(THEME_LABEL_KEYS[mode])
  const Icon = THEME_ICONS[mode]

  return (
    <button
      type="button"
      onClick={cycleMode}
      title={t('theme.hint')}
      aria-label={`${t('theme.label')}: ${label}`}
      className={cn(
        'ml-auto inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs font-medium',
        'text-muted-foreground transition-colors hover:bg-muted hover:text-foreground sm:px-2.5 sm:text-sm',
        FOCUS_RING,
      )}
    >
      <Icon aria-hidden="true" className="size-4 shrink-0" />
      <span>{label}</span>
    </button>
  )
}

/** Brand block shared by the sidebar and the drawer; links back to the overview. */
function Brand({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useT()
  return (
    <Link
      to="/"
      onClick={onNavigate}
      className={cn('flex items-center gap-2 rounded-md px-1 py-1', FOCUS_RING)}
    >
      <SunMedium aria-hidden="true" className="size-5 shrink-0 text-primary" />
      <span className="min-w-0">
        <span className="block truncate text-sm font-semibold">{t('app.title')}</span>
        <span className="block truncate text-xs text-muted-foreground">{t('app.tagline')}</span>
      </span>
    </Link>
  )
}

/** Grouped links for the desktop sidebar; the group label is not a heading. */
function SidebarNav() {
  const { t } = useT()
  return (
    <>
      {NAV_GROUPS.map((group) => {
        const labelId = `sidebar-nav-${group.id}`
        return (
          <div key={group.id} className="mt-4 first:mt-0">
            <p
              id={labelId}
              className="px-3 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground"
            >
              {t(group.labelKey)}
            </p>
            <ul aria-labelledby={labelId} className="space-y-1">
              {NAV_ITEMS.filter((item) => item.group === group.id).map(({ to, labelKey, icon: Icon }) => (
                <li key={to}>
                  <NavLink
                    to={to}
                    end={to === '/'}
                    className={({ isActive }) =>
                      cn(
                        'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                        FOCUS_RING,
                        isActive
                          ? 'bg-muted font-medium text-foreground'
                          : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                      )
                    }
                  >
                    <Icon aria-hidden="true" className="size-4 shrink-0" />
                    {t(labelKey)}
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </>
  )
}

/**
 * Application structure: skip link, sidebar, sticky header with breadcrumb and
 * theme toggle, content, and a hand-written mobile drawer.
 *
 * The navigation table in `lib/navigation` is the single source of truth; the
 * sidebar and the drawer render the same items, and the breadcrumb reads the
 * same table. The drawer is not a `<dialog>`: its focus handling and scroll
 * lock are written out, which keeps them observable in jsdom.
 *
 * `ThemeProvider` lives here, around the shell, so every routed page gets the
 * same preference without each page having to mount it.
 */
export function AppShell({ children }: AppShellProps) {
  return (
    <ThemeProvider>
      <AppShellLayout>{children}</AppShellLayout>
    </ThemeProvider>
  )
}

function AppShellLayout({ children }: AppShellProps) {
  const { t } = useT()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const shouldRestoreFocus = useRef(false)

  const closeDrawer = () => setDrawerOpen(false)

  // While the drawer is open: lock the page scroll, move focus into the drawer,
  // and close on Escape. The cleanup runs on close *and* on unmount, so neither
  // path can leave the page locked.
  useEffect(() => {
    if (!drawerOpen) {
      return
    }
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeRef.current?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setDrawerOpen(false)
      }
    }
    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previousOverflow
    }
  }, [drawerOpen])

  // Return focus to the trigger when the drawer closes, but never on unmount:
  // the trigger may no longer belong to the document.
  useEffect(() => {
    if (drawerOpen) {
      shouldRestoreFocus.current = true
      return
    }
    if (shouldRestoreFocus.current) {
      shouldRestoreFocus.current = false
      triggerRef.current?.focus()
    }
  }, [drawerOpen])

  return (
    <div className="flex min-h-dvh bg-background text-foreground">
      <a
        href="#main"
        className={cn(
          'sr-only rounded-md bg-background px-3 py-2 text-sm font-medium',
          'focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50',
          FOCUS_RING,
        )}
      >
        {t('shell.skip')}
      </a>

      <aside className="hidden w-60 shrink-0 border-r lg:sticky lg:top-0 lg:flex lg:h-dvh lg:flex-col">
        <div className="px-4 py-4">
          <Brand />
        </div>
        <nav aria-label={t('nav.aria')} className="flex-1 overflow-y-auto px-2 py-3">
          <SidebarNav />
        </nav>
        <p className="border-t px-4 py-3 text-xs text-muted-foreground">{t('footer.sources')}</p>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b bg-background/90 px-4 backdrop-blur">
          <button
            ref={triggerRef}
            type="button"
            aria-expanded={drawerOpen}
            aria-controls="mobile-nav"
            aria-label={t('shell.menu.open')}
            onClick={() => setDrawerOpen(true)}
            className={cn(
              'inline-flex size-9 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground lg:hidden',
              FOCUS_RING,
            )}
          >
            <Menu aria-hidden="true" className="size-5" />
          </button>
          <div className="min-w-0 overflow-hidden py-1">
            <Breadcrumb />
          </div>
          <ThemeToggle />
        </header>

        <main id="main" tabIndex={-1} className="min-w-0 flex-1 px-4 py-6 outline-none lg:px-8">
          {children}
        </main>
      </div>

      {drawerOpen ? (
        <>
          <div
            data-testid="mobile-nav-overlay"
            aria-hidden="true"
            onClick={closeDrawer}
            className="fixed inset-0 z-40 bg-black/40 lg:hidden"
          />
          <div
            id="mobile-nav"
            role="dialog"
            aria-modal="true"
            aria-label={t('nav.aria')}
            className="fixed left-0 top-0 z-50 flex h-dvh w-72 flex-col border-r bg-background shadow-xl lg:hidden"
          >
            <div className="flex items-start justify-between gap-2 px-4 py-3">
              <Brand onNavigate={closeDrawer} />
              <button
                ref={closeRef}
                type="button"
                aria-label={t('shell.menu.close')}
                onClick={closeDrawer}
                className={cn(
                  'inline-flex size-9 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground',
                  FOCUS_RING,
                )}
              >
                <X aria-hidden="true" className="size-5" />
              </button>
            </div>
            <ul className="flex-1 space-y-1 overflow-y-auto px-2 py-3">
              {NAV_ITEMS.map(({ to, labelKey, icon: Icon }) => (
                <li key={to}>
                  <NavLink
                    to={to}
                    end={to === '/'}
                    onClick={closeDrawer}
                    className={({ isActive }) =>
                      cn(
                        'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                        FOCUS_RING,
                        isActive
                          ? 'bg-muted font-medium text-foreground'
                          : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                      )
                    }
                  >
                    <Icon aria-hidden="true" className="size-4 shrink-0" />
                    {t(labelKey)}
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        </>
      ) : null}
    </div>
  )
}
