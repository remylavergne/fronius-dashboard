import { ApiAlert } from '@/components/ApiAlert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n'
import { useSystem } from '@/lib/queries'

/**
 * Page Paramètres : configuration effective, en **lecture seule**.
 *
 * Aucun formulaire n'est présent tant que l'API n'a pas d'authentification
 * (ADR 0005) : une interface d'édition sans contrôle d'accès serait un chemin
 * d'écriture ouvert.
 */
export function SettingsPage() {
  const { t } = useT()
  const system = useSystem()

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

      <p className="max-w-3xl text-sm text-muted-foreground">{t('settings.readOnlyNote')}</p>
      <p className="text-xs text-muted-foreground">{t('settings.intro')}</p>
    </div>
  )
}
