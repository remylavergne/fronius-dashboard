import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { formatDateTime } from '@/lib/format'
import type {
  Connectivity,
  ConnectivityOverall,
  ConnectivityProbe,
  ConnectivityProbeState,
} from '@/lib/schema'
import { SettingsPage } from './Settings'

const { systemMock, connectivityMock, refetchMock } = vi.hoisted(() => ({
  systemMock: vi.fn(),
  connectivityMock: vi.fn(),
  refetchMock: vi.fn(),
}))

vi.mock('@/lib/queries', () => ({
  useSystem: () => systemMock(),
  useConnectivity: () => connectivityMock(),
}))

const GENERATED_AT = '2026-10-17T09:12:04Z'
const TESTED_AT = '2026-10-17T09:12:04Z'

/** `/api/v1/system` as the page receives it; only the fields shown matter here. */
function systemResult(credentialsConfigured: boolean) {
  return {
    data: {
      data: {
        version: '1.4.0',
        go_version: 'go1.25.0',
        solarapi_url: 'http://inverter.local/solar_api/v1/',
        poll_interval_s: 5,
        tz: 'Europe/Zurich',
        db_path: '/var/lib/fronius/fronius.db',
        retention_days: 30,
        api_addr: ':8080',
        stale_after_s: 45,
        detect_interval_s: 30,
        http_timeout_s: 7,
        credentials_configured: credentialsConfigured,
        device: null,
        detection: null,
      },
      meta: { generated_at: GENERATED_AT },
    },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  }
}

/** Result of `useConnectivity`, in the shape the page reads. */
function connectivityResult(
  payload: Connectivity | undefined,
  flags: { isFetching?: boolean; isError?: boolean; error?: Error } = {},
) {
  return {
    data: payload === undefined ? undefined : { data: payload, meta: { generated_at: GENERATED_AT } },
    isFetching: flags.isFetching ?? false,
    isLoading: false,
    isError: flags.isError ?? false,
    error: flags.error,
    refetch: refetchMock,
  }
}

function probe(
  name: string,
  state: ConnectivityProbeState,
  extra: Partial<ConnectivityProbe> = {},
): ConnectivityProbe {
  return {
    name,
    path: name === 'internal_version' ? '/status/version' : '/solar_api/v1/GetPowerFlowRealtimeData.fcgi',
    state,
    duration_ms: 41,
    ...extra,
  }
}

function payload(probes: ConnectivityProbe[], overall: ConnectivityOverall, extra: Partial<Connectivity> = {}): Connectivity {
  return { tested_at: TESTED_AT, cached: false, credentials_configured: true, overall, probes, ...extra }
}

beforeEach(() => {
  vi.clearAllMocks()
  systemMock.mockReturnValue(systemResult(true))
  connectivityMock.mockReturnValue(connectivityResult(undefined))
})

describe('SettingsPage configuration', () => {
  it('shows the device credentials as not configured, and never a value', () => {
    systemMock.mockReturnValue(systemResult(false))

    render(<SettingsPage />)

    expect(screen.getByText('not configured')).toBeInTheDocument()
    expect(screen.queryByText('configured')).not.toBeInTheDocument()
    // The payload carries no credential: the page cannot render one, and must
    // not invent a masked placeholder either.
    expect(screen.queryByText(/password|username|secret|hunter2|\*\*\*/i)).not.toBeInTheDocument()
  })

  it('shows the device credentials as configured, without any value', () => {
    systemMock.mockReturnValue(systemResult(true))

    render(<SettingsPage />)

    expect(screen.getByText('configured')).toBeInTheDocument()
    expect(screen.queryByText(/password|username|secret|hunter2|\*\*\*/i)).not.toBeInTheDocument()
  })

  it('renders the detection interval and the HTTP timeout', () => {
    render(<SettingsPage />)

    expect(screen.getByText('Detection interval')).toBeInTheDocument()
    expect(screen.getByText('30 s')).toBeInTheDocument()
    expect(screen.getByText('HTTP timeout')).toBeInTheDocument()
    expect(screen.getByText('7 s')).toBeInTheDocument()
  })
})

