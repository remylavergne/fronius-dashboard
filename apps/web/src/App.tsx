import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from '@/components/AppShell'
import { EmptyState } from '@/components/EmptyState'
import { useT } from '@/i18n'
import { CapabilitiesPage } from '@/routes/Capabilities'
import { Overview } from '@/routes/Overview'
import { SettingsPage } from '@/routes/Settings'
import { SystemPage } from '@/routes/System'

function NotFound() {
  const { t } = useT()
  return <EmptyState title={t('state.notFound')} hint={t('state.notFoundHint')} />
}

const router = createBrowserRouter([
  { path: '/', element: <AppShell><Overview /></AppShell> },
  { path: '/system', element: <AppShell><SystemPage /></AppShell> },
  { path: '/capacites', element: <AppShell><CapabilitiesPage /></AppShell> },
  { path: '/parametres', element: <AppShell><SettingsPage /></AppShell> },
  { path: '*', element: <AppShell><NotFound /></AppShell> },
])

/** Racine de l'application : routage client, sans état global. */
export function App() {
  return <RouterProvider router={router} />
}
