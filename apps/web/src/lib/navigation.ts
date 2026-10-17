import {
  BatteryCharging,
  BellRing,
  CircleDot,
  CircuitBoard,
  Database,
  Gauge,
  ListChecks,
  Settings,
  type LucideIcon,
} from 'lucide-react'
import type { TranslationKey } from '@/i18n'

export type NavGroupId = 'monitoring' | 'device' | 'application'

/**
 * Navigation groups, in display order.
 *
 * The sidebar, the mobile drawer and the breadcrumb all derive from this table
 * and from `NAV_ITEMS`: their order and labels cannot drift apart.
 */
export const NAV_GROUPS: readonly { id: NavGroupId; labelKey: TranslationKey }[] = [
  { id: 'monitoring', labelKey: 'nav.group.monitoring' },
  { id: 'device', labelKey: 'nav.group.device' },
  { id: 'application', labelKey: 'nav.group.application' },
]

/** Every destination of the application, in the order the sidebar shows them. */
export const NAV_ITEMS: readonly {
  to: string
  labelKey: TranslationKey
  icon: LucideIcon
  group: NavGroupId
}[] = [
  { to: '/', labelKey: 'nav.overview', icon: Gauge, group: 'monitoring' },
  { to: '/energy', labelKey: 'nav.energy', icon: BatteryCharging, group: 'monitoring' },
  { to: '/events', labelKey: 'nav.events', icon: BellRing, group: 'monitoring' },
  { to: '/detail', labelKey: 'nav.detail', icon: CircuitBoard, group: 'device' },
  { to: '/system', labelKey: 'nav.system', icon: CircleDot, group: 'device' },
  { to: '/capabilities', labelKey: 'nav.capabilities', icon: ListChecks, group: 'device' },
  { to: '/sources', labelKey: 'nav.sources', icon: Database, group: 'application' },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings, group: 'application' },
]
