import { BatteryCharging, BellRing, CircleDot, CircuitBoard, Database, Gauge, ListChecks, Settings, SunMedium } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useT } from '@/i18n'
import { cn } from '@/lib/utils'

export type AppShellProps = {
  children: ReactNode
}

/**
 * Application structure: header, side navigation, content, footer.
 *
 * Semantic landmarks `header` / `nav` / `main` / `footer`, navigation by a list
 * of real links (therefore usable with the keyboard and by a screen reader),
 * and focus ring kept on active links.
 */
export function AppShell({ children }: AppShellProps) {
  const { t } = useT()

  const links = [
    { to: '/', label: t('nav.overview'), icon: Gauge },
    { to: '/energy', label: t('nav.energy'), icon: BatteryCharging },
    // Events sits right after the device detail: both are the device's own
    // account of itself, read by the collector.
    { to: '/detail', label: t('nav.detail'), icon: CircuitBoard },
    { to: '/events', label: t('nav.events'), icon: BellRing },
    { to: '/system', label: t('nav.system'), icon: CircleDot },
    { to: '/capabilities', label: t('nav.capabilities'), icon: ListChecks },
    { to: '/sources', label: t('nav.sources'), icon: Database },
    { to: '/settings', label: t('nav.settings'), icon: Settings },
  ]

  return (
    <div className="flex min-h-dvh flex-col bg-background text-foreground">
      <header className="border-b px-4 py-3">
        <div className="mx-auto flex max-w-6xl items-center gap-3">
          <SunMedium aria-hidden="true" className="size-5 text-primary" />
          <div>
            <p className="text-sm font-semibold">{t('app.title')}</p>
            <p className="text-xs text-muted-foreground">{t('app.tagline')}</p>
          </div>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-6xl flex-1 flex-col gap-6 px-4 py-6 md:flex-row">
        <nav aria-label={t('nav.aria')} className="md:w-52 md:shrink-0">
          <ul className="flex flex-wrap gap-1 md:flex-col">
            {links.map(({ to, label, icon: Icon }) => (
              <li key={to}>
                <NavLink
                  to={to}
                  end={to === '/'}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-2 rounded-md px-3 py-2 text-sm outline-none transition-colors',
                      'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
                      isActive
                        ? 'bg-muted font-medium text-foreground'
                        : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                    )
                  }
                >
                  <Icon aria-hidden="true" className="size-4" />
                  {label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        <main className="min-w-0 flex-1">{children}</main>
      </div>

      <footer className="border-t px-4 py-4 text-center text-xs text-muted-foreground">
        {t('footer.sources')}
      </footer>
    </div>
  )
}
