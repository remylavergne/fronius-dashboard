import { afterEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useConnectivity } from './queries'

/**
 * `useConnectivity` is the only query of the API that must never run on mount
 * and must run on every press: the service caches its answer for 10 s, and the
 * client must still ask, so that `cached: true` comes from the server rather
 * than from the browser's HTTP cache.
 */
const PAYLOAD = {
  data: {
    tested_at: '2026-10-17T09:12:04Z',
    cached: true,
    credentials_configured: true,
    overall: 'ok',
    probes: [
      {
        name: 'solar_api_powerflow',
        path: '/solar_api/v1/GetPowerFlowRealtimeData.fcgi',
        state: 'ok',
        http_status: 200,
        duration_ms: 41,
      },
    ],
  },
  meta: { generated_at: '2026-10-17T09:12:04Z' },
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

/** Minimal consumer: one press button and the payload's overall verdict. */
function Harness() {
  const connectivity = useConnectivity()
  return (
    <div>
      <button type="button" onClick={() => void connectivity.refetch()} disabled={connectivity.isFetching}>
        press
      </button>
      <p>{connectivity.data ? connectivity.data.data.overall : 'empty'}</p>
      {connectivity.data?.data.cached ? <p>cached</p> : null}
    </div>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useConnectivity', () => {
  it('stays idle on mount and refetches on every press, bypassing the HTTP cache', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(async () =>
      jsonResponse(PAYLOAD),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = new QueryClient()

    render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    )

    expect(screen.getByText('empty')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'press' }))
    await waitFor(() => expect(screen.getByText('ok')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('cached')).toBeInTheDocument()

    // A second press is a second request, even though the server answers from
    // its own 10 s window: the client never treats `cached` as an error.
    await user.click(screen.getByRole('button', { name: 'press' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ cache: 'no-store' })
    expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({ cache: 'no-store' })
  })
})
