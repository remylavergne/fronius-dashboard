import type { ComponentProps } from 'react'
import { ApiAlert } from '@/components/ApiAlert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { formatDateTime, formatDuration } from '@/lib/format'
import { useConnectivity, useSystem } from '@/lib/queries'
import type { Connectivity, ConnectivityOverall, ConnectivityProbeState } from '@/lib/schema'

type BadgeVariant = ComponentProps<typeof Badge>['variant']

/**
 * Word and colour of each probe state.
 *
 * The word carries the meaning; the badge only repeats it. `skipped` is not a
 * failure — it is a probe deliberately not attempted (no credentials) — so it
 * never takes the destructive colour.
 */
const PROBE_STATES: Record<ConnectivityProbeState, { label: TranslationKey; variant: BadgeVariant }> = {
  ok: { label: 'settings.connectivity.state.ok', variant: 'default' },
  skipped: { label: 'settings.connectivity.state.skipped', variant: 'outline' },
  unauthorized: { label: 'settings.connectivity.state.unauthorized', variant: 'destructive' },
  absent: { label: 'settings.connectivity.state.absent', variant: 'destructive' },
  timeout: { label: 'settings.connectivity.state.timeout', variant: 'destructive' },
  unreachable: { label: 'settings.connectivity.state.unreachable', variant: 'destructive' },
  error: { label: 'settings.connectivity.state.error', variant: 'destructive' },
}

/** Overall verdict: `partial` is reachable but not fully read, not a failure. */
const OVERALLS: Record<ConnectivityOverall, { label: TranslationKey; variant: BadgeVariant }> = {
  ok: { label: 'settings.connectivity.overall.ok', variant: 'default' },
  partial: { label: 'settings.connectivity.overall.partial', variant: 'secondary' },
  failed: { label: 'settings.connectivity.overall.failed', variant: 'destructive' },
}

/**
 * Result of one connectivity test: the overall verdict, then one row per probe.
 *
 * The table shows what the server measured and nothing else. A probe that was
 * not attempted says `skipped` with its reason; an absent HTTP status or reason
 * is a dash, never an invented value.
 */
