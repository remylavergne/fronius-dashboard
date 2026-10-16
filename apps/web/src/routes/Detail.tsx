import { useId, useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { ApiAlert } from '@/components/ApiAlert'
import { EmptyState } from '@/components/EmptyState'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { formatDateTime, formatKWh, formatW } from '@/lib/format'
import { useDetail } from '@/lib/queries'
import {
  detailCacheDataSchema,
  detailDevicesDataSchema,
  detailEntriesDataSchema,
  detailInverterDataSchema,
  detailNetworkDataSchema,
  detailSystemDataSchema,
  findDetailFamily,
  parseDetailFamilyData,
} from '@/lib/schema'
import type {
  Detail,
  DetailEnergies,
  DetailFamily,
  DetailInverterState,
  DetailMeasurement,
} from '@/lib/schema'
import { cn } from '@/lib/utils'

/** Dash of an absent value: never a zero, never a blank cell. */
const MISSING = '—'

/**
 * Readable decimal of a cache `float`. Grouped like every other number of the
 * interface, and capped so a long binary tail does not swamp the cell. An `int`
 * never goes through it: a counter is displayed as the digits that arrived.
 */
const cacheFloat = new Intl.NumberFormat('en-GB', { maximumFractionDigits: 6 })

/**
 * Families this page renders with a shape of its own, in display order. A family
 * the backend adds later, and which is not listed here, still renders — through
 * the generic `entries` shape.
 */
const KNOWN_FAMILIES = ['inverter', 'system', 'network', 'devices', 'emrs', 'cache'] as const
type KnownFamily = (typeof KNOWN_FAMILIES)[number]

const FAMILY_TITLES: Record<KnownFamily, TranslationKey> = {
  inverter: 'detail.family.inverter',
  system: 'detail.family.system',
  network: 'detail.family.network',
  devices: 'detail.family.devices',
  emrs: 'detail.family.emrs',
  cache: 'detail.family.cache',
}

/** Text of a measurement value: base units, a dash when the device reported none. */
function measurementText(measurement: DetailMeasurement): string {
  const { value, unit } = measurement
  if (value === null || value === undefined) {
    return MISSING
  }
  // The interface formats in the unit the reader reasons in (kW, kWh), which is
  // what `formatW` / `formatKWh` do; every other unit is the device's own and is
  // displayed as it arrives.
  if (unit === 'W') {
    const formatted = formatW(value)
    return `${formatted.value} ${formatted.unit}`.trim()
  }
  if (unit === 'Wh') {
    const formatted = formatKWh(value)
    return `${formatted.value} ${formatted.unit}`.trim()
  }
  return `${value} ${unit ?? ''}`.trim()
}

/** Text of an energy in Wh; `null` — the device reported nothing — stays a dash. */
function energyText(wattHours: number | null | undefined): string {
  const formatted = formatKWh(wattHours)
  return `${formatted.value} ${formatted.unit}`.trim()
}

/**
 * Text of a flag. Absent is not `false`: the device did not answer, so the cell
 * gets a dash rather than a "no" it never said.
 */
function flagText(value: boolean | null | undefined, yes: string, no: string): string {
  if (value === null || value === undefined) {
    return MISSING
  }
  return value ? yes : no
}

/** Text of a scalar of a generic family; a shape not anticipated renders as JSON. */
function scalarText(value: unknown, yes: string, no: string): string {
  if (value === null || value === undefined) {
    return MISSING
  }
  if (typeof value === 'boolean') {
    return value ? yes : no
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'number') {
    return String(value)
  }
  return JSON.stringify(value) ?? MISSING
}

/** The answer of a family this build cannot read: said, never rendered empty. */
function Unreadable() {
  const { t } = useT()
  return <p className="text-sm text-muted-foreground">{t('detail.unreadable')}</p>
}

/** Absence of rows, named: an empty table is not a table full of zeros. */
function NoRows() {
  const { t } = useT()
  return <p className="text-sm text-muted-foreground">{t('detail.none')}</p>
}

/** One labelled group of rows inside a family block. */
function SubSection({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <p className="text-sm font-medium">{label}</p>
      {children}
    </div>
  )
}

/**
 * One family block.
 *
 * Three states, rendered distinctly: the body when the family is available, its
 * `reason` when it is not, and "never read" when it is absent from the answer.
 * The header always carries the instant *this* family was read.
 */
function Block({
  title,
  family,
  children,
}: {
  title: string
  family: DetailFamily | undefined
  children: ReactNode
}) {
  const { t } = useT()
  const titleId = useId()

  let body: ReactNode
  if (!family) {
    body = <p className="text-sm text-muted-foreground">{t('detail.family.neverReadHint')}</p>
  } else if (!family.available) {
    body = (
      <p className="text-sm">
        <Badge variant="outline">{t('detail.family.unavailable')}</Badge>{' '}
        <span className="text-muted-foreground">
          {family.reason ? family.reason : t('detail.family.unavailable')}
        </span>
      </p>
    )
  } else {
    body = children
  }

  return (
    <Card role="article" aria-labelledby={titleId}>
      <CardHeader>
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <CardTitle id={titleId}>{title}</CardTitle>
          <span className="text-xs text-muted-foreground">
            {family?.read_at
              ? `${t('detail.readAt')}: ${formatDateTime(family.read_at)}`
              : t('detail.family.neverRead')}
          </span>
        </div>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}

/** Inverter: state and its codes, the measurements table, then the energies. */
function InverterBody({ family }: { family: DetailFamily }) {
  const { t } = useT()
  const data = parseDetailFamilyData(family, detailInverterDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const state: DetailInverterState | null = data.state ?? null
  const measurements = data.measurements ?? []
  const energies: DetailEnergies | null = data.energies ?? null

  return (
    <div className="space-y-5">
      <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[max-content_max-content]">
        <dt className="text-muted-foreground">{t('detail.inverter.state')}</dt>
        <dd className="font-medium">{state?.inverter_state ?? MISSING}</dd>
        <dt className="text-muted-foreground">{t('detail.inverter.statusCode')}</dt>
        <dd className="tabular-nums">{state?.status_code ?? MISSING}</dd>
        <dt className="text-muted-foreground">{t('detail.inverter.errorCode')}</dt>
        <dd className="tabular-nums">{state?.error_code ?? MISSING}</dd>
      </dl>

      <SubSection label={t('detail.inverter.measurements')}>
        {measurements.length > 0 ? (
          <Table>
            <TableCaption className="sr-only">{t('detail.inverter.measurements')}</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">{t('detail.col.label')}</TableHead>
                <TableHead scope="col">{t('detail.col.key')}</TableHead>
                <TableHead scope="col">{t('detail.col.unit')}</TableHead>
                <TableHead scope="col" className="text-right">
                  {t('detail.col.value')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {/* The order is the backend's: this page never reorders it. */}
              {measurements.map((measurement, index) => (
                <TableRow key={`${measurement.key}-${index}`}>
                  <TableCell>{measurement.label ?? MISSING}</TableCell>
                  <TableCell className="font-mono text-xs">{measurement.key}</TableCell>
                  <TableCell>{measurement.unit ?? MISSING}</TableCell>
                  <TableCell className="text-right tabular-nums">{measurementText(measurement)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <NoRows />
        )}
      </SubSection>

      <SubSection label={t('detail.inverter.energies')}>
        <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[max-content_max-content]">
          <dt className="text-muted-foreground">{t('detail.inverter.day')}</dt>
          <dd className="tabular-nums">{energyText(energies?.day_wh)}</dd>
          <dt className="text-muted-foreground">{t('detail.inverter.year')}</dt>
          <dd className="tabular-nums">{energyText(energies?.year_wh)}</dd>
          <dt className="text-muted-foreground">{t('detail.inverter.total')}</dt>
          <dd className="tabular-nums">{energyText(energies?.total_wh)}</dd>
        </dl>
      </SubSection>
    </div>
  )
}

/** System: identity, service flags, declared accounts, then the interfaces. */
function SystemBody({ family }: { family: DetailFamily }) {
  const { t } = useT()
  const data = parseDetailFamilyData(family, detailSystemDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const interfaces = data.interfaces ?? []
  // A record has no order of its own: sorting by account name makes the block
  // stable from one refresh to the next.
  const accounts = Object.entries(data.accounts ?? {}).sort(([left], [right]) =>
    left < right ? -1 : left > right ? 1 : 0,
  )

  return (
    <div className="space-y-5">
      <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[max-content_1fr]">
        <dt className="text-muted-foreground">{t('detail.system.name')}</dt>
        <dd>{data.system_name ?? MISSING}</dd>
        <dt className="text-muted-foreground">{t('detail.system.serviceState')}</dt>
        <dd>{data.service_state ?? MISSING}</dd>
        <dt className="text-muted-foreground">{t('detail.system.updateRunning')}</dt>
        <dd>{flagText(data.update_running, t('detail.yes'), t('detail.no'))}</dd>
        <dt className="text-muted-foreground">{t('detail.system.supportEnabled')}</dt>
        <dd>{flagText(data.support_enabled, t('detail.yes'), t('detail.no'))}</dd>
        <dt className="text-muted-foreground">{t('detail.system.connectedViaAp')}</dt>
        <dd>{flagText(data.connected_via_ap, t('detail.yes'), t('detail.no'))}</dd>
        <dt className="text-muted-foreground">{t('detail.system.accounts')}</dt>
        <dd>
          {accounts.length > 0 ? (
            <ul className="space-y-0.5">
              {accounts.map(([name, granted]) => (
                <li key={name} className="flex items-baseline gap-1.5">
                  <span className="font-mono text-xs">{name}</span>
                  <span>{flagText(granted, t('detail.yes'), t('detail.no'))}</span>
                </li>
              ))}
            </ul>
          ) : (
            <span className="text-muted-foreground">{t('detail.system.accountsNone')}</span>
          )}
        </dd>
      </dl>

      <SubSection label={t('detail.system.interfaces')}>
        {interfaces.length > 0 ? (
          <Table>
            <TableCaption className="sr-only">{t('detail.system.interfaces')}</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">{t('detail.col.name')}</TableHead>
                <TableHead scope="col">{t('detail.col.interfaceType')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {interfaces.map((declared, index) => (
                <TableRow key={`${declared.name ?? MISSING}-${index}`}>
                  <TableCell className="font-mono text-xs">{declared.name ?? MISSING}</TableCell>
                  <TableCell>{declared.type ?? MISSING}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <NoRows />
        )}
      </SubSection>
    </div>
  )
}

/** Network: the FDMP flag, then one row per interface. */
function NetworkBody({ family }: { family: DetailFamily }) {
  const { t } = useT()
  const data = parseDetailFamilyData(family, detailNetworkDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const interfaces = data.interfaces ?? []

  return (
    <div className="space-y-5">
      <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[max-content_max-content]">
        <dt className="text-muted-foreground">{t('detail.network.fdmp')}</dt>
        <dd>{flagText(data.fdmp_connected, t('detail.yes'), t('detail.no'))}</dd>
      </dl>

      <SubSection label={t('detail.network.interfaces')}>
        {interfaces.length > 0 ? (
          <Table>
            <TableCaption className="sr-only">{t('detail.network.interfaces')}</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">{t('detail.col.name')}</TableHead>
                <TableHead scope="col">{t('detail.col.ip')}</TableHead>
                <TableHead scope="col">{t('detail.col.mask')}</TableHead>
                <TableHead scope="col">{t('detail.col.gateway')}</TableHead>
                <TableHead scope="col">{t('detail.col.dns')}</TableHead>
                <TableHead scope="col">{t('detail.col.configured')}</TableHead>
                <TableHead scope="col">{t('detail.col.mode')}</TableHead>
                <TableHead scope="col">{t('detail.col.status')}</TableHead>
                <TableHead scope="col">{t('detail.col.ssid')}</TableHead>
                <TableHead scope="col">{t('detail.col.wps')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {interfaces.map((networkInterface, index) => {
                const dns = networkInterface.dns ?? []
                return (
                  <TableRow key={`${networkInterface.name}-${index}`}>
                    <TableCell className="font-mono text-xs">{networkInterface.name}</TableCell>
                    <TableCell className="font-mono text-xs">{networkInterface.ip ?? MISSING}</TableCell>
                    <TableCell className="font-mono text-xs">{networkInterface.mask ?? MISSING}</TableCell>
                    <TableCell className="font-mono text-xs">{networkInterface.gateway ?? MISSING}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {dns.length > 0 ? dns.join(', ') : MISSING}
                    </TableCell>
                    <TableCell>{networkInterface.configured ?? MISSING}</TableCell>
                    <TableCell>{networkInterface.interface_mode ?? MISSING}</TableCell>
                    <TableCell>{networkInterface.status ?? MISSING}</TableCell>
                    <TableCell>{networkInterface.ssid ?? MISSING}</TableCell>
                    <TableCell>{networkInterface.wps_state ?? MISSING}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        ) : (
          <NoRows />
        )}
      </SubSection>
    </div>
  )
}

/** Devices: one row per declared device, with its status code and message. */
function DevicesBody({ family, title }: { family: DetailFamily; title: string }) {
  const { t } = useT()
  const data = parseDetailFamilyData(family, detailDevicesDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const devices = data.devices ?? []
  if (devices.length === 0) {
    return <NoRows />
  }

  return (
    <Table>
      <TableCaption className="sr-only">{title}</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead scope="col">{t('detail.col.deviceType')}</TableHead>
          <TableHead scope="col">{t('detail.col.statusCode')}</TableHead>
          <TableHead scope="col">{t('detail.col.statusMessage')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {devices.map((device, index) => (
          <TableRow key={`${device.type ?? MISSING}-${index}`}>
            <TableCell className="font-mono text-xs">{device.type ?? MISSING}</TableCell>
            <TableCell className="tabular-nums">{device.status ?? MISSING}</TableCell>
            <TableCell>{device.status_message ?? MISSING}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/**
 * Cache: the inverter's internal register set — the richest diagnostic the
 * device exposes, and the one it names itself.
 *
 * The attributes come first (the device labels those), then the registers,
 * inside a disclosure: they number 81 on the reference unit, and open they would
 * bury the rest of the page. The registers keep the order the backend sent
 * (named ones first, then the bare `<id>` identifiers), a bare identifier is
 * displayed verbatim — no name is invented for it — and the type suffix carries
 * a subdued badge, never a translation. No unit is appended: the cache has none.
 */
function CacheBody({ family, title }: { family: DetailFamily; title: string }) {
  const { t } = useT()
  const [expanded, setExpanded] = useState(false)
  const registersId = useId()
  const data = parseDetailFamilyData(family, detailCacheDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const nodes = data.nodes ?? []
  if (nodes.length === 0) {
    return <NoRows />
  }

  const channels = nodes.flatMap((node) => node.channels ?? [])
  // A register the device names is the readable half of the cache: the count of
  // them, next to the total, is what tells a full node from a mostly opaque one.
  const named = channels.filter((channel) => (channel.name ?? '') !== '').length

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">{t('detail.cache.note')}</p>

      {nodes.map((node, nodeIndex) => {
        const attributes = node.attributes ?? []
        return (
          <div key={`${node.id}-${String(nodeIndex)}`} className="space-y-1">
            <p className="font-mono text-xs text-muted-foreground">
              {`${t('detail.cache.node')} ${node.id}`}
            </p>
            {attributes.length > 0 ? (
              <>
                <p className="text-sm font-medium">{t('detail.cache.attributes')}</p>
                <ul className="space-y-0.5 text-sm">
                  {attributes.map((attribute, index) => (
                    <li
                      key={`${attribute.key}-${String(index)}`}
                      className="flex flex-wrap items-baseline gap-1.5"
                    >
                      <span className="font-mono text-xs break-all">{attribute.key}</span>
                      <span aria-hidden="true" className="text-muted-foreground">
                        ·
                      </span>
                      <span className="break-all">{attribute.value ?? MISSING}</span>
                    </li>
                  ))}
                </ul>
              </>
            ) : null}
          </div>
        )
      })}

      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={registersId}
        onClick={() => setExpanded((value) => !value)}
        className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
      >
        <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', expanded && 'rotate-180')} />
        {`${String(channels.length)} ${t('detail.cache.registers')} · ${String(named)} ${t('detail.cache.named')}`}
      </button>

      {/*
        The registers stay in the document and are hidden by the `hidden`
        attribute: `aria-controls` therefore always names an element that exists,
        and whether they are disclosed stays readable through `aria-expanded`.
      */}
      <div id={registersId} hidden={!expanded}>
        {channels.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('detail.cache.none')}</p>
        ) : (
          nodes.map((node, nodeIndex) => {
            const registers = node.channels ?? []
            if (registers.length === 0) {
              return null
            }
            return (
              <Table key={`${node.id}-${String(nodeIndex)}`}>
                <TableCaption className="sr-only">
                  {`${title} — ${t('detail.cache.node')} ${node.id}`}
                </TableCaption>
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">{t('detail.cache.col.register')}</TableHead>
                    <TableHead scope="col">{t('detail.cache.col.kind')}</TableHead>
                    <TableHead scope="col" className="text-right">
                      {t('detail.col.value')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {/* The order is the backend's: this page never reorders it. */}
                  {registers.map((channel, index) => (
                    <TableRow key={`${channel.key}-${String(index)}`}>
                      <TableCell className="font-mono text-xs break-all">
                        {channel.name ? channel.name : channel.key}
                      </TableCell>
                      <TableCell>
                        {channel.kind ? (
                          <Badge variant="outline" className="font-mono text-muted-foreground">
                            {channel.kind}
                          </Badge>
                        ) : (
                          MISSING
                        )}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {/*
                          The device's own `type` decides: a `float` becomes a
                          readable decimal, every other type goes through the
                          scalar rule — an `int` keeps the digits it arrived
                          with (a counter is exact), a `null` is a dash. No unit
                          is appended, since the cache carries none.
                        */}
                        {channel.type === 'float' &&
                        typeof channel.value === 'number' &&
                        Number.isFinite(channel.value)
                          ? cacheFloat.format(channel.value)
                          : scalarText(channel.value, t('detail.yes'), t('detail.no'))}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )
          })
        )}
      </div>
    </div>
  )
}

/** Generic families (EMRS, and any family the backend adds): the flat path table. */
function EntriesBody({ family, title }: { family: DetailFamily; title: string }) {
  const { t } = useT()
  const data = parseDetailFamilyData(family, detailEntriesDataSchema)
  if (!data) {
    return <Unreadable />
  }

  const entries = data.entries ?? []
  if (entries.length === 0) {
    return <NoRows />
  }

  return (
    <Table>
      <TableCaption className="sr-only">{title}</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead scope="col">{t('detail.col.path')}</TableHead>
          <TableHead scope="col">{t('detail.col.value')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {entries.map((entry, index) => (
          <TableRow key={`${entry.path}-${index}`}>
            <TableCell className="font-mono text-xs break-all">{entry.path}</TableCell>
            <TableCell>{scalarText(entry.value, t('detail.yes'), t('detail.no'))}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/**
 * Device detail: what the device answers on its own interface, read by the
 * collector on a slow cycle and displayed as it stands.
 *
 * Nothing here is a live value: each block carries the instant *its* family was
 * read. A family may be absent (never read), present and unavailable (with the
 * backend's reason), or available with a typed body; those three states are
 * rendered distinctly, and an absent value is a dash — never a zero.
 */
export function DetailPage() {
  const { t } = useT()
  const detail = useDetail()
  const payload: Detail | undefined = detail.data?.data
  const families = payload?.families ?? []
  const extraFamilies = families.filter(
    (family) => !(KNOWN_FAMILIES as readonly string[]).includes(family.family),
  )
  const inverter = findDetailFamily(payload, 'inverter')
  const system = findDetailFamily(payload, 'system')
  const network = findDetailFamily(payload, 'network')
  const devices = findDetailFamily(payload, 'devices')
  const emrs = findDetailFamily(payload, 'emrs')
  const cache = findDetailFamily(payload, 'cache')

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="text-lg font-semibold">{t('detail.title')}</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">{t('detail.intro')}</p>
        {detail.data && payload?.read_at ? (
          <p className="text-xs text-muted-foreground">
            {`${t('detail.readAt')}: ${formatDateTime(payload.read_at)}`}
          </p>
        ) : null}
      </div>

      {detail.isLoading ? <Skeleton className="h-64 w-full" /> : null}

      {detail.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={detail.error instanceof Error ? detail.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void detail.refetch()}
        />
      ) : null}

      {detail.data && payload?.read_at === undefined && families.length === 0 ? (
        <EmptyState title={t('detail.neverRead')} hint={t('detail.neverReadHint')} />
      ) : null}

      {detail.data ? (
        <>
          <Block title={t(FAMILY_TITLES.inverter)} family={inverter}>
            {inverter ? <InverterBody family={inverter} /> : null}
          </Block>
          <Block title={t(FAMILY_TITLES.system)} family={system}>
            {system ? <SystemBody family={system} /> : null}
          </Block>
          <Block title={t(FAMILY_TITLES.network)} family={network}>
            {network ? <NetworkBody family={network} /> : null}
          </Block>
          <Block title={t(FAMILY_TITLES.devices)} family={devices}>
            {devices ? <DevicesBody family={devices} title={t(FAMILY_TITLES.devices)} /> : null}
          </Block>
          <Block title={t(FAMILY_TITLES.emrs)} family={emrs}>
            {emrs ? <EntriesBody family={emrs} title={t(FAMILY_TITLES.emrs)} /> : null}
          </Block>
          <Block title={t(FAMILY_TITLES.cache)} family={cache}>
            {cache ? <CacheBody family={cache} title={t(FAMILY_TITLES.cache)} /> : null}
          </Block>
          {/* A family this build does not know yet still renders, generically. */}
          {extraFamilies.map((family) => (
            <Block key={family.family} title={family.family} family={family}>
              <EntriesBody family={family} title={family.family} />
            </Block>
          ))}
        </>
      ) : null}
    </div>
  )
}
