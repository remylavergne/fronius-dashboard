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
 * The generated contract (`./api-types.ts`, from `api/openapi.json`) is the
 * machine-readable form of `internal/api/dto.go`; the links at the bottom of
 * this file make a drift between the two a `tsc` error. Regenerate the types
 * with `make api-types` after an API change, never by editing them.
 */
import { z } from 'zod'
import type { components, operations } from './api-types'

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

const dbHealthSchema = z.object({
  ok: z.boolean(),
  error: z.string().optional(),
})

const collectorHealthSchema = z.object({
  // Empty string as long as no successful cycle has occurred.
  last_success_utc: z.string(),
  last_error: z.string(),
  last_run_ms: z.number(),
  samples_total: z.number(),
})

export const healthSchema = z.object({
  status: z.string(),
  version: z.string(),
  uptime_s: z.number(),
  db: dbHealthSchema,
  collector: collectorHealthSchema,
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

/**
 * Reading of the device's own clock (`/commands/GetUTCTime`). `drift_s` is the
 * device's clock minus the host's, in seconds.
 *
 * `device_utc` and `drift_s` are absent — never `0` nor a 1970 timestamp —
 * when the clock could not be read: an unread clock has no drift.
 */
export const clockResourceSchema = detectionResourceSchema.extend({
  device_utc: timestamp.optional(),
  drift_s: z.number().optional(),
})
export type ClockResource = z.infer<typeof clockResourceSchema>

/**
 * Reading of one system service (`/commands/GetServiceState`); `value` is the
 * state string the device reported, absent when nothing was read.
 */
export const serviceResourceSchema = detectionResourceSchema.extend({
  value: z.string().optional(),
})
export type ServiceResource = z.infer<typeof serviceResourceSchema>

export const detectionSchema = z.object({
  read_at_utc: timestamp.nullable(),
  meter: detectionResourceSchema,
  battery: detectionResourceSchema,
  internal_api: detectionResourceSchema,
  device_events: detectionResourceSchema,
  clock: clockResourceSchema,
  service: serviceResourceSchema,
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
  detect_interval_s: z.number(),
  http_timeout_s: z.number(),
  /** Whether device credentials are set — never the values themselves. */
  credentials_configured: z.boolean(),
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
  since_wh: nullableNumber,
  since_baseline_utc: z.string(),
  since_reason: z.string(),
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

/**
 * One local day of `/api/v1/energy/daily`.
 *
 * `energy_wh` is the device's **own** counter and it is `null` whenever the
 * firmware publishes no day counter at all (the reference installation: it
 * never reports `E_Day`); `derived_wh` is measured from our own stored total
 * and is then the value that exists. A day may therefore carry either, both or
 * neither — and a day that carries neither says why in `derived_reason`.
 *
 * `derived_reason` and `partial` are `omitempty` on the Go side: absent is the
 * normal encoding of "no reason to give" and "the day is over". They are
 * accepted as absent so a payload from a backend that omits them never fails
 * the whole page.
 */
export const dailyEnergySchema = z.object({
  /** Local day (`YYYY-MM-DD`) of the **installation**, not of the browser. */
  date: localDate,
  energy_wh: nullableNumber,
  derived_wh: nullableNumber.optional(),
  derived_reason: z.string().optional(),
  /** Day in progress: its value grows until the day ends. */
  partial: z.boolean().optional(),
})
export type DailyEnergy = z.infer<typeof dailyEnergySchema>

/**
 * One event of `/api/v1/events`.
 *
 * `severity` is a closed vocabulary server-side (`info`, `warning`, `error`) but
 * it is read as a plain string: a severity this build does not know yet renders
 * as its raw token rather than taking the page down. `kind` is an open machine
 * word (`collector_error`, `device_event`, …); `detail` is a free-text payload,
 * frequently raw JSON, bounded to 2 KB by the backend — it is text, never HTML.
 */
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
 * One value the last read-only survey observed in an answer: its JSON path, its
 * type, and — when the value was an array — how many items it carried.
 *
 * `array_len` is absent when the Go field is zero (`omitempty`): it is the count
 * of a non-empty array, never a claim about an empty one. No value of the answer
 * is ever carried here: the survey records the shape, never the values.
 */
export const probeFieldSchema = z.object({
  path: z.string(),
  type: z.string(),
  array_len: z.number().optional(),
})
export type ProbeField = z.infer<typeof probeFieldSchema>

/**
 * What the last read-only survey observed on one path.
 *
 * `class` is the outcome — `ok`, `empty`, `not_json`, `auth_required`,
 * `not_found`, `error` — and, like the field statuses of the catalogue, it is
 * read as a plain string: a class this build does not know yet must render as
 * its raw token rather than take the whole catalogue down.
 *
 * `digest_required` says the path answered 401 without a `Digest` header, which
 * is how the interface can say "reachable, authentication needed" without ever
 * holding a credential. `error` is set for the `error` class only, `firmware`
 * is absent when unknown, and `fields` is absent when the answer carried no
 * shape (`empty`, `not_json`, an error) — an absent `fields` and an empty one
 * mean the same absence here.
 */
export const sourceProbeSchema = z.object({
  class: z.string(),
  http_status: z.number(),
  digest_required: z.boolean(),
  bytes: z.number(),
  error: z.string().optional(),
  firmware: z.string().optional(),
  probed_at: timestamp,
  fields: z.array(probeFieldSchema).nullish(),
})
export type SourceProbe = z.infer<typeof sourceProbeSchema>

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
  /**
   * Absent when this path was never surveyed on this firmware. That absence is
   * a fact of its own — "never observed" is not `not_found`, and neither is
   * `empty`: the first says nobody looked, the second that the firmware does not
   * serve the path, the third that it served it and the component is not there.
   */
  probe: sourceProbeSchema.optional(),
})
export type Source = z.infer<typeof sourceSchema>

/**
 * Counters of the last recorded survey.
 *
 * All zero and no `last_probed_at` until a survey has been run: `last_probed_at`
 * is the marker of a survey having happened at all, so the interface can say
 * "never surveyed" instead of showing zeros as if they were results.
 *
 * `firmware` is absent when the survey met no device (every path answered an
 * error), and `last_probed_at` is the *latest* instant of the survey: the client
 * stamps each attempt, so the survey is as fresh as its last path.
 */
export const sourceSurveySchema = z.object({
  probed: z.number(),
  ok: z.number(),
  empty: z.number(),
  not_json: z.number(),
  auth_required: z.number(),
  not_found: z.number(),
  error: z.number(),
  last_probed_at: timestamp.optional(),
  firmware: z.string().optional(),
})
export type SourceSurvey = z.infer<typeof sourceSurveySchema>

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
 * Catalogue payload: what the device answers, what this project does with each
 * field of each answer, and what the last read-only survey observed on each
 * path.
 *
 * The survey is what tells `available and unmapped` (the device serves the path,
 * nothing here reads it) from `documented but never observed` (no probe at all).
 *
 * Static data (it changes only with a deployment), so the answer carries no
 * measurement, only the envelope's `generated_at`. The runtime half — is the
 * device answering, which capability is available and why — stays in
 * `/api/v1/system`, `/api/v1/health` and `/api/v1/capabilities`: the interface
 * joins them, the API does not duplicate them.
 */
export const sourcesSchema = z.object({
  summary: sourceSummarySchema,
  /**
   * Optional: a backend that predates the survey simply does not send it, and
   * the page then says "never surveyed" — which is exactly what its absence
   * means on a current one.
   */
  survey: sourceSurveySchema.optional(),
  sources: z.array(sourceSchema),
})
export type SourceCatalogue = z.infer<typeof sourcesSchema>

// ---------------------------------------------------------------------------
// Device detail (/api/v1/detail)
// ---------------------------------------------------------------------------

/**
 * One family of the device detail: what the collector reads from the device's
 * own interface, on a slow cycle.
 *
 * A family may be **absent** from the payload, which means the collector has
 * never read it; `available` may be `false` with a `reason` — a sentence of the
 * backend. Those two states are not the same, and neither is a zero.
 *
 * `data` is `unknown`: each family has a shape of its own, described below and
 * validated where it is typed. Inside a family's `data`, only the field that
 * identifies a row is required (`key` of a measurement, `name` of an interface,
 * `type` of a device, `path` of an entry): every other field is optional,
 * because the answer varies with the firmware. An absent value renders as a
 * dash — while a `0` the device really reported stays a `0`.
 */
export const detailFamilySchema = z.object({
  family: z.string(),
  available: z.boolean(),
  /**
   * Sentence of the backend explaining an unavailable family. Accepted absent
   * or `null` as well as empty: the Go DTO omits it when it is empty, and the
   * three spellings mean the same absence here.
   */
  reason: z.string().nullish(),
  /**
   * Instant of the read this family comes from. Absent when the collector has
   * never read this family — the same absence as being missing from `families`.
   */
  read_at: timestamp.optional(),
  data: z.unknown(),
})
export type DetailFamily = z.infer<typeof detailFamilySchema>

export const detailSchema = z.object({

  source: z.string().nullish(),  /** Absent when the collector has never read the detail at all. */
  read_at: timestamp.optional(),
  /** `null` and an absent key both mean "nothing was read" (nil slice). */
  families: z.array(detailFamilySchema).nullish(),
})
export type Detail = z.infer<typeof detailSchema>

/**
 * One measured quantity of the inverter, in the base unit of `unit`.
 *
 * Every field but `key` is nullable: the backend publishes `null` for a value
 * the device did not report (never a zero), and passes a stored answer through
 * untouched when it cannot read it.
 */
export const detailMeasurementSchema = z.object({
  /** Machine key (`PAC`, `DAY_ENERGY`…), displayed in monospace. */
  key: z.string(),
  label: z.string().nullish(),
  /** Unit to display as-is: `W`, `Wh`, `V`, `A`, `VA`. */
  unit: z.string().nullish(),
  /** `null` (or absent) when the device reported no value: never a zero. */
  value: z.number().nullish(),
})
export type DetailMeasurement = z.infer<typeof detailMeasurementSchema>

/** Inverter state: the word, plus the two numeric codes that qualify it. */
export const detailInverterStateSchema = z.object({
  inverter_state: z.string().nullish(),
  status_code: z.number().nullish(),
  error_code: z.number().nullish(),
})
export type DetailInverterState = z.infer<typeof detailInverterStateSchema>

/** Base-unit energies (Wh); each stays `null` when the device reports none. */
export const detailEnergiesSchema = z.object({
  day_wh: z.number().nullish(),
  year_wh: z.number().nullish(),
  total_wh: z.number().nullish(),
})
export type DetailEnergies = z.infer<typeof detailEnergiesSchema>

export const detailInverterDataSchema = z.object({
  state: detailInverterStateSchema.nullish(),
  /** Ordered by the backend: never sorted here. */
  measurements: z.array(detailMeasurementSchema).nullish(),
  energies: detailEnergiesSchema.nullish(),
})
export type DetailInverterData = z.infer<typeof detailInverterDataSchema>

/**
 * One interface declared by the system family: its name and its kind.
 *
 * Both are pointers on the Go side: `null` is the device not naming either, and
 * the page renders that absence as a dash.
 */
export const detailSystemInterfaceSchema = z.object({
  name: z.string().nullish(),
  type: z.string().nullish(),
})
export type DetailSystemInterface = z.infer<typeof detailSystemInterfaceSchema>

export const detailSystemDataSchema = z.object({
  system_name: z.string().nullish(),
  service_state: z.string().nullish(),
  update_running: z.boolean().nullish(),
  support_enabled: z.boolean().nullish(),
  connected_via_ap: z.boolean().nullish(),
  /**
   * Declared accounts by name: `{customer: true, technician: false}`. A value
   * is `null` when the device did not declare that account — which is a third
   * state, not a `false`.
   */
  accounts: z.record(z.string(), z.boolean().nullish()).nullish(),
  interfaces: z.array(detailSystemInterfaceSchema).nullish(),
})
export type DetailSystemData = z.infer<typeof detailSystemDataSchema>

/** One network interface: addressing, mode and radio state. */
export const detailNetworkInterfaceSchema = z.object({
  name: z.string(),
  ip: z.string().nullish(),
  mask: z.string().nullish(),
  gateway: z.string().nullish(),
  dns: z.array(z.string()).nullish(),
  configured: z.string().nullish(),
  interface_mode: z.string().nullish(),
  status: z.string().nullish(),
  ssid: z.string().nullish(),
  wps_state: z.string().nullish(),
})
export type DetailNetworkInterface = z.infer<typeof detailNetworkInterfaceSchema>

export const detailNetworkDataSchema = z.object({
  fdmp_connected: z.boolean().nullish(),
  interfaces: z.array(detailNetworkInterfaceSchema).nullish(),
})
export type DetailNetworkData = z.infer<typeof detailNetworkDataSchema>

export const detailDeviceSchema = z.object({
  /** Kind of the device, as the inverter names it; `null` when it names none. */
  type: z.string().nullish(),
  /** Numeric status code: `0` is a real code, not an absence. */
  status: z.number().nullish(),
  status_message: z.string().nullish(),
})
export type DetailDevice = z.infer<typeof detailDeviceSchema>

export const detailDevicesDataSchema = z.object({
  devices: z.array(detailDeviceSchema).nullish(),
})
export type DetailDevicesData = z.infer<typeof detailDevicesDataSchema>

/**
 * One `path → value` pair of a generic family.
 *
 * The value is a JSON scalar; it is kept `unknown` so that a shape this build
 * does not anticipate (an object, an array) still renders as text rather than
 * failing the whole table.
 */
export const detailEntrySchema = z.object({
  path: z.string(),
  value: z.unknown(),
})
export type DetailEntry = z.infer<typeof detailEntrySchema>

/**
 * The generic shape of a family: EMRS today, and any family the backend adds
 * later — a flat list whose values are JSON scalars.
 */
export const detailEntriesDataSchema = z.object({
  entries: z.array(detailEntrySchema).nullish(),
})
export type DetailEntriesData = z.infer<typeof detailEntriesDataSchema>

/**
 * One attribute of a cache node. The device labels these itself, so both the
 * label and the value stay the text the device answered — a `createTS` is not a
 * timestamp this interface may reformat.
 */
export const detailCacheAttributeSchema = z.object({
  key: z.string(),
  value: z.string().nullish(),
})
export type DetailCacheAttribute = z.infer<typeof detailCacheAttributeSchema>

/**
 * One register of a cache node.
 *
 * `key` is the device's own identifier and is kept verbatim. A register the
 * device named carries the type suffix the device itself appends (`_F32`,
 * `_U64`…), published separately as `kind`; a register it identifies only by a
 * bare `<id>` has neither `name` nor `kind` (`omitempty` on the Go side) and is
 * displayed as it stands.
 *
 * `value` is the device's own JSON value, left as it arrived, and `type` names
 * its JSON type (`int`, `float`, `bool`, `string`, `null`): an energy counter is
 * exact, so rounding it here would invent a number the device did not report.
 * No unit exists in the cache, and none is ever displayed.
 */
export const detailCacheChannelSchema = z.object({
  key: z.string(),
  name: z.string().nullish(),
  kind: z.string().nullish(),
  /** The device's own JSON type; absent or unknown falls back to the value's shape. */
  type: z.string().nullish(),
  value: z.unknown(),
})
export type DetailCacheChannel = z.infer<typeof detailCacheChannelSchema>

/** One node of the readable cache: an identifier, its attributes, its registers. */
export const detailCacheNodeSchema = z.object({
  /** The device's node identifier (393216 on the reference unit). */
  id: z.string(),
  attributes: z.array(detailCacheAttributeSchema).nullish(),
  channels: z.array(detailCacheChannelSchema).nullish(),
})
export type DetailCacheNode = z.infer<typeof detailCacheNodeSchema>

/**
 * The readable cache: the inverter's internal register set, grouped by node —
 * the richest diagnostic the device exposes, and the one it names itself.
 *
 * `nodes` may be absent or `null`: the same "nothing was read" as an empty list,
 * never a table of blanks. The backend orders the channels (named first, then
 * the bare identifiers) and this shape preserves that order.
 */
export const detailCacheDataSchema = z.object({
  nodes: z.array(detailCacheNodeSchema).nullish(),
})
export type DetailCacheData = z.infer<typeof detailCacheDataSchema>

/**
 * One family by its name. `undefined` means the collector never read it: a fact
 * of its own, rendered as such — never as an unavailable family, never as zeros.
 */
export function findDetailFamily(payload: Detail | null | undefined, name: string): DetailFamily | undefined {
  return (payload?.families ?? []).find((family) => family.family === name)
}

/**
 * Typed read of a family's `data`. `null` when the family is absent, carries no
 * data, or its shape is not the expected one — the caller then says the answer
 * could not be read rather than render a half-empty table.
 */
export function parseDetailFamilyData<T>(family: DetailFamily | undefined, schema: z.ZodType<T>): T | null {
  if (!family) {
    return null
  }
  const result = schema.safeParse(family.data)
  return result.success ? result.data : null
}

// ---------------------------------------------------------------------------
// Connectivity test
// ---------------------------------------------------------------------------

/**
 * State of one probe (`GET /api/v1/connectivity`), mapped by the backend from
 * the classified device error. The list is closed: a state added on the Go side
 * fails validation here rather than rendering as an unlabelled badge.
 */
export const connectivityProbeStateSchema = z.enum([
  'ok',
  'skipped',
  'unauthorized',
  'absent',
  'timeout',
  'unreachable',
  'error',
])
export type ConnectivityProbeState = z.infer<typeof connectivityProbeStateSchema>

export const connectivityProbeSchema = z.object({
  name: z.string(),
  path: z.string(),
  state: connectivityProbeStateSchema,
  /** Present when a response was received. */
  http_status: z.number().optional(),
  duration_ms: z.number(),
  /** Firmware string for `internal_version` only; empty elsewhere. */
  detail: z.string().optional(),
  /** Classification of the failure, never a raw body. */
  reason: z.string().optional(),
})
export type ConnectivityProbe = z.infer<typeof connectivityProbeSchema>

/**
 * Overall verdict: `ok` when every probe is `ok` or `skipped`, `failed` when
 * the device is not reachable as the application reaches it, `partial` when it
 * answers but the internal route or the credentials do not.
 */
export const connectivityOverallSchema = z.enum(['ok', 'partial', 'failed'])
export type ConnectivityOverall = z.infer<typeof connectivityOverallSchema>

export const connectivitySchema = z.object({
  tested_at: timestamp,
  /** The service reused a result younger than its 10 s window. */
  cached: z.boolean(),
  credentials_configured: z.boolean(),
  overall: connectivityOverallSchema,
  probes: z.array(connectivityProbeSchema),
})
export type Connectivity = z.infer<typeof connectivitySchema>

// ---------------------------------------------------------------------------
// Envelopes per endpoint
// ---------------------------------------------------------------------------

export const healthEnvelopeSchema = envelopeSchema(healthSchema)
export const systemEnvelopeSchema = envelopeSchema(systemSchema)
export const connectivityEnvelopeSchema = envelopeSchema(connectivitySchema)
/** `data` is `null` when no measurement exists yet. */
export const liveEnvelopeSchema = envelopeSchema(sampleSchema.nullable())
export const timeseriesEnvelopeSchema = envelopeSchema(z.array(bucketSchema))
/** `data` is `[]` for a range that holds nothing; `null` is accepted as the same absence. */
export const energyDailyEnvelopeSchema = envelopeSchema(z.array(dailyEnergySchema).nullish())
/** `data` is `[]` when the window holds no event; `null` is accepted as the same absence. */
export const eventsEnvelopeSchema = envelopeSchema(z.array(eventSchema).nullish())
export const capabilitiesEnvelopeSchema = envelopeSchema(z.array(capabilitySchema))
export const sourcesEnvelopeSchema = envelopeSchema(sourcesSchema)
export const detailEnvelopeSchema = envelopeSchema(detailSchema)

export type HealthEnvelope = z.infer<typeof healthEnvelopeSchema>
export type SystemEnvelope = z.infer<typeof systemEnvelopeSchema>
export type ConnectivityEnvelope = z.infer<typeof connectivityEnvelopeSchema>
export type LiveEnvelope = z.infer<typeof liveEnvelopeSchema>
export type TimeseriesEnvelope = z.infer<typeof timeseriesEnvelopeSchema>
export type EnergyDailyEnvelope = z.infer<typeof energyDailyEnvelopeSchema>
export type EventsEnvelope = z.infer<typeof eventsEnvelopeSchema>
export type CapabilitiesEnvelope = z.infer<typeof capabilitiesEnvelopeSchema>
export type SourcesEnvelope = z.infer<typeof sourcesEnvelopeSchema>
export type DetailEnvelope = z.infer<typeof detailEnvelopeSchema>

/** Error code signalling a still-empty database (`/api/v1/live`). */
export const CODE_NO_DATA = 'no_data'

// ---------------------------------------------------------------------------
// Compile-time link to the generated contract
// ---------------------------------------------------------------------------
//
// `api/openapi.json` is the machine-readable contract of the Go DTOs and
// `api-types.ts` is generated from it. Each validator below is tied to its
// generated counterpart by a type assertion, so a field renamed, removed,
// added or retyped on the Go side is a `tsc` error here instead of a runtime
// surprise.
//
// The check is deliberately one-directional in the *values* it accepts:
// `Wire extends z.input<Schema>`. Every payload the contract describes must be
// accepted by the validator, which is exactly the validator's job at the
// boundary. A validator that is intentionally more tolerant than the contract
// (an unknown enum member rendered as its raw token, a nil Go slice serialised
// as `null`) stays valid; one that is too *narrow* fails. Property names are
// compared exactly, so adding a key on either side fails too.

/** Every value the generated contract describes is accepted by the schema. */
type Accepts<Schema extends z.ZodTypeAny, Wire> = [Wire] extends [z.input<Schema>] ? true : false

/** Every key of the generated type is declared by the schema. */
type Covers<Schema extends z.ZodTypeAny, Wire> = [Exclude<keyof Wire, keyof z.input<Schema>>] extends [never]
  ? true
  : false

/** The schema declares no key the generated type does not have. */
type KeysMatch<Schema extends z.ZodTypeAny, Wire> =
  Covers<Schema, Wire> extends true
    ? [Exclude<keyof z.input<Schema>, keyof Wire>] extends [never]
      ? true
      : false
    : false

/** A DTO link: accepted values and the exact same set of property names. */
type Linked<Name extends string, Schema extends z.ZodTypeAny, Wire> =
  Accepts<Schema, Wire> extends true
    ? KeysMatch<Schema, Wire> extends true
      ? true
      : `keys:${Name}`
    : `accepts:${Name}`

/**
 * An envelope link. The generated success envelope declares `error` only on
 * `/live`, while `envelopeSchema` always carries it as an optional key, so only
 * the generated keys must be covered.
 */
type EnvelopeLinked<Name extends string, Schema extends z.ZodTypeAny, Wire> =
  Accepts<Schema, Wire> extends true
    ? Covers<Schema, Wire> extends true
      ? true
      : `keys:${Name}`
    : `accepts:${Name}`

/** A schema for a bare value (an enum), with no property names to compare. */
type ValueLinked<Name extends string, Schema extends z.ZodTypeAny, Wire> =
  Accepts<Schema, Wire> extends true ? true : `accepts:${Name}`

type JsonOk<Operation extends keyof operations> =
  operations[Operation]['responses'][200]['content']['application/json']

type ContractLinks = [
  Linked<'Meta', typeof metaSchema, components['schemas']['Meta']>,
  Linked<'ApiError', typeof apiErrorSchema, components['schemas']['ApiError']>,
  Linked<'ErrorEnvelope', typeof errorEnvelopeSchema, components['schemas']['ErrorEnvelope']>,
  Linked<'DBHealthDTO', typeof dbHealthSchema, components['schemas']['DBHealthDTO']>,
  Linked<'CollectorHealthDTO', typeof collectorHealthSchema, components['schemas']['CollectorHealthDTO']>,
  Linked<'HealthDTO', typeof healthSchema, components['schemas']['HealthDTO']>,
  Linked<'SystemDTO', typeof systemSchema, components['schemas']['SystemDTO']>,
  Linked<'DeviceDTO', typeof deviceSchema, components['schemas']['DeviceDTO']>,
  Linked<'DetectionDTO', typeof detectionSchema, components['schemas']['DetectionDTO']>,
  Linked<'DetectionResourceDTO', typeof detectionResourceSchema, components['schemas']['DetectionResourceDTO']>,
  Linked<'ClockResourceDTO', typeof clockResourceSchema, components['schemas']['ClockResourceDTO']>,
  Linked<'ServiceResourceDTO', typeof serviceResourceSchema, components['schemas']['ServiceResourceDTO']>,
  Linked<'ConnectivityDTO', typeof connectivitySchema, components['schemas']['ConnectivityDTO']>,
  Linked<'ConnectivityProbeDTO', typeof connectivityProbeSchema, components['schemas']['ConnectivityProbeDTO']>,
  ValueLinked<'ConnectivityProbeDTO.state', typeof connectivityProbeStateSchema, components['schemas']['ConnectivityProbeDTO']['state']>,
  ValueLinked<'ConnectivityDTO.overall', typeof connectivityOverallSchema, components['schemas']['ConnectivityDTO']['overall']>,
  Linked<'SampleDTO', typeof sampleSchema, components['schemas']['SampleDTO']>,
  Linked<'InverterDTO', typeof inverterSchema, components['schemas']['InverterDTO']>,
  Linked<'DerivedEnergyDTO', typeof derivedEnergySchema, components['schemas']['DerivedEnergyDTO']>,
  Linked<'BucketDTO', typeof bucketSchema, components['schemas']['BucketDTO']>,
  Linked<'DailyEnergyDTO', typeof dailyEnergySchema, components['schemas']['DailyEnergyDTO']>,
  Linked<'EventDTO', typeof eventSchema, components['schemas']['EventDTO']>,
  Linked<'CapabilityDTO', typeof capabilitySchema, components['schemas']['CapabilityDTO']>,
  Linked<'SourcesDTO', typeof sourcesSchema, components['schemas']['SourcesDTO']>,
  Linked<'SurveyDTO', typeof sourceSurveySchema, components['schemas']['SurveyDTO']>,
  Linked<'SourceDTO', typeof sourceSchema, components['schemas']['SourceDTO']>,
  Linked<'ProbeDTO', typeof sourceProbeSchema, components['schemas']['ProbeDTO']>,
  Linked<'ProbeField', typeof probeFieldSchema, components['schemas']['ProbeField']>,
  Linked<'SourceSummary', typeof sourceSummarySchema, components['schemas']['SourceSummary']>,
  Linked<'SourceField', typeof sourceFieldSchema, components['schemas']['SourceField']>,
  Linked<'SourceProof', typeof sourceProofSchema, components['schemas']['SourceProof']>,
  Linked<'DetailDTO', typeof detailSchema, components['schemas']['DetailDTO']>,
  Linked<'DetailFamilyDTO', typeof detailFamilySchema, components['schemas']['DetailFamilyDTO']>,
  Linked<'DetailMeasurementDTO', typeof detailMeasurementSchema, components['schemas']['DetailMeasurementDTO']>,
  Linked<'InverterDetailDTO', typeof detailInverterDataSchema, components['schemas']['InverterDetailDTO']>,
  Linked<'InverterStateDetail', typeof detailInverterStateSchema, components['schemas']['InverterStateDetail']>,
  Linked<'InverterEnergies', typeof detailEnergiesSchema, components['schemas']['InverterEnergies']>,
  EnvelopeLinked<'HealthEnvelope', typeof healthEnvelopeSchema, JsonOk<'getHealth'>>,
  EnvelopeLinked<'SystemEnvelope', typeof systemEnvelopeSchema, JsonOk<'getSystem'>>,
  EnvelopeLinked<'ConnectivityEnvelope', typeof connectivityEnvelopeSchema, JsonOk<'getConnectivity'>>,
  EnvelopeLinked<'LiveEnvelope', typeof liveEnvelopeSchema, JsonOk<'getLive'>>,
  EnvelopeLinked<'TimeseriesEnvelope', typeof timeseriesEnvelopeSchema, JsonOk<'getTimeSeries'>>,
  EnvelopeLinked<'EnergyDailyEnvelope', typeof energyDailyEnvelopeSchema, JsonOk<'getDailyEnergy'>>,
  EnvelopeLinked<'EventsEnvelope', typeof eventsEnvelopeSchema, JsonOk<'getEvents'>>,
  EnvelopeLinked<'CapabilitiesEnvelope', typeof capabilitiesEnvelopeSchema, JsonOk<'getCapabilities'>>,
  EnvelopeLinked<'SourcesEnvelope', typeof sourcesEnvelopeSchema, JsonOk<'getSources'>>,
  EnvelopeLinked<'DetailEnvelope', typeof detailEnvelopeSchema, JsonOk<'getDetail'>>,
]

/** Fails to compile (naming the DTO) as soon as one link above drifts. */
type AllLinked<Links extends readonly true[]> = Links
export type ApiContractLinks = AllLinked<ContractLinks>
