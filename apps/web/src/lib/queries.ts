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
  detailEnvelopeSchema,
  energyDailyEnvelopeSchema,
  healthEnvelopeSchema,
  liveEnvelopeSchema,
  sourcesEnvelopeSchema,
  systemEnvelopeSchema,
  timeseriesEnvelopeSchema,
} from './schema'
import type { CapabilitiesEnvelope, DetailEnvelope, EnergyDailyEnvelope, HealthEnvelope, LiveEnvelope, SourcesEnvelope, SystemEnvelope, TimeseriesEnvelope } from './schema'

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

export function useEnergyDaily(): UseQueryResult<EnergyDailyEnvelope> {
  return useQuery({
    queryKey: ['energy', 'daily'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/energy/daily', energyDailyEnvelopeSchema, signal),
    ...pollOptions,
  })
}

/** Preset ranges offered by the overview view. */
export const RANGES = ['24h', '7d', '30d'] as const
export type RangeKey = (typeof RANGES)[number]

/** Granularity steps accepted by the API (cf. internal/api/server.go). */
export const STEPS = ['auto', '1m', '5m', '15m', '1h', '1d'] as const
export type StepKey = (typeof STEPS)[number]

const RANGE_HOURS: Record<RangeKey, number> = {
  '24h': 24,
  '7d': 24 * 7,
  '30d': 24 * 30,
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
