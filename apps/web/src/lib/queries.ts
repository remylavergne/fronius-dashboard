/**
 * Requêtes TanStack Query vers l'API de supervision.
 *
 * Politique de rafraîchissement (ADR 0002) : polling côté client, avec
 * `refetchInterval: 5000` — la même cadence que le collecteur, elle-même
 * alignée sur les 5 000 ms du client officiel. `staleTime` légèrement inférieur
 * pour que deux requêtes successives ne se croisent pas, `retry: 1` pour
 * absorber un hoquet réseau sans masquer une panne, et `throwOnError: false`
 * pour que chaque page décide de son rendu d'erreur.
 */
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import { fetchJSON } from './api'
import {
  capabilitiesEnvelopeSchema,
  energyDailyEnvelopeSchema,
  healthEnvelopeSchema,
  liveEnvelopeSchema,
  systemEnvelopeSchema,
  timeseriesEnvelopeSchema,
} from './schema'
import type { CapabilitiesEnvelope, EnergyDailyEnvelope, HealthEnvelope, LiveEnvelope, SystemEnvelope, TimeseriesEnvelope } from './schema'

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

export function useEnergyDaily(): UseQueryResult<EnergyDailyEnvelope> {
  return useQuery({
    queryKey: ['energy', 'daily'],
    queryFn: ({ signal }) => fetchJSON('/api/v1/energy/daily', energyDailyEnvelopeSchema, signal),
    ...pollOptions,
  })
}

/** Plages prédéfinies proposées par la vue d'ensemble. */
export const RANGES = ['24h', '7d', '30d'] as const
export type RangeKey = (typeof RANGES)[number]

/** Pas de granularité acceptés par l'API (cf. internal/api/server.go). */
export const STEPS = ['auto', '1m', '5m', '15m', '1h', '1d'] as const
export type StepKey = (typeof STEPS)[number]

const RANGE_HOURS: Record<RangeKey, number> = {
  '24h': 24,
  '7d': 24 * 7,
  '30d': 24 * 30,
}

export type TimeseriesParams = { from: string; to: string; step: string }

/**
 * Construit les paramètres d'une plage prédéfinie.
 *
 * La borne haute est arrondie à la minute : sans cet arrondi, l'horodatage
 * changerait à chaque rendu et la clé de requête deviendrait instable, ce qui
 * déclencherait des rechargements en boucle. `step: 'auto'` laisse le backend
 * choisir le pas le plus fin produisant au plus 1500 points.
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
 * Convertit une valeur d'`<input type="datetime-local">` en horodatage RFC3339,
 * ou `null` si elle est vide ou illisible. La chaîne sans fuseau est interprétée
 * par le navigateur comme une heure locale, ce qui est le comportement attendu.
 */
export function localInputToISO(value: string): string | null {
  if (value === '') {
    return null
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date.toISOString()
}

/** Convertit un horodatage en valeur d'`<input type="datetime-local">` (heure locale). */
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
 * Horloge rafraîchie chaque minute.
 *
 * Ancre des plages glissantes : la fenêtre se décale d'elle-même, sans changer
 * la clé de requête plus d'une fois par minute.
 */
export function useMinuteTick(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 60_000)
    return () => window.clearInterval(id)
  }, [])
  return now
}
