/**
 * Client HTTP de l'API de supervision.
 *
 * Le frontend ne s'adresse qu'à `/api` (relayé vers `froniusd` par le proxy
 * Vite en développement). Il ne connaît ni l'adresse de l'onduleur, ni un
 * identifiant quelconque : la phase 1 n'en possède aucun.
 */
import type { ZodType } from 'zod'
import { errorEnvelopeSchema } from './schema'

export type ApiErrorKind = 'network' | 'http' | 'schema'

/** Erreur d'accès à l'API, classée par nature. */
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
      return 'requête annulée'
    }
    return cause.message
  }
  return 'échec réseau'
}

/**
 * Exécute une requête GET et valide la réponse.
 *
 * Trois échecs sont distingués : `network` (l'API est injoignable), `http`
 * (l'API a répondu une erreur) et `schema` (réponse inattendue — c'est le
 * signal d'une dérive entre les DTO Go et ces schémas).
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
      throw new ApiError('schema', 'réponse illisible : JSON invalide', {
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
      parsed.success ? parsed.data.error.message : `erreur HTTP ${response.status}`,
      parsed.success
        ? { status: response.status, code: parsed.data.error.code }
        : { status: response.status },
    )
  }

  const result = schema.safeParse(body)
  if (!result.success) {
    throw new ApiError('schema', 'réponse inattendue de l’API', {
      status: response.status,
      cause: result.error,
    })
  }
  return result.data
}
