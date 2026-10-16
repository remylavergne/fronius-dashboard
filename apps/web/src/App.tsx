import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from '@/components/AppShell'
import { EmptyState } from '@/components/EmptyState'
import { useT } from '@/i18n'
import { CapabilitiesPage } from '@/routes/Capabilities'
import { DetailPage } from '@/routes/Detail'
import { EnergyPage } from '@/routes/Energy'
import { EventsPage } from '@/routes/Events'
import { Overview } from '@/routes/Overview'
import { SettingsPage } from '@/routes/Settings'
import { SourcesPage } from '@/routes/Sources'
import { SystemPage } from '@/routes/System'

function NotFound() {
  const { t } = useT()
  return <EmptyState title={t('state.notFound')} hint={t('state.notFoundHint')} />
}

const router = createBrowserRouter([
  { path: '/', element: <AppShell><Overview /></AppShell> },
  { path: '/energy', element: <AppShell><EnergyPage /></AppShell> },
  { path: '/events', element: <AppShell><EventsPage /></AppShell> },
  { path: '/system', element: <AppShell><SystemPage /></AppShell> },
  { path: '/capabilities', element: <AppShell><CapabilitiesPage /></AppShell> },
  { path: '/sources', element: <AppShell><SourcesPage /></AppShell> },
  { path: '/detail', element: <AppShell><DetailPage /></AppShell> },
  { path: '/settings', element: <AppShell><SettingsPage /></AppShell> },
  { path: '*', element: <AppShell><NotFound /></AppShell> },
])

/** Application root: client routing, no global state. */
export function App() {
  return <RouterProvider router={router} />
}
