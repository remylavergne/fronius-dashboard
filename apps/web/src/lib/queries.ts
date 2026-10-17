/**
 * TanStack Query requests to the monitoring API.
 *
 * Refresh policy (ADR 0002): client-side polling, with
 * `refetchInterval: 5000` — the same cadence as the collector, itself aligned
 * with the 5000 ms of the official client. `staleTime` slightly lower so that
 * two successive requests do not overlap, `retry: 1` to absorb a network hiccup
 * without masking a failure, and `throwOnError: false` so that each page
 * decides its own error rendering.
 */
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import { fetchJSON } from './api'
import {
  capabilitiesEnvelopeSchema,
  connectivityEnvelopeSchema,
  detailEnvelopeSchema,
  energyDailyEnvelopeSchema,
  eventsEnvelopeSchema,
  healthEnvelopeSchema,
  liveEnvelopeSchema,
  sourcesEnvelopeSchema,
  systemEnvelopeSchema,
  timeseriesEnvelopeSchema,
} from './schema'
import type { CapabilitiesEnvelope, ConnectivityEnvelope, DetailEnvelope, EnergyDailyEnvelope, EventsEnvelope, HealthEnvelope, LiveEnvelope, SourcesEnvelope, SystemEnvelope, TimeseriesEnvelope } from './schema'

const POLL_MS = 5000

const pollOptions = {
  refetchInterval: POLL_MS,
  staleTime: 4000,
  retry: 1,
  throwOnError: false,
} as const

