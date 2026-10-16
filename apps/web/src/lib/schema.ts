/**
 * Zod schemas mirroring the Go DTOs of `internal/api/dto.go`.
 *
 * Role: detect drift between the backend and the interface. If a field is
 * renamed or changes type on the Go side, the request fails here with an
 * `ApiError { kind: 'schema' }` — visible in the interface — rather than
 * producing a silently wrong display.
 *
 * Conventions taken from the backend:
 *  - snake_case keys;
 *  - base SI units: powers in W, energies in Wh, ratios in %;
 *  - an absent value arrives as `null`, never as 0;
 *  - zod objects ignore unknown keys: an enriched backend does not break the
 *    interface.
 *
 * To be maintained jointly with `internal/api/dto.go`.
 */
import { z } from 'zod'

/** UTC timestamp produced by `apiTimeLayout` (with or without milliseconds). */
const timestamp = z
  .string()
  .regex(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/, 'expected UTC timestamp')

/** Local date `YYYY-MM-DD` (production day). */
const localDate = z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'expected local date')

export const metaSchema = z.object({
  generated_at: timestamp,
  sampled_at: timestamp.optional(),
  stale: z.boolean().optional(),
  stale_after_s: z.number().optional(),
  /** Step actually applied by /timeseries, in seconds. */
  step_s: z.number().optional(),
})
export type Meta = z.infer<typeof metaSchema>

export const apiErrorSchema = z.object({
  code: z.string(),
  message: z.string(),
})
export type ApiErrorBody = z.infer<typeof apiErrorSchema>

/**
 * Success envelope: `{ data, meta }`, with `error` set for the single `no_data`
 * case of `/api/v1/live` (database still empty).
 */
export function envelopeSchema<T extends z.ZodTypeAny>(data: T) {
  return z.object({
    data,
    meta: metaSchema,
    error: apiErrorSchema.optional(),
  })
}

/** Error envelope: `{ error }`, without a `data` field. */
export const errorEnvelopeSchema = z.object({ error: apiErrorSchema })

// ---------------------------------------------------------------------------
// Health, system
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
    // Empty string as long as no successful cycle has occurred.
    last_success_utc: z.string(),
    last_error: z.string(),
    last_run_ms: z.number(),
    samples_total: z.number(),
  }),
})
export type Health = z.infer<typeof healthSchema>

/**
 * Device identity.
 *
 * The identity fields are nullable: a response that does not name the model
 * yields `null`, never `""` nor "unknown". `device_name` is the human name of
 * the unit (not its model, which stays `null` when the API exposes none) and
 * `article_number` is the product article number of that model. `read_at_utc`
 * is the timestamp of the read from which these values come.
 */
export const deviceSchema = z.object({
  model: z.string().nullable(),
  device_name: z.string().nullable(),
  article_number: z.string().nullable(),
  firmware: z.string().nullable(),
  serial_number: z.string().nullable(),
  source: z.string(),
  read_at_utc: timestamp,
})
export type Device = z.infer<typeof deviceSchema>

/**
 * State of a detection: `unknown`, `absent` or `present`.
 *
 * `unknown` is not `absent`: the first means that no conclusion has been drawn
 * (401, 404, network failure), the second that the device answered and does not
 * report the component.
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
// Measurements
// ---------------------------------------------------------------------------

const nullableNumber = z.number().nullable()

/**
 * Day and year energies derived from the total counter.
 *
 * The firmware of some inverters publishes no dedicated day/year counter
 * (`energy_day_wh` / `energy_year_wh` stay `null`); the backend then derives
 * those two quantities from `energy_total_wh`. `from` names the counter the
 * derivation starts from, each `*_baseline_utc` is the boundary the
 * subtraction is anchored to, and each `*_wh` is `null` when no sample close
 * enough to the boundary exists — the matching `*_reason` then says why.
 * Mirrors `derivedEnergyDTO` of `internal/api/dto.go`.
 */
export const derivedEnergySchema = z.object({
  from: z.string(),
  day_wh: nullableNumber,
  day_baseline_utc: z.string(),
  day_reason: z.string(),
  year_wh: nullableNumber,
  year_baseline_utc: z.string(),
  year_reason: z.string(),
})
export type DerivedEnergy = z.infer<typeof derivedEnergySchema>

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
  /**
   * Present on `/api/v1/live` only; absent on the endpoints that do not derive
   * it. Kept optional so a payload without it never fails the schema.
   */
  energy_derived: derivedEnergySchema.nullable().optional(),
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
// Source catalogue (/api/v1/sources)
// ---------------------------------------------------------------------------

/**
 * One value of a source, named by the path it has in the answer.
 *
 * `type` is the JSON type observed in the capture (`float`, `string`, object…),
 * `status` what this project does with the value: `displayed` (read into the
 * model and shown), `stored` (read and persisted), `validated` (read to accept
 * or reject the payload, never shown), `ignored` (present in the answer and
 * deliberately unused) or `unverified` (never observed carrying a value on the
 * reference installation). Those five words are a closed vocabulary of the
 * backend (`internal/sources`), but they are read as plain strings: an unknown
 * status is rendered as it arrives rather than failing the whole page.
 *
 * `why` explains an ignored or an unverified field — required for those two by
 * the backend — and `capability` names the capability the field feeds.
 */
