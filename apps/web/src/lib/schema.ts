/**
 * Schémas zod miroirs des DTO Go de `internal/api/dto.go`.
 *
 * Rôle : détecter la dérive entre le backend et l'interface. Si un champ est
 * renommé ou change de type côté Go, la requête échoue ici avec un
 * `ApiError { kind: 'schema' }` — visible dans l'interface — plutôt que de
 * produire un affichage silencieusement faux.
 *
 * Conventions reprises du backend :
 *  - clés en snake_case ;
 *  - unités SI de base : puissances en W, énergies en Wh, ratios en % ;
 *  - une valeur absente arrive en `null`, jamais en 0 ;
 *  - les objets zod ignorent les clés inconnues : un backend enrichi ne casse
 *    pas l'interface.
 *
 * À maintenir conjointement avec `internal/api/dto.go`.
 */
import { z } from 'zod'

/** Horodatage UTC produit par `apiTimeLayout` (avec ou sans milliseconde). */
const timestamp = z
  .string()
  .regex(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/, 'horodatage UTC attendu')

/** Date locale `YYYY-MM-DD` (journée de production). */
const localDate = z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'date locale attendue')

export const metaSchema = z.object({
  generated_at: timestamp,
  sampled_at: timestamp.optional(),
  stale: z.boolean().optional(),
  stale_after_s: z.number().optional(),
  /** Pas réellement appliqué par /timeseries, en secondes. */
  step_s: z.number().optional(),
})
export type Meta = z.infer<typeof metaSchema>

export const apiErrorSchema = z.object({
  code: z.string(),
  message: z.string(),
})
export type ApiErrorBody = z.infer<typeof apiErrorSchema>

/**
 * Enveloppe de succès : `{ data, meta }`, avec `error` renseigné pour le seul
 * cas `no_data` de `/api/v1/live` (base encore vide).
 */
export function envelopeSchema<T extends z.ZodTypeAny>(data: T) {
  return z.object({
    data,
    meta: metaSchema,
    error: apiErrorSchema.optional(),
  })
}

/** Enveloppe d'erreur : `{ error }`, sans champ `data`. */
export const errorEnvelopeSchema = z.object({ error: apiErrorSchema })

// ---------------------------------------------------------------------------
// Santé, système
// ---------------------------------------------------------------------------

export const healthSchema = z.object({
  status: z.string(),
  version: z.string(),
  uptime_s: z.number(),
  db: z.object({
    ok: z.boolean(),
    error: z.string().optional(),
  }),
  collector: z.object({
    // Chaîne vide tant qu'aucun cycle réussi n'a eu lieu.
    last_success_utc: z.string(),
    last_error: z.string(),
    last_run_ms: z.number(),
    samples_total: z.number(),
  }),
})
export type Health = z.infer<typeof healthSchema>

/**
 * Identité de l'appareil.
 *
 * Les trois champs d'identité sont nullables : une réponse qui ne nomme pas le
 * modèle produit `null`, jamais `""` ni « inconnu ». `read_at_utc` est
 * l'horodatage de la lecture dont ces valeurs proviennent.
 */
export const deviceSchema = z.object({
  model: z.string().nullable(),
  firmware: z.string().nullable(),
  serial_number: z.string().nullable(),
  source: z.string(),
  read_at_utc: timestamp,
})
export type Device = z.infer<typeof deviceSchema>

/**
 * État d'une détection : `unknown`, `absent` ou `present`.
 *
 * `unknown` n'est pas `absent` : le premier signifie qu'aucune conclusion n'a
 * été tirée (401, 404, panne réseau), le second que l'appareil a répondu et ne
 * signale pas le composant.
 */
export const detectionResourceSchema = z.object({
  state: z.string(),
  source: z.string(),
  reason: z.string(),
})
export type DetectionResource = z.infer<typeof detectionResourceSchema>

export const detectionSchema = z.object({
  read_at_utc: timestamp.nullable(),
  meter: detectionResourceSchema,
  battery: detectionResourceSchema,
  internal_api: detectionResourceSchema,
  device_events: detectionResourceSchema,
  last_error: z.string(),
})
export type Detection = z.infer<typeof detectionSchema>

