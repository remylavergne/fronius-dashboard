import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { AlertTriangle } from 'lucide-react'

export type ApiAlertProps = {
  title: string
  message: string
  hint?: string | undefined
  /** Label of the retry button. */
  retryLabel: string
  onRetry: () => void
}

/**
 * Banner shown when the monitoring API is unreachable.
 *
 * The frontend only talks to `/api`: if this request fails with `network`, it
 * means `froniusd` is not answering — never that the inverter is absent.
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
