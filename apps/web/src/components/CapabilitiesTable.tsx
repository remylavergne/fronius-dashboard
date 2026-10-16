import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import type { Capability } from '@/lib/schema'

const QUALITY_KEYS: Record<string, TranslationKey> = {
  measured: 'quality.measured',
  estimated: 'quality.estimated',
  missing: 'quality.missing',
}

export type CapabilitiesTableProps = {
  capabilities: Capability[]
}

/**
 * Tableau des capacités de la supervision.
 *
 * Une capacité indisponible est rendue comme telle, avec son motif : aucune
 * carte « consommation », « import/export » ou « batterie » n'est codée en dur
 * ailleurs dans l'interface, et aucune valeur numérique n'est affichée pour une
 * capacité indisponible (l'invariant « pas de zéro inventé » va jusqu'à
 * l'écran).
 */
export function CapabilitiesTable({ capabilities }: CapabilitiesTableProps) {
  const { t } = useT()

  // Tri stable par identifiant : l'ordre ne dépend pas de l'API.
  const rows = [...capabilities].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))

  return (
    <div className="overflow-x-auto">
      <Table>
        <TableCaption className="sr-only">{t('capabilities.intro')}</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">{t('capabilities.col.metric')}</TableHead>
            <TableHead scope="col">{t('capabilities.col.status')}</TableHead>
            <TableHead scope="col">{t('capabilities.col.quality')}</TableHead>
            <TableHead scope="col">{t('capabilities.col.requires')}</TableHead>
            <TableHead scope="col">{t('capabilities.col.evidence')}</TableHead>
            <TableHead scope="col">{t('capabilities.col.reason')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((capability) => {
            const qualityKey = QUALITY_KEYS[capability.quality]
            return (
              <TableRow key={capability.id} data-available={capability.available ? 'true' : 'false'}>
                <TableCell>
                  <div className="font-medium">{capability.label}</div>
                  <div className="font-mono text-xs text-muted-foreground">{capability.id}</div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={capability.available ? 'secondary' : 'outline'}>
                      {capability.available ? t('capabilities.available') : t('capabilities.unavailable')}
                    </Badge>
                    <span className="text-xs text-muted-foreground">
                      {capability.verified ? t('capabilities.verified') : t('capabilities.unverified')}
                    </span>
                    {capability.read_only ? (
                      <span className="text-xs text-muted-foreground">{t('capabilities.readOnly')}</span>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell>{qualityKey ? t(qualityKey) : capability.quality}</TableCell>
                <TableCell>{capability.requires.length > 0 ? capability.requires.join(', ') : '—'}</TableCell>
                <TableCell>{capability.evidence.length > 0 ? capability.evidence.join(', ') : '—'}</TableCell>
                <TableCell className="text-muted-foreground">{capability.reason === '' ? '—' : capability.reason}</TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
