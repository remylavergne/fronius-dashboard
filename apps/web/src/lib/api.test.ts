import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchJSON } from './api'
import { healthEnvelopeSchema, liveEnvelopeSchema } from './schema'

const validHealthBody = {
  data: {
    status: 'ok',
    version: 'test-version',
    uptime_s: 12,
    db: { ok: true },
    collector: { last_success_utc: '', last_error: '', last_run_ms: 0, samples_total: 0 },
  },
  meta: { generated_at: '2026-10-16T09:17:43.000Z' },
}

function stubFetch(impl: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>) {
  vi.stubGlobal('fetch', vi.fn(impl))
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchJSON', () => {
  it('retourne la charge utile validée', async () => {
    stubFetch(async () => jsonResponse(validHealthBody))
    const result = await fetchJSON('/api/v1/health', healthEnvelopeSchema)
    expect(result.data.version).toBe('test-version')
    expect(result.meta.generated_at).toBe('2026-10-16T09:17:43.000Z')
  })

  it('envoie un GET avec Accept: application/json', async () => {
    const spy = vi.fn<(path: string, init: RequestInit) => Promise<Response>>(async () =>
      jsonResponse(validHealthBody),
    )
    vi.stubGlobal('fetch', spy)

    await fetchJSON('/api/v1/health', healthEnvelopeSchema)

    expect(spy).toHaveBeenCalledTimes(1)
    const [path, init] = spy.mock.calls[0] ?? []
    expect(path).toBe('/api/v1/health')
    expect(init?.headers).toMatchObject({ Accept: 'application/json' })
  })

  it('classe une réponse inattendue en erreur de schéma', async () => {
    // `uptime_s` en chaîne : exactement le genre de dérive DTO que zod doit voir.
    stubFetch(async () => jsonResponse({ ...validHealthBody, data: { ...validHealthBody.data, uptime_s: 'douze' } }))

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).kind).toBe('schema')
  })

  it('classe un 200 illisible en erreur de schéma', async () => {
    stubFetch(async () => new Response('<html>erreur</html>', { status: 200 }))
    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('schema')
  })

  it('classe une erreur HTTP en erreur http, avec son code', async () => {
    stubFetch(async () => jsonResponse({ error: { code: 'invalid_step', message: 'step inconnu' } }, 400))

    const error = await fetchJSON('/api/v1/timeseries', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('http')
    expect((error as ApiError).status).toBe(400)
    expect((error as ApiError).code).toBe('invalid_step')
    expect((error as ApiError).message).toBe('step inconnu')
  })

  it('garde le statut HTTP quand le corps d’erreur n’est pas exploitable', async () => {
    stubFetch(async () => new Response('Bad Gateway', { status: 502 }))

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('http')
    expect((error as ApiError).status).toBe(502)
    expect((error as ApiError).code).toBeUndefined()
  })

  it('classe un rejet de fetch en erreur réseau', async () => {
    stubFetch(async () => {
      throw new TypeError('Failed to fetch')
    })

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('network')
    expect((error as ApiError).status).toBeUndefined()
  })

  it('accepte `data: null` avec un code no_data sur /live', async () => {
    // État d'une base encore vide : 200, data null, error.code = no_data.
    stubFetch(async () =>
      jsonResponse({
        data: null,
        meta: { generated_at: '2026-10-16T09:17:43.000Z' },
        error: { code: 'no_data', message: 'aucun échantillon enregistré' },
      }),
    )

    const result = await fetchJSON('/api/v1/live', liveEnvelopeSchema)
    expect(result.data).toBeNull()
    expect(result.error?.code).toBe('no_data')
  })
})