export const sourceFieldSchema = z.object({
  path: z.string(),
  type: z.string(),
  status: z.string(),
  why: z.string().optional(),
  capability: z.string().optional(),
})
export type SourceField = z.infer<typeof sourceFieldSchema>

/**
 * Provenance of a claim about a source: the register's own marker (`[VERIFIED]`,
 * `[SOURCE-JS]`, `[DOC]`…), the firmware that produced the evidence (empty when
 * the proof did not come from the device) and the detail.
 */
export const sourceProofSchema = z.object({
  tag: z.string(),
  firmware: z.string().optional(),
  detail: z.string(),
})
export type SourceProof = z.infer<typeof sourceProofSchema>

/**
 * One endpoint, or one query variant of an endpoint.
 *
 * `transport` is `solar_api` (documented Solar API v1) or `internal_api` (the
 * manufacturer's web interface, Digest-protected and reachable only through the
 * code allowlist); `state` is one of `called`, `captured`, `candidate`,
 * `archived`; `called` says whether this application reads it today (a probe
 * run by the user is not a call).
 *
 * `capability` names a capability that exists in `/api/v1/capabilities` today,
 * `would_unlock` a capability that does **not** exist yet — a candidate id, to
 * be rendered as such and never as a measured capability.
 *
 * The optional keys mirror the Go tags (`omitempty`), and every collection is
 * `nullish` as well: `encoding/json` marshals a nil slice as `null`, so an
 * absent key and a `null` mean the same absence here, while `[]` and a
 * populated array are both accepted. The identity keys (`id`, `path`, `method`,
 * `state`) stay required: without them a source can neither be rendered nor
 * filtered.
 */
export const sourceSchema = z.object({
  id: z.string(),
  transport: z.string(),
  path: z.string(),
  query: z.string().optional(),
  method: z.string(),
  provides: z.string(),
  state: z.string(),
  called: z.boolean(),
  fixture: z.string().optional(),
  capability: z.string().optional(),
  would_unlock: z.string().optional(),
  fields: z.array(sourceFieldSchema).nullish(),
  proofs: z.array(sourceProofSchema).nullish(),
  docs: z.array(z.string()).nullish(),
  note: z.string().optional(),
})
export type Source = z.infer<typeof sourceSchema>

/**
 * Counters computed by the backend over exactly the list it returns, so the
 * interface never recounts: a divergence between the two would be a bug on one
 * side, not something the page papers over.
 */
export const sourceSummarySchema = z.object({
  sources: z.number(),
  called: z.number(),
  captured: z.number(),
  candidates: z.number(),
  archived: z.number(),
  fields: z.number(),
  fields_displayed: z.number(),
  fields_stored: z.number(),
  fields_validated: z.number(),
  fields_ignored: z.number(),
  fields_unverified: z.number(),
})
export type SourceSummary = z.infer<typeof sourceSummarySchema>

/**
 * Catalogue payload: what the device answers, and what this project does with
 * each field of each answer.
 *
 * Static data (it changes only with a deployment), so the answer carries no
 * measurement, only the envelope's `generated_at`. The runtime half — is the
 * device answering, which capability is available and why — stays in
 * `/api/v1/system`, `/api/v1/health` and `/api/v1/capabilities`: the interface
 * joins them, the API does not duplicate them.
 */
export const sourcesSchema = z.object({
  summary: sourceSummarySchema,
  sources: z.array(sourceSchema),
})
export type SourceCatalogue = z.infer<typeof sourcesSchema>

// ---------------------------------------------------------------------------
// Envelopes per endpoint
// ---------------------------------------------------------------------------

export const healthEnvelopeSchema = envelopeSchema(healthSchema)
export const systemEnvelopeSchema = envelopeSchema(systemSchema)
/** `data` is `null` when no measurement exists yet. */
export const liveEnvelopeSchema = envelopeSchema(sampleSchema.nullable())
export const timeseriesEnvelopeSchema = envelopeSchema(z.array(bucketSchema))
export const energyDailyEnvelopeSchema = envelopeSchema(z.array(dailyEnergySchema))
export const eventsEnvelopeSchema = envelopeSchema(z.array(eventSchema))
export const capabilitiesEnvelopeSchema = envelopeSchema(z.array(capabilitySchema))
export const sourcesEnvelopeSchema = envelopeSchema(sourcesSchema)

export type HealthEnvelope = z.infer<typeof healthEnvelopeSchema>
export type SystemEnvelope = z.infer<typeof systemEnvelopeSchema>
export type LiveEnvelope = z.infer<typeof liveEnvelopeSchema>
export type TimeseriesEnvelope = z.infer<typeof timeseriesEnvelopeSchema>
export type EnergyDailyEnvelope = z.infer<typeof energyDailyEnvelopeSchema>
export type EventsEnvelope = z.infer<typeof eventsEnvelopeSchema>
export type CapabilitiesEnvelope = z.infer<typeof capabilitiesEnvelopeSchema>
export type SourcesEnvelope = z.infer<typeof sourcesEnvelopeSchema>

/** Error code signalling a still-empty database (`/api/v1/live`). */
export const CODE_NO_DATA = 'no_data'
