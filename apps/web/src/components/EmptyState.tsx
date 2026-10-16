import { Inbox } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export type EmptyStateProps = {
  title: string
  hint: string
}

/**
 * Explicit empty state.
 *
 * A database without a measurement (new installation, collector never started)
 * is not an error: it is a state to name, to avoid a blank page being
 * interpreted as a failure.
 */
export function EmptyState({ title, hint }: EmptyStateProps) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2 text-muted-foreground">
          <Inbox aria-hidden="true" className="size-4" />
          <CardTitle>{title}</CardTitle>
        </div>
        <CardDescription>{hint}</CardDescription>
      </CardHeader>
      <CardContent />
    </Card>
  )
}