export function useHealth(): UseQueryResult<HealthEnvelope> {
  return useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/health', healthEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export function useSystem(): UseQueryResult<SystemEnvelope> {
  return useQuery({
    queryKey: ['system'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/system', systemEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export function useLive(): UseQueryResult<LiveEnvelope> {
  return useQuery({
    queryKey: ['live'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/live', liveEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export function useCapabilities(): UseQueryResult<CapabilitiesEnvelope> {
  return useQuery({
    queryKey: ['capabilities'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/capabilities', capabilitiesEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export function useSources(): UseQueryResult<SourcesEnvelope> {
  return useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/sources', sourcesEnvelopeSchema, signal),
    ...pollOptions,
  })
}

/**
 * Read-only connectivity test, on demand.
 *
 * `enabled: false`: nothing probes the device on mount — only the operator's
 * press does. `retry: false`: an automatic second attempt would hide the very
 * failure the test exists to report. `cache: 'no-store'`: a re-test must reach
 * the service; the service's own 10 s window is the only cache allowed to
 * answer it, and it says so through `cached`.
 *
 * The query refetches on every press, including inside that window: the fresh
 * payload then carries `cached: true` and the original `tested_at`.
 */
export function useConnectivity(): UseQueryResult<ConnectivityEnvelope> {
  return useQuery({
    queryKey: ['connectivity'],
    queryFn: ({ signal }) =>
      fetchJSON('/api/v1/connectivity', connectivityEnvelopeSchema, signal, { cache: 'no-store' }),
    enabled: false,
    retry: false,
    throwOnError: false,
  })
}

/**
 * Device detail.
 *
 * The poll cadence is the interface's, not the device's: the answer carries what
 * the collector last read, on its own slow cycle. Polling this path therefore
 * costs one read of the API's cache, never a read of the device.
 */
export function useDetail(): UseQueryResult<DetailEnvelope> {
  return useQuery({
    queryKey: ['detail'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/detail', detailEnvelopeSchema, signal),
    ...pollOptions,
  })
}

/**
 * Daily energy, one row per local day of the requested range.
 *
 * `from` and `to` are RFC3339 **instants in UTC**, as everywhere else: the
 * server parses them with `time.RFC3339` and refuses a bare date with
 * `400 invalid_range`. The range is bounded server-side to 366 days. Both are
 * optional; without them the backend answers the last `defaultDailyDays` days
 * (30) — passing them is what makes the page's window explicit, and it keeps the
 * query key stable from one minute to the next.
 *
 * The days themselves are the *installation's* local days, whatever instants
 * bound the request.
 */
export function useEnergyDaily(params: EnergyDailyParams): UseQueryResult<EnergyDailyEnvelope> {
  const search = new URLSearchParams()
  if (params.from !== undefined) {
    search.set('from', params.from)
  }
  if (params.to !== undefined) {
    search.set('to', params.to)
  }
  const query = search.toString()
  return useQuery({
    queryKey: ['energy', 'daily', query],
    queryFn: ({ signal }) => fetchJSON(`/api/v1/energy/daily?${query}`, energyDailyEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export type EnergyDailyParams = { from?: string | undefined; to?: string | undefined }

/**
 * Events, newest first, at most `limit` of them (`1..500`, 100 by default).
 *
 * `from` and `to` are RFC3339 instants in UTC, and `to` must be after `from`:
 * the API answers `400 invalid_range` otherwise. The endpoint has neither a
 * severity nor a kind parameter — those two filters belong to the page, applied
 * over the page it fetched, which is exactly why `limit` is a real choice and
 * why the page states how many of the fetched events it shows.
 */
export function useEvents(params: EventsParams): UseQueryResult<EventsEnvelope> {
  const search = new URLSearchParams({ limit: String(params.limit) })
  if (params.from !== undefined) {
    search.set('from', params.from)
  }
  if (params.to !== undefined) {
    search.set('to', params.to)
  }
  const query = search.toString()
  return useQuery({
    queryKey: ['events', query],
    queryFn: ({ signal }) => fetchJSON(`/api/v1/events?${query}`, eventsEnvelopeSchema, signal),
    ...pollOptions,
  })
}

export type EventsParams = { limit: number; from?: string | undefined; to?: string | undefined }

/** Preset ranges offered by the overview view. */
export const RANGES = ['24h', '7d', '30d'] as const
export type RangeKey = (typeof RANGES)[number]

/** Preset windows offered by the events page; `all` asks for no bound at all. */
export const EVENT_RANGES = ['24h', '7d', '30d', 'all'] as const
export type EventRangeKey = (typeof EVENT_RANGES)[number]

/** Limits offered by the events page: the API accepts 1..500. */
export const EVENT_LIMITS = [50, 100, 250, 500] as const

/** Granularity steps accepted by the API (cf. internal/api/server.go). */
export const STEPS = ['auto', '1m', '5m', '15m', '1h', '1d'] as const
export type StepKey = (typeof STEPS)[number]

const RANGE_HOURS: Record<RangeKey, number> = {
  '24h': 24,
  '7d': 24 * 7,
  '30d': 24 * 30,
}

/**
 * Builds the parameters of a preset window of events.
 *
 * The upper bound is rounded to the minute, for the same reason as
 * `rangeToQuery`: an unrounded timestamp would change on every render and make
 * the query key unstable. `all` sends no bound at all — an absent window is an
 * unbounded one server-side.
 */
export function eventsRangeToQuery(
  range: EventRangeKey,
  limit: number,
  now: Date = new Date(),
): EventsParams {
  if (range === 'all') {
    return { limit }
  }
  const anchor = new Date(Math.floor(now.getTime() / 60_000) * 60_000)
  return {
    limit,
    from: new Date(anchor.getTime() - RANGE_HOURS[range] * 3600 * 1000).toISOString(),
    to: anchor.toISOString(),
  }
}

export type TimeseriesParams = { from: string; to: string; step: string }

/**
 * Builds the parameters of a preset range.
 *
 * The upper bound is rounded to the minute: without this rounding, the
 * timestamp would change on every render and the query key would become
 * unstable, triggering reload loops. `step: 'auto'` lets the backend choose the
 * finest step producing at most 1500 points.
 */
export function rangeToQuery(range: RangeKey, now: Date = new Date()): TimeseriesParams {
  const anchor = new Date(Math.floor(now.getTime() / 60_000) * 60_000)
  return {
    from: new Date(anchor.getTime() - RANGE_HOURS[range] * 3600 * 1000).toISOString(),
    to: anchor.toISOString(),
    step: 'auto',
  }
}

/**
 * Converts a value of `<input type="datetime-local">` into an RFC3339
 * timestamp, or `null` if it is empty or unreadable. The string without a time
 * zone is interpreted by the browser as a local time, which is the expected
 * behaviour.
 */
export function localInputToISO(value: string): string | null {
  if (value === '') {
    return null
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date.toISOString()
}

/** Converts a timestamp into a value of `<input type="datetime-local">` (local time). */
export function isoToLocalInput(iso: string | Date): string {
  const date = typeof iso === 'string' ? new Date(iso) : iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function useTimeseries(params: TimeseriesParams, enabled = true): UseQueryResult<TimeseriesEnvelope> {
  const search = new URLSearchParams({ from: params.from, to: params.to, step: params.step }).toString()
  return useQuery({
    queryKey: ['timeseries', search],
    queryFn: ({ signal }) => fetchJSON(`/api/v1/timeseries?${search}`, timeseriesEnvelopeSchema, signal),
    enabled,
    ...pollOptions,
  })
}

/**
 * Clock refreshed every minute.
 *
 * Anchor of sliding ranges: the window shifts by itself, without changing the
 * query key more than once per minute.
 */
export function useMinuteTick(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 60_000)
    return () => window.clearInterval(id)
  }, [])
  return now
}