function ConnectivityResult({ payload }: { payload: Connectivity }) {
  const { t } = useT()
  const overall = OVERALLS[payload.overall]

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm" data-testid="connectivity-overall">
        <span className="text-muted-foreground">{t('settings.connectivity.overall')}</span>
        <Badge variant={overall.variant}>{t(overall.label)}</Badge>
        <span className="text-muted-foreground">{t('settings.connectivity.testedAt')}</span>
        <time dateTime={payload.tested_at}>{formatDateTime(payload.tested_at)}</time>
        {payload.cached ? <span className="text-muted-foreground">{t('settings.connectivity.cached')}</span> : null}
      </div>

      <Table>
        <TableCaption className="sr-only">{t('settings.connectivity.caption')}</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead>{t('settings.connectivity.col.probe')}</TableHead>
            <TableHead>{t('settings.connectivity.col.state')}</TableHead>
            <TableHead>{t('settings.connectivity.col.http')}</TableHead>
            <TableHead>{t('settings.connectivity.col.duration')}</TableHead>
            <TableHead>{t('settings.connectivity.col.reason')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {payload.probes.map((probe) => {
            const state = PROBE_STATES[probe.state]
            return (
              <TableRow key={probe.name} data-testid="connectivity-probe">
                <TableCell className="align-top">
                  <span className="font-mono text-xs">{probe.name}</span>
                  <div className="font-mono text-xs text-muted-foreground break-all">{probe.path}</div>
                </TableCell>
                <TableCell className="align-top">
                  <Badge variant={state.variant}>{t(state.label)}</Badge>
                </TableCell>
                <TableCell className="align-top font-mono text-xs">{probe.http_status ?? '—'}</TableCell>
                <TableCell className="align-top font-mono text-xs">{formatDuration(probe.duration_ms)}</TableCell>
                <TableCell className="align-top text-xs text-muted-foreground">{probe.reason ?? probe.detail ?? '—'}</TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

/**
 * Settings page: effective configuration and a read-only connectivity test.
 *
 * No form is present as long as the API has no authentication (ADR 0005): an
 * editing interface without access control would be an open write path.
 *
 * The connectivity test is read-only too. It proves that the device answers
 * `GET` and that the configured credentials are accepted for the paths probed;
 * it proves nothing about write access, and a role is a display rule.
 */
export function SettingsPage() {
  const { t } = useT()
  const system = useSystem()
  const connectivity = useConnectivity()

  if (system.isLoading) {
    return <Skeleton className="h-64 w-full" />
  }

  if (system.isError || !system.data) {
    return (
      <ApiAlert
        title={t('state.error')}
        message={system.error instanceof Error ? system.error.message : t('state.error')}
        retryLabel={t('state.retry')}
        onRetry={() => void system.refetch()}
      />
    )
  }

  const config = system.data.data
  const fields: { key: string; label: string; value: string }[] = [
    { key: 'solarapi_url', label: t('settings.field.solarapiUrl'), value: config.solarapi_url },
    { key: 'api_addr', label: t('settings.field.apiAddr'), value: config.api_addr },
    { key: 'poll_interval_s', label: t('settings.field.pollInterval'), value: `${config.poll_interval_s} s` },
    { key: 'stale_after_s', label: t('settings.field.staleAfter'), value: `${config.stale_after_s} s` },
    { key: 'tz', label: t('settings.field.tz'), value: config.tz },
    { key: 'db_path', label: t('settings.field.dbPath'), value: config.db_path },
    {
      key: 'retention_days',
      label: t('settings.field.retention'),
      value: config.retention_days === 0 ? t('settings.retentionUnlimited') : String(config.retention_days),
    },
    { key: 'detect_interval_s', label: t('settings.field.detectionInterval'), value: `${config.detect_interval_s} s` },
    { key: 'http_timeout_s', label: t('settings.field.httpTimeout'), value: `${config.http_timeout_s} s` },
    {
      key: 'credentials_configured',
      label: t('settings.field.credentials'),
      value: config.credentials_configured
        ? t('settings.credentialsConfigured')
        : t('settings.credentialsNotConfigured'),
    },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-lg font-semibold">{t('settings.title')}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('system.config')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2">
            {fields.map((field) => (
              <div key={field.key}>
                <dt className="text-xs text-muted-foreground">{field.label}</dt>
                <dd className="font-mono text-sm break-all">{field.value}</dd>
              </div>
            ))}
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('settings.connectivity.title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="max-w-3xl text-sm text-muted-foreground">{t('settings.connectivity.intro')}</p>

          <Button onClick={() => void connectivity.refetch()} disabled={connectivity.isFetching}>
            {connectivity.isFetching ? t('settings.connectivity.running') : t('settings.connectivity.run')}
          </Button>

          {connectivity.isError ? (
            <ApiAlert
              title={t('state.error')}
              message={connectivity.error instanceof Error ? connectivity.error.message : t('state.error')}
              retryLabel={t('state.retry')}
              onRetry={() => void connectivity.refetch()}
            />
          ) : null}

          {connectivity.data ? (
            <ConnectivityResult payload={connectivity.data.data} />
          ) : !connectivity.isFetching && !connectivity.isError ? (
            <div className="text-sm text-muted-foreground" data-testid="connectivity-empty">
              <p>{t('settings.connectivity.empty')}</p>
              <p className="text-xs">{t('settings.connectivity.emptyHint')}</p>
            </div>
          ) : null}

          <p className="max-w-3xl text-xs text-muted-foreground">{t('settings.connectivity.caveat')}</p>
        </CardContent>
      </Card>

      <p className="max-w-3xl text-sm text-muted-foreground">{t('settings.readOnlyNote')}</p>
      <p className="text-xs text-muted-foreground">{t('settings.intro')}</p>
    </div>
  )
}
