/**
 * HTTP client of the monitoring API.
 *
 * The frontend only addresses `/api` (relayed to `froniusd` by the Vite proxy
 * in development). It knows neither the inverter address nor any credential:
 * the device credentials stay in the service environment and never cross this
 * boundary.
 */
import type { ZodType } from 'zod'
import { errorEnvelopeSchema } from './schema'

export type ApiErrorKind = 'network' | 'http' | 'schema'

/** API access error, classified by nature. */
export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly status: number | undefined
  readonly code: string | undefined

  constructor(
    kind: ApiErrorKind,
    message: string,
    options: { status?: number; code?: string; cause?: unknown } = {},
  ) {
    super(message, 'cause' in options ? { cause: options.cause } : undefined)
    this.name = 'ApiError'
    this.kind = kind
    this.status = options.status
    this.code = options.code
  }
}

function describeFailure(cause: unknown): string {
  if (cause instanceof Error) {
    if (cause.name === 'AbortError') {
      return 'request cancelled'
    }
    return cause.message
  }
  return 'network failure'
}

/**
 * Executes a GET request and validates the response.
 *
 * Three failures are distinguished: `network` (the API is unreachable), `http`
 * (the API returned an error) and `schema` (unexpected response — the signal of
 * a drift between the Go DTOs and these schemas).
 */
export async function fetchJSON<T>(
  path: string,
  schema: ZodType<T>,
  signal?: AbortSignal,
): Promise<T> {
  const init: RequestInit = { headers: { Accept: 'application/json' } }
  if (signal) {
    init.signal = signal
  }

  let response: Response
  try {
    response = await fetch(path, init)
  } catch (cause) {
    throw new ApiError('network', describeFailure(cause), { cause })
  }

  const text = await response.text()
  let body: unknown
  try {
    body = text === '' ? null : JSON.parse(text)
  } catch (cause) {
    if (response.ok) {
      throw new ApiError('schema', 'unreadable response: invalid JSON', {
        status: response.status,
        cause,
      })
    }
    body = null
  }

  if (!response.ok) {
    const parsed = errorEnvelopeSchema.safeParse(body)
    throw new ApiError(
      'http',
      parsed.success ? parsed.data.error.message : `HTTP error ${response.status}`,
      parsed.success
        ? { status: response.status, code: parsed.data.error.code }
        : { status: response.status },
    )
  }

  const result = schema.safeParse(body)
  if (!result.success) {
    throw new ApiError('schema', 'unexpected API response', {
      status: response.status,
      cause: result.error,
    })
  }
  return result.data
}