export const systemSchema = z.object({
  version: z.string(),
  go_version: z.string(),
  solarapi_url: z.string(),
  poll_interval_s: z.number(),
  tz: z.string(),
  db_path: z.string(),
  retention_days: z.number(),
  api_addr: z.string(),
  stale_after_s: z.number(),
  device: deviceSchema.nullable(),
  detection: detectionSchema.nullable(),
})
export type System = z.infer<typeof systemSchema>

// ---------------------------------------------------------------------------
// Mesures
// ---------------------------------------------------------------------------

const nullableNumber = z.number().nullable()

export const inverterSchema = z.object({
  id: z.string(),
  dt: z.number(),
  power_w: nullableNumber,
  energy_day_wh: nullableNumber,
})
export type Inverter = z.infer<typeof inverterSchema>

export const sampleSchema = z.object({
  ts: timestamp,
  ts_source: z.string(),
  source: z.string(),
  quality: z.string(),
  mode: z.string(),
  power_pv_w: nullableNumber,
  energy_day_wh: nullableNumber,
  energy_year_wh: nullableNumber,
  energy_total_wh: nullableNumber,
  power_grid_w: nullableNumber,
  power_load_w: nullableNumber,
  power_battery_w: nullableNumber,
  rel_autonomy_pct: nullableNumber,
  rel_self_consumption_pct: nullableNumber,
  inverters: z.array(inverterSchema),
})
export type Sample = z.infer<typeof sampleSchema>

export const bucketSchema = z.object({
  start: timestamp,
  count: z.number(),
  missing: z.number(),
  power_pv_avg_w: nullableNumber,
  power_pv_min_w: nullableNumber,
  power_pv_max_w: nullableNumber,
})
export type Bucket = z.infer<typeof bucketSchema>

export const dailyEnergySchema = z.object({
  date: localDate,
  energy_wh: nullableNumber,
})
export type DailyEnergy = z.infer<typeof dailyEnergySchema>

export const eventSchema = z.object({
  ts: timestamp,
  kind: z.string(),
  severity: z.string(),
  message: z.string(),
  detail: z.string(),
})
export type Event = z.infer<typeof eventSchema>

export const capabilitySchema = z.object({
  id: z.string(),
  label: z.string(),
  available: z.boolean(),
  quality: z.string(),
  requires: z.array(z.string()),
  read_only: z.boolean(),
  verified: z.boolean(),
  evidence: z.array(z.string()),
  reason: z.string(),
})
export type Capability = z.infer<typeof capabilitySchema>

// ---------------------------------------------------------------------------
// Enveloppes par endpoint
// ---------------------------------------------------------------------------

export const healthEnvelopeSchema = envelopeSchema(healthSchema)
export const systemEnvelopeSchema = envelopeSchema(systemSchema)
/** `data` vaut `null` quand aucune mesure n'existe encore. */
export const liveEnvelopeSchema = envelopeSchema(sampleSchema.nullable())
export const timeseriesEnvelopeSchema = envelopeSchema(z.array(bucketSchema))
export const energyDailyEnvelopeSchema = envelopeSchema(z.array(dailyEnergySchema))
export const eventsEnvelopeSchema = envelopeSchema(z.array(eventSchema))
export const capabilitiesEnvelopeSchema = envelopeSchema(z.array(capabilitySchema))

export type HealthEnvelope = z.infer<typeof healthEnvelopeSchema>
export type SystemEnvelope = z.infer<typeof systemEnvelopeSchema>
export type LiveEnvelope = z.infer<typeof liveEnvelopeSchema>
export type TimeseriesEnvelope = z.infer<typeof timeseriesEnvelopeSchema>
export type EnergyDailyEnvelope = z.infer<typeof energyDailyEnvelopeSchema>
export type EventsEnvelope = z.infer<typeof eventsEnvelopeSchema>
export type CapabilitiesEnvelope = z.infer<typeof capabilitiesEnvelopeSchema>

/** Code d'erreur signalant une base encore vide (`/api/v1/live`). */
export const CODE_NO_DATA = 'no_data'
