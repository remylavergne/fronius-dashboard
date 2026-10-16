import { Inbox } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export type EmptyStateProps = {
  title: string
  hint: string
}

/**
 * État vide explicite.
 *
 * Une base sans mesure (installation neuve, collecteur jamais démarré) n'est pas
 * une erreur : c'est un état à nommer, pour éviter qu'une page blanche soit
 * interprétée comme une panne.
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
