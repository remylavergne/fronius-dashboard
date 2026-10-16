import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useT } from '@/i18n'
import type { TranslationKey } from '@/i18n'
import { formatAge, formatDateTime } from '@/lib/format'
import type { ClockResource, Detection, Device, DetectionResource, ServiceResource } from '@/lib/schema'

/** Labels of the three detection states. */
const STATE_KEYS: Record<string, TranslationKey> = {
  present: 'state.present',
  absent: 'state.absent',
  unknown: 'state.unknown',
}

/**
 * A drift beyond this many seconds already distorts what "today" means: the
 * device's `E_Day` counter resets at its own midnight, and the daily derivation
 * tolerates a baseline reading only within ten minutes.
 */
const DRIFT_WARNING_S = 60

/** Displayed resource: a detection, plus the reading that may accompany it. */
type ResourceSpec = {
  key: string
  label: TranslationKey
  pick: (d: Detection) => DetectionResource
  /** The reading beside the state — its text, and whether it warns — or `null`. */
  reading?: (
    resource: DetectionResource,
    t: (key: TranslationKey) => string,
  ) => { text: string; warn: boolean } | null
}

/** Displayed resources, in a stable order. */
const RESOURCES: ResourceSpec[] = [
  { key: 'meter', label: 'system.resource.meter', pick: (d) => d.meter },
  { key: 'battery', label: 'system.resource.battery', pick: (d) => d.battery },
  { key: 'internal_api', label: 'system.resource.internalApi', pick: (d) => d.internal_api },
  { key: 'device_events', label: 'system.resource.deviceEvents', pick: (d) => d.device_events },
  {
    key: 'clock',
    label: 'system.resource.clock',
    pick: (d) => d.clock,
    // Absent fields mean the clock was not read: render no reading at all, never
    // a zero drift nor a 1970 timestamp.
    reading: (resource, t) => {
      const clock = resource as ClockResource
      const { device_utc: deviceUTC, drift_s: driftS } = clock
      if (deviceUTC === undefined || driftS === undefined) {
        return null
      }
      const rounded = Math.round(driftS * 10) / 10
      const warn = Math.abs(driftS) > DRIFT_WARNING_S
      const over = warn ? ` (${t('system.clockDriftOver')})` : ''
      const text = `${t('system.clockDeviceTime')} ${formatDateTime(deviceUTC)} · ${t('system.clockDrift')} ${rounded > 0 ? '+' : ''}${rounded} s${over}`
      return { text, warn }
    },
  },
  {
    key: 'service',
    label: 'system.resource.service',
    pick: (d) => d.service,
    reading: (resource, t) => {
      const value = (resource as ServiceResource).value
      return value === undefined ? null : { text: `${t('system.serviceValue')} ${value}`, warn: false }
    },
  },
]

export type DevicePanelProps = {
  device: Device | null
  detection: Detection | null
}

/**
 * Inverter identity and hardware detection status.
 *
 * Invariants taken from the model:
 *  - an absent field (`null`) is rendered "—", never "0" nor "unknown";
 *  - "unknown" and "absent" are two distinct labels: the first says that no
 *    conclusion has been drawn, the second that the device answered and does
 *    not report the component;
 *  - a detection that is not "present" always carries its reason, displayed
 *    as-is.
 */
export function DevicePanel({ device, detection }: DevicePanelProps) {
  const { t } = useT()

  const identityField = (label: string, value: string | null) => (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-foreground">{value ?? '—'}</dd>
    </div>
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{t('system.device')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <div data-testid="device-identity">
          <div className="mb-2 text-xs font-medium text-muted-foreground">{t('system.identity')}</div>
          {device ? (
            <>
              <dl className="grid gap-3 sm:grid-cols-3">
                {identityField(t('system.model'), device.model)}
                {identityField(t('system.deviceName'), device.device_name)}
                {identityField(t('system.firmware'), device.firmware)}
                {identityField(t('system.serialNumber'), device.serial_number)}
                {identityField(t('system.articleNumber'), device.article_number)}
              </dl>
              <dl className="mt-3 grid gap-3 sm:grid-cols-2">
                {identityField(t('system.source'), device.source)}
                <div>
                  <dt className="text-xs text-muted-foreground">{t('system.readAt')}</dt>
                  <dd className="text-foreground" data-testid="device-read-at">
                    {formatAge(device.read_at_utc)} ({formatDateTime(device.read_at_utc)})
                  </dd>
                </div>
              </dl>
            </>
          ) : (
            <>
              <dl className="grid gap-3 sm:grid-cols-3">
                {identityField(t('system.model'), null)}
                {identityField(t('system.deviceName'), null)}
                {identityField(t('system.firmware'), null)}
                {identityField(t('system.serialNumber'), null)}
                {identityField(t('system.articleNumber'), null)}
              </dl>
              <p className="mt-3 text-muted-foreground">{t('system.noIdentity')}</p>
            </>
          )}
        </div>

        <Separator />

        <div data-testid="device-detection">
          <div className="mb-2 text-xs font-medium text-muted-foreground">{t('system.detection')}</div>
          <dl className="grid gap-3">
            {RESOURCES.map(({ key, label, pick, reading }) => {
              const resource = detection ? pick(detection) : null
              const stateKey = resource ? STATE_KEYS[resource.state] : undefined
              const stateLabel = stateKey ? t(stateKey) : (resource?.state ?? t('state.unknown'))
              const detail = resource && reading ? reading(resource, t) : null
              return (
                <div key={key} className="flex flex-wrap items-baseline gap-x-3" data-state={resource?.state ?? 'unknown'}>
                  <dt className="min-w-32 text-xs text-muted-foreground">{t(label)}</dt>
                  <dd>
                    {detail?.warn ? (
                      <Badge variant="destructive" className="mr-2">
                        {t('system.clockDriftWarning')}
                      </Badge>
                    ) : null}
                    <span className="font-medium">{stateLabel}</span>
                    {resource?.reason ? (
                      <span className="ml-2 text-muted-foreground">{resource.reason}</span>
                    ) : null}
                    {detail ? <span className="ml-2 text-muted-foreground">{detail.text}</span> : null}
                  </dd>
                </div>
              )
            })}
          </dl>
          {detection?.last_error ? (
            <p className="mt-3 text-destructive" data-testid="detection-last-error">
              {t('system.lastError')}: {detection.last_error}
            </p>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}