describe('SettingsPage connectivity', () => {
  it('runs the query once per press and disables the button while it runs', async () => {
    const user = userEvent.setup()
    const { rerender } = render(<SettingsPage />)

    await user.click(screen.getByRole('button', { name: 'Test the connection' }))
    expect(refetchMock).toHaveBeenCalledTimes(1)

    // While the request is in flight, the button says so and cannot be pressed again.
    connectivityMock.mockReturnValue(connectivityResult(undefined, { isFetching: true }))
    rerender(<SettingsPage />)
    expect(screen.getByRole('button', { name: 'Testing…' })).toBeDisabled()

    // Once the answer is in, the button is live again: a second press is a
    // second request, which the service may answer from its own 10 s window.
    connectivityMock.mockReturnValue(
      connectivityResult(payload([probe('solar_api_powerflow', 'ok')], 'ok', { cached: true })),
    )
    rerender(<SettingsPage />)
    await user.click(screen.getByRole('button', { name: 'Test the connection' }))
    expect(refetchMock).toHaveBeenCalledTimes(2)
  })

  it('reports two ok probes as an overall ok, with the test time', () => {
    connectivityMock.mockReturnValue(
      connectivityResult(
        payload(
          [
            probe('solar_api_powerflow', 'ok', { http_status: 200 }),
            probe('internal_version', 'ok', { http_status: 200, duration_ms: 63, detail: 'firmware 1.19.7-1' }),
          ],
          'ok',
        ),
      ),
    )

    render(<SettingsPage />)

    const rows = screen.getAllByTestId('connectivity-probe')
    expect(rows).toHaveLength(2)
    expect(within(rows[0]!).getByText('ok')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('200')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('41 ms')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('firmware 1.19.7-1')).toBeInTheDocument()

    const overall = screen.getByTestId('connectivity-overall')
    expect(overall).toHaveTextContent('ok')
    expect(overall).toHaveTextContent(formatDateTime(TESTED_AT))
  })

  it('renders an unauthorized probe with its reason and a partial verdict', () => {
    connectivityMock.mockReturnValue(
      connectivityResult(
        payload(
          [
            probe('solar_api_powerflow', 'ok', { http_status: 200 }),
            probe('internal_version', 'unauthorized', {
              http_status: 401,
              duration_ms: 63,
              reason: 'credentials rejected (401)',
            }),
          ],
          'partial',
        ),
      ),
    )

    render(<SettingsPage />)

    const rows = screen.getAllByTestId('connectivity-probe')
    const internal = rows[1]!
    expect(within(internal).getByText('unauthorized')).toBeInTheDocument()
    expect(within(internal).getByText('credentials rejected (401)')).toBeInTheDocument()
    expect(within(internal).getByText('401')).toBeInTheDocument()

    expect(screen.getByTestId('connectivity-overall')).toHaveTextContent('partial')
  })

  it('shows an internal probe skipped for lack of credentials as skipped, not as a failure', () => {
    connectivityMock.mockReturnValue(
      connectivityResult(
        payload(
          [
            probe('solar_api_powerflow', 'ok', { http_status: 200 }),
            probe('internal_version', 'skipped', { reason: 'no credentials configured' }),
          ],
          'ok',
          { credentials_configured: false },
        ),
      ),
    )

    render(<SettingsPage />)

    const rows = screen.getAllByTestId('connectivity-probe')
    expect(within(rows[1]!).getByText('skipped')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('no credentials configured')).toBeInTheDocument()
    expect(screen.getByTestId('connectivity-overall')).toHaveTextContent('ok')
    expect(screen.queryByText('failed')).not.toBeInTheDocument()
  })

  it('marks a reused result as cached', () => {
    connectivityMock.mockReturnValue(
      connectivityResult(payload([probe('solar_api_powerflow', 'ok')], 'ok', { cached: true })),
    )

    render(<SettingsPage />)

    expect(screen.getByText('(cached)')).toBeInTheDocument()
    expect(screen.getByText(formatDateTime(TESTED_AT))).toBeInTheDocument()
  })

  it('invents nothing before the first press', () => {
    render(<SettingsPage />)

    expect(screen.getByTestId('connectivity-empty')).toBeInTheDocument()
    expect(screen.getByText('No test has run yet.')).toBeInTheDocument()
    expect(screen.queryAllByTestId('connectivity-probe')).toHaveLength(0)
    expect(screen.queryByTestId('connectivity-overall')).not.toBeInTheDocument()
  })

  it('shows the alert with a retry when the query fails', async () => {
    const user = userEvent.setup()
    connectivityMock.mockReturnValue(
      connectivityResult(undefined, { isError: true, error: new Error('connectivity request failed') }),
    )

    render(<SettingsPage />)

    const alert = screen.getByTestId('api-error')
    expect(within(alert).getByText('connectivity request failed')).toBeInTheDocument()

    await user.click(within(alert).getByRole('button', { name: 'Retry' }))
    expect(refetchMock).toHaveBeenCalledTimes(1)
  })
})
