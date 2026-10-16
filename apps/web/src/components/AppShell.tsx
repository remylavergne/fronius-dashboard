import { CircleDot, Gauge, ListChecks, Settings, SunMedium } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useT } from '@/i18n'
import { cn } from '@/lib/utils'

export type AppShellProps = {
  children: ReactNode
}

/**
 * Structure de l'application : en-tête, navigation latérale, contenu, pied.
 *
 * Repères sémantiques `header` / `nav` / `main` / `footer`, navigation par une
 * liste de liens réels (donc utilisables au clavier et par un lecteur d'écran),
 * et anneau de focus conservé sur les liens actifs.
 */
export function AppShell({ children }: AppShellProps) {
  const { t } = useT()

  const links = [
    { to: '/', label: t('nav.overview'), icon: Gauge },
    { to: '/system', label: t('nav.system'), icon: CircleDot },
    { to: '/capacites', label: t('nav.capabilities'), icon: ListChecks },
    { to: '/parametres', label: t('nav.settings'), icon: Settings },
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
