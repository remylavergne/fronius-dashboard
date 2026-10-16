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
  it('returns the validated payload', async () => {
    stubFetch(async () => jsonResponse(validHealthBody))
    const result = await fetchJSON('/api/v1/health', healthEnvelopeSchema)
    expect(result.data.version).toBe('test-version')
    expect(result.meta.generated_at).toBe('2026-10-16T09:17:43.000Z')
  })

  it('sends a GET with Accept: application/json', async () => {
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

  it('classifies an unexpected response as a schema error', async () => {
    // `uptime_s` as a string: exactly the kind of DTO drift zod must see.
    stubFetch(async () => jsonResponse({ ...validHealthBody, data: { ...validHealthBody.data, uptime_s: 'twelve' } }))

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).kind).toBe('schema')
  })

  it('classifies an unreadable 200 as a schema error', async () => {
    stubFetch(async () => new Response('<html>error</html>', { status: 200 }))
    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('schema')
  })

  it('classifies an HTTP error as an http error, with its code', async () => {
    stubFetch(async () => jsonResponse({ error: { code: 'invalid_step', message: 'unknown step' } }, 400))

    const error = await fetchJSON('/api/v1/timeseries', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('http')
    expect((error as ApiError).status).toBe(400)
    expect((error as ApiError).code).toBe('invalid_step')
    expect((error as ApiError).message).toBe('unknown step')
  })

  it('keeps the HTTP status when the error body is unusable', async () => {
    stubFetch(async () => new Response('Bad Gateway', { status: 502 }))

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('http')
    expect((error as ApiError).status).toBe(502)
    expect((error as ApiError).code).toBeUndefined()
  })

  it('classifies a fetch rejection as a network error', async () => {
    stubFetch(async () => {
      throw new TypeError('Failed to fetch')
    })

    const error = await fetchJSON('/api/v1/health', healthEnvelopeSchema).catch((e: unknown) => e)
    expect((error as ApiError).kind).toBe('network')
    expect((error as ApiError).status).toBeUndefined()
  })

  it('accepts `data: null` with a no_data code on /live', async () => {
    // State of a still-empty database: 200, data null, error.code = no_data.
    stubFetch(async () =>
      jsonResponse({
        data: null,
        meta: { generated_at: '2026-10-16T09:17:43.000Z' },
        error: { code: 'no_data', message: 'no sample recorded' },
      }),
    )

    const result = await fetchJSON('/api/v1/live', liveEnvelopeSchema)
    expect(result.data).toBeNull()
    expect(result.error?.code).toBe('no_data')
  })
})
