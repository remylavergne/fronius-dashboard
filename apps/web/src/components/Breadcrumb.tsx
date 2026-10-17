import { ChevronRight } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import { useT } from '@/i18n'
import { NAV_ITEMS } from '@/lib/navigation'

/**
 * Location trail derived from `NAV_ITEMS`.
 *
 * The root of the site is the "Overview" page: it is linked to from every other
 * page, but not from itself (the current page is never a self-link). An address
 * that matches no navigation item belongs to no page we can name, so the
 * breadcrumb renders nothing rather than claim a page the user is not on.
 */
export function Breadcrumb() {
  const { t } = useT()
  const { pathname } = useLocation()
  // Trailing slashes address the same page: `/energy/` and `/energy` must not
  // diverge into "known" and "unknown".
  const path = pathname.replace(/\/+$/, '') || '/'
  const current = NAV_ITEMS.find((item) => item.to === path)

  if (!current) {
    return null
  }

  const root = NAV_ITEMS.find((item) => item.to === '/')
  const items = root && current.to !== '/' ? [root, current] : [current]

  return (
    <nav aria-label={t('breadcrumb.aria')}>
      <ol className="flex items-center gap-1 text-sm text-muted-foreground">
        {items.map((item, index) => (
          <li key={item.to} className="flex items-center gap-1">
            {index > 0 ? <ChevronRight aria-hidden="true" className="size-3.5" /> : null}
            {item.to === current.to ? (
              <span aria-current="page" className="font-medium text-foreground">
                {t(item.labelKey)}
              </span>
            ) : (
              <Link
                to={item.to}
                className="rounded-sm outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
              >
                {t(item.labelKey)}
              </Link>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}
