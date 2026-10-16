import { ApiAlert } from '@/components/ApiAlert'
import { CapabilitiesTable } from '@/components/CapabilitiesTable'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n'
import { useCapabilities } from '@/lib/queries'

/** Capabilities page: source of truth for what is actually monitorable. */
export function CapabilitiesPage() {
  const { t } = useT()
  const capabilities = useCapabilities()

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold">{t('capabilities.title')}</h1>
      <p className="max-w-3xl text-sm text-muted-foreground">{t('capabilities.intro')}</p>

      {capabilities.isLoading ? <Skeleton className="h-64 w-full" /> : null}

      {capabilities.isError ? (
        <ApiAlert
          title={t('state.error')}
          message={capabilities.error instanceof Error ? capabilities.error.message : t('state.error')}
          retryLabel={t('state.retry')}
          onRetry={() => void capabilities.refetch()}
        />
      ) : null}

      {capabilities.data ? <CapabilitiesTable capabilities={capabilities.data.data} /> : null}
    </div>
  )
}
