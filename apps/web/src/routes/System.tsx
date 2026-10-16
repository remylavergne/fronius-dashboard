import { ApiAlert } from '@/components/ApiAlert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n'
import { formatAge, formatDuration } from '@/lib/format'
import { useHealth, useSystem } from '@/lib/queries'

/**
 * État de la supervision : collecteur, base, identité de l'onduleur.
 *
 * La section « onduleur » reste vide en phase 1 : aucun endpoint confirmé ne
 * fournit modèle, firmware ou numéro de série. C'est dit explicitement plutôt
 * que comblé par une valeur déduite.
 */
export function SystemPage() {
  const { t } = useT()
  const health = useHealth()
  const system = useSystem()

  if (health.isLoading && system.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  if (health.isError || system.isError) {
    const error = health.error ?? system.error
    return (
      <ApiAlert
        title={t('state.error')}
        message={error instanceof Error ? error.message : t('state.error')}
        retryLabel={t('state.retry')}
        onRetry={() => {
          void health.refetch()
          void system.refetch()
        }}
      />
    )
  }

  const healthData = health.data?.data
  const systemData = system.data?.data
  if (!healthData || !systemData) {
    return null
  }

  return (
    <div className="space-y-6">
      <h1 className="text-lg font-semibold">{t('system.title')}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('system.status')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="flex flex-wrap items-center gap-3">
            <Badge variant={healthData.status === 'ok' ? 'secondary' : 'destructive'}>
              {t(`health.${healthData.status}` as 'health.ok')}
            </Badge>
            <span className="text-muted-foreground">
              {t('health.db')} : {healthData.db.ok ? 'OK' : t('health.error')}
            </span>
            {healthData.db.error ? <span className="text-destructive">{healthData.db.error}</span> : null}
          </div>

          <Separator />

          <dl className="grid gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">{t('kpi.lastSuccess')}</dt>
              <dd>
                {formatAge(healthData.collector.last_success_utc)} ({healthData.collector.last_success_utc || '—'})
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('kpi.runDuration')}</dt>
              <dd>{formatDuration(healthData.collector.last_run_ms)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('kpi.samples')}</dt>
              <dd className="tabular-nums">{healthData.collector.samples_total}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('health.lastError')}</dt>
              <dd className={healthData.collector.last_error ? 'text-destructive' : ''}>
                {healthData.collector.last_error || '—'}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('system.device')}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          {systemData.device ? (
            <dl className="grid gap-3 sm:grid-cols-2">
              <div>
                <dt className="text-xs">{t('system.device')}</dt>
                <dd className="text-foreground">{systemData.device.model}</dd>
              </div>
              <div>
                <dt className="text-xs">Firmware</dt>
                <dd className="text-foreground">{systemData.device.firmware}</dd>
              </div>
            </dl>
          ) : (
            <p>{t('system.deviceUnknown')}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('system.application')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">{t('settings.field.version')}</dt>
              <dd className="font-mono">{systemData.version}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('settings.field.goVersion')}</dt>
              <dd className="font-mono">{systemData.go_version}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('kpi.uptime')}</dt>
              <dd>{healthData.uptime_s} s</dd>
            </div>
          </dl>
        </CardContent>
      </Card>
    </div>
  )
}
