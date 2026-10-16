import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { AlertTriangle } from 'lucide-react'

export type ApiAlertProps = {
  title: string
  message: string
  hint?: string | undefined
  /** Libellé du bouton de nouvelle tentative. */
  retryLabel: string
  onRetry: () => void
}

/**
 * Bandeau affiché lorsque l'API de supervision est injoignable.
 *
 * Le frontend ne parle qu'à `/api` : si cette requête échoue en `network`, c'est
 * que `froniusd` ne répond pas — jamais que l'onduleur est absent.
 */
export function ApiAlert({ title, message, hint, retryLabel, onRetry }: ApiAlertProps) {
  return (
    <Alert variant="destructive" data-testid="api-error">
      <AlertTriangle aria-hidden="true" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <p>{message}</p>
        {hint ? <p className="text-muted-foreground">{hint}</p> : null}
      </AlertDescription>
      <AlertAction>
        <Button variant="outline" size="sm" onClick={onRetry}>
          {retryLabel}
        </Button>
      </AlertAction>
    </Alert>
  )
}
