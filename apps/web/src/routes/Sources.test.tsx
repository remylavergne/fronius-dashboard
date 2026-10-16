import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { sourcesEnvelopeSchema } from '@/lib/schema'
import type { Capability, Source, SourceCatalogue, SourceSummary, SourceSurvey } from '@/lib/schema'
import { SourcesPage } from './Sources'

// `vi.mock` is hoisted above the imports: the two query results must live in a
// hoisted binding to be referenced by the factory.
const { sourcesMock, capabilitiesMock } = vi.hoisted(() => ({
  sourcesMock: vi.fn(),
  capabilitiesMock: vi.fn(),
}))

vi.mock('@/lib/queries', () => ({
  useSources: () => sourcesMock(),
  useCapabilities: () => capabilitiesMock(),
}))

const META = { generated_at: '2026-10-16T18:00:00Z' }

const PROBED_AT = '2026-10-16T18:00:00Z'

/** Distinct numbers: a counter can then be asserted without ambiguity. */
const summary: SourceSummary = {
  sources: 27,
  called: 5,
  captured: 2,
  candidates: 19,
  archived: 1,
  fields: 60,
  fields_displayed: 12,
  fields_stored: 6,
  fields_validated: 4,
  fields_ignored: 22,
  fields_unverified: 15,
}

/**
 * Two of the three sources below carry a probe, both `ok`: `probed` matches the
 * number of surveyed rows, since that is the invariant the backend keeps.
 */
const surveyCounters = {
  probed: 2,
  ok: 2,
  empty: 0,
  not_json: 0,
  auth_required: 0,
  not_found: 0,
  error: 0,
}

const survey: SourceSurvey = { ...surveyCounters, last_probed_at: PROBED_AT, firmware: '1.19.7-1' }

/** A survey that ran, on a device that announced no version. */
const surveyNoFirmware: SourceSurvey = { ...surveyCounters, last_probed_at: PROBED_AT }

/** Counters of a survey that never ran: no instant, so nothing to display. */
const surveyBare: SourceSurvey = { ...surveyCounters }

const capabilityRows: Capability[] = [
  {
    id: 'production.power',
    label: 'Instantaneous production',
    available: true,
    quality: 'measured',
    requires: [],
    read_only: true,
    verified: true,
    evidence: ['conversation'],
    reason: '',
  },
  {
    id: 'grid.import_export',
    label: 'Grid import / export',
    available: false,
    quality: 'missing',
    requires: ['smart_meter'],
    read_only: true,
    verified: false,
    evidence: ['conversation'],
    reason: 'No Fronius Smart Meter detected',
  },
]

/** The source the collector reads: the only one with a fixture, proofs and docs. */
const calledSource: Source = {
  id: 'solarapi.powerflow',
  transport: 'solar_api',
  path: '/solar_api/v1/GetPowerFlowRealtimeData.fcgi',
  method: 'GET',
  provides: 'Site power flow and the total energy: the only source the collector reads',
  state: 'called',
  called: true,
  fixture: 'test/fixtures/solarapi/powerflow_produce_only.json',
  capability: 'production.power',
  fields: [
    { path: 'Body.Data.Site.P_PV', type: 'float', status: 'displayed', capability: 'production.power' },
    { path: 'Body.Data.Site.E_Total', type: 'float', status: 'ignored', why: 'Derived from the day counter instead' },
    { path: 'Body.Data.Site.P_Akku', type: 'float', status: 'unverified', why: 'Never observed with a value' },
    { path: 'Body.Data.Site.P_Grid', type: 'float', status: 'stored', capability: 'grid.import_export' },
  ],
  proofs: [
    {
      tag: '[VERIFIED]',
      firmware: '1.19.7-1',
      detail: 'captured 2026-10-16: P_PV = 3200 W, E_Total = 15089627.58 Wh',
    },
  ],
  docs: ['docs/SOURCES.md § Shape of the Solar API v1 response'],
  note: '',
  // Surveyed and read: the probe is a fact about the device, not a reason to
  // read it, and it is not a candidate for anything either.
  probe: {
    class: 'ok',
    http_status: 200,
    digest_required: false,
    bytes: 883,
    probed_at: PROBED_AT,
  },
}

/** A captured shape with no inventory yet: every optional collection is absent. */
const bareSource: Source = {
  id: 'internalapi.status.common',
  transport: 'internal_api',
  path: '/status/common',
  method: 'GET',
  provides: 'Inverter model, firmware and the configured time zone',
  state: 'captured',
  called: false,
}

/** A candidate that would unlock a capability which does not exist yet. */
const candidateSource: Source = {
  id: 'internalapi.getservicestate',
  transport: 'internal_api',
  path: '/commands/GetServiceState',
  query: 'serviceId=<id>',
  method: 'GET',
  provides: 'Device state and the service state; it would unlock the inverter state capability',
  state: 'candidate',
  called: false,
  would_unlock: 'inverter.state',
  fields: [{ path: 'Body.Data.DeviceState', type: 'string', status: 'validated' }],
  proofs: [{ tag: '[SOURCE-JS]', detail: 'read by the manufacturer interface, never observed on this firmware' }],
  note: 'Kept as a candidate until a capture proves the shape.',
  // The case the survey exists for: the device answers the path with content,
  // and nothing in this project reads it. No probe firmware here, so the tests
  // of the proof's own missing firmware stay unambiguous.
  probe: {
    class: 'ok',
    http_status: 200,
    digest_required: false,
    bytes: 512,
    probed_at: PROBED_AT,
    fields: [
      { path: 'Body.Data.DeviceState', type: 'string' },
      { path: 'Body.Data.ServiceState', type: 'string', array_len: 2 },
    ],
  },
}

const catalogue: SourceCatalogue = { summary, survey, sources: [calledSource, bareSource, candidateSource] }

function mockPage(payload: SourceCatalogue = catalogue, rows: Capability[] = capabilityRows) {
  sourcesMock.mockReturnValue({
    data: { data: payload, meta: META },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
  capabilitiesMock.mockReturnValue({
    data: { data: rows, meta: META },
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: vi.fn(),
  })
}

/** Climb of the DOM for one source, by its accessible name (its address). */
function sourceBlock(address: string) {
  return screen.getByRole('article', { name: address })
}

describe('SourcesPage summary', () => {
  beforeEach(() => {
    mockPage()
  })

  it('renders the state and field counters of the payload', () => {
    render(<SourcesPage />)

    const strip = screen.getByRole('region', { name: 'Catalogue summary' })

    // The five state counters, each with its own number.
    expect(within(strip).getByText('Sources')).toBeInTheDocument()
    expect(within(strip).getByText('27')).toBeInTheDocument()
    expect(within(strip).getByText('called')).toBeInTheDocument()
    expect(within(strip).getByText('5')).toBeInTheDocument()
    expect(within(strip).getByText('captured')).toBeInTheDocument()
    expect(within(strip).getByText('2')).toBeInTheDocument()
    expect(within(strip).getByText('candidate')).toBeInTheDocument()
    expect(within(strip).getByText('19')).toBeInTheDocument()
    expect(within(strip).getByText('archived')).toBeInTheDocument()
    expect(within(strip).getByText('1')).toBeInTheDocument()

    // The field counters, including the two that are not the same thing.
    expect(within(strip).getByText('60')).toBeInTheDocument()
    expect(within(strip).getByText('displayed')).toBeInTheDocument()
    expect(within(strip).getByText('12')).toBeInTheDocument()
    expect(within(strip).getByText('stored')).toBeInTheDocument()
    expect(within(strip).getByText('6')).toBeInTheDocument()
    expect(within(strip).getByText('validated')).toBeInTheDocument()
    expect(within(strip).getByText('4')).toBeInTheDocument()
    expect(within(strip).getByText('ignored')).toBeInTheDocument()
    expect(within(strip).getByText('22')).toBeInTheDocument()
    expect(within(strip).getByText('unverified')).toBeInTheDocument()
    expect(within(strip).getByText('15')).toBeInTheDocument()
  })

  it('counts the sources it is given, not a recount of its own', () => {
    render(<SourcesPage />)

    expect(screen.getByText('Sources shown: 3 / 3')).toBeInTheDocument()
    expect(screen.getAllByRole('article')).toHaveLength(3)
  })
})

describe('SourcesPage survey summary', () => {
  beforeEach(() => {
    mockPage()
  })

  it('renders the survey counters, the firmware and the instant of the survey', () => {
    render(<SourcesPage />)

    const region = screen.getByRole('region', { name: 'Last survey' })

    // `probed` of the catalogued sources, from the payload's own counters.
    expect(within(region).getByText('Paths surveyed')).toBeInTheDocument()
    expect(within(region).getByText('2 / 27')).toBeInTheDocument()
    expect(within(region).getByText('Firmware at survey time')).toBeInTheDocument()
    expect(within(region).getByText('1.19.7-1')).toBeInTheDocument()
    expect(within(region).getByText('Surveyed at')).toBeInTheDocument()
    expect(within(region).getByText(/\/2026,/)).toBeInTheDocument()

    // The six class counters, each under the word of its class.
    for (const cls of ['ok', 'empty', 'not_json', 'auth_required', 'not_found', 'error']) {
      expect(within(region).getByText(cls)).toBeInTheDocument()
    }
    expect(within(region).getByText('2')).toBeInTheDocument()
    expect(within(region).getAllByText('0')).toHaveLength(5)
  })

  it('renders an unknown firmware as a dash, never as a fabricated version', () => {
    mockPage({ summary, survey: surveyNoFirmware, sources: [calledSource] })
    render(<SourcesPage />)

    const region = screen.getByRole('region', { name: 'Last survey' })

    expect(within(region).getByText('—')).toBeInTheDocument()
    expect(within(region).queryByText('1.19.7-1')).not.toBeInTheDocument()
  })

  it('says plainly that nothing was surveyed instead of showing zeros as results', () => {
    // No survey block at all: a backend that never ran one, or ran it on an
    // empty catalogue.
    mockPage({ summary, sources: [bareSource] })
    render(<SourcesPage />)

    const region = screen.getByRole('region', { name: 'Last survey' })

    expect(within(region).getByText(/No survey has been recorded yet/)).toBeInTheDocument()
    expect(within(region).queryByText('Paths surveyed')).not.toBeInTheDocument()
    expect(within(region).queryByText('0')).not.toBeInTheDocument()
    expect(within(region).queryByText('Surveyed at')).not.toBeInTheDocument()
  })

  it('reads a survey whose counters never ran as never surveyed', () => {
    // The contract's marker is `last_probed_at`; without it, the counters are a
    // row of zeros that would be a lie to display.
    mockPage({ summary, survey: surveyBare, sources: [bareSource] })
    render(<SourcesPage />)

    const region = screen.getByRole('region', { name: 'Last survey' })

    expect(within(region).getByText(/No survey has been recorded yet/)).toBeInTheDocument()
    expect(within(region).queryByText('ok')).not.toBeInTheDocument()
  })
})

describe('SourcesPage probe badge', () => {
  beforeEach(() => {
    mockPage()
  })

  it('shows the class, the HTTP status, the byte count and the instant of a surveyed source', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')

    expect(within(block).getByText('ok')).toHaveClass('bg-primary')
    expect(within(block).getByText('HTTP:')).toBeInTheDocument()
    expect(within(block).getByText('200')).toBeInTheDocument()
    expect(within(block).getByText('Bytes:')).toBeInTheDocument()
    expect(within(block).getByText('883')).toBeInTheDocument()
    expect(within(block).getByText('Probed at:')).toBeInTheDocument()
    // The instant, not the proof detail that also carries the word 2026.
    expect(within(block).getByText(/\/2026,/)).toBeInTheDocument()
    // The state badge is still there, beside it: two axes, two badges.
    expect(within(block).getByText('called')).toBeInTheDocument()
    expect(within(block).getByText('read by this project')).toBeInTheDocument()
  })

  it('names the firmware observed on the path when the survey recorded one', () => {
    mockPage({
      summary,
      survey,
      sources: [
        {
          ...bareSource,
          probe: {
            class: 'empty',
            http_status: 200,
            digest_required: false,
            bytes: 42,
            firmware: '1.19.7-1',
            probed_at: PROBED_AT,
          },
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/status/common')

    expect(within(block).getByText('empty')).toBeInTheDocument()
    expect(within(block).getByText('Firmware:')).toBeInTheDocument()
    expect(within(block).getByText('1.19.7-1')).toBeInTheDocument()
    expect(within(block).getByText('42')).toBeInTheDocument()
  })

  it('marks a path that answered without a credential as needing authentication', () => {
    mockPage({
      summary,
      survey,
      sources: [
        {
          ...bareSource,
          path: '/status/version',
          probe: {
            class: 'auth_required',
            http_status: 401,
            digest_required: true,
            bytes: 0,
            probed_at: PROBED_AT,
          },
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/status/version')

    expect(within(block).getByText('auth_required')).toHaveClass('border-dashed')
    expect(within(block).getByText('needs authentication')).toBeInTheDocument()
    expect(within(block).getByText('401')).toBeInTheDocument()
  })

  it('carries the message of a path that answered an error', () => {
    mockPage({
      summary,
      survey,
      sources: [
        {
          ...bareSource,
          path: '/status/emrs',
          probe: {
            class: 'error',
            http_status: 0,
            digest_required: false,
            bytes: 0,
            error: 'connection refused',
            probed_at: PROBED_AT,
          },
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/status/emrs')

    expect(within(block).getByText('error')).toHaveClass('bg-destructive/10')
    expect(within(block).getByText('Error:')).toBeInTheDocument()
    expect(within(block).getByText('connection refused')).toBeInTheDocument()
    // An error never needs a credential marker: nothing answered 401.
    expect(within(block).queryByText('needs authentication')).not.toBeInTheDocument()
  })

  it('gives each class its own colour, the badge carrying the word anyway', () => {
    const classes = ['ok', 'empty', 'not_json', 'auth_required', 'not_found', 'error']
    mockPage({
      summary,
      survey,
      sources: classes.map((cls, index) => ({
        ...bareSource,
        id: `probe.${cls}`,
        path: `/probe/${cls}`,
        probe: {
          class: cls,
          http_status: 200,
          digest_required: false,
          bytes: index,
          probed_at: PROBED_AT,
        },
      })),
    })
    render(<SourcesPage />)

    const badges = classes.map((cls) => within(sourceBlock(`/probe/${cls}`)).getByText(cls))

    expect(new Set(badges.map((badge) => badge.className)).size).toBe(classes.length)
  })

  it('renders a class it does not know as the token it arrived with', () => {
    mockPage({
      summary,
      survey,
      sources: [
        {
          ...bareSource,
          probe: {
            class: 'throttled',
            http_status: 429,
            digest_required: false,
            bytes: 12,
            probed_at: PROBED_AT,
          },
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/status/common')

    expect(within(block).getByText('throttled')).toBeInTheDocument()
    expect(within(block).getByText('429')).toBeInTheDocument()
  })

  it('renders a source the survey never looked at as never surveyed', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/status/common')

    expect(within(block).getByText('never surveyed')).toHaveClass('border-dashed')
    // Never surveyed and observed nothing are not the same thing: no detail row
    // is rendered at all here.
    expect(within(block).queryByText('HTTP:')).not.toBeInTheDocument()
    expect(within(block).queryByText('Bytes:')).not.toBeInTheDocument()
    expect(within(block).queryByText('Probed at:')).not.toBeInTheDocument()
  })
})

describe('SourcesPage observed shape', () => {
  beforeEach(() => {
    mockPage()
  })

  it('renders the shape of the answer as a second table, with types and array lengths', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')
    const table = within(block).getByRole('table', {
      name: 'Observed shape — /commands/GetServiceState?serviceId=<id>',
    })

    expect(within(table).getByRole('columnheader', { name: 'Path' })).toBeInTheDocument()
    expect(within(table).getByRole('columnheader', { name: 'Type' })).toBeInTheDocument()
    expect(within(table).getByText('Body.Data.DeviceState')).toBeInTheDocument()
    expect(within(table).getByText('string')).toBeInTheDocument()
    // The array length is rendered as [N], and only where the survey saw one.
    expect(within(table).getByText('string [2]')).toBeInTheDocument()
    expect(within(block).getByText('The survey records the shape of the answer — never its values.')).toBeInTheDocument()
  })

  it('leaves the catalogue inventory a table of its own, not merged with the shape', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')
    // The shape is always readable; the catalogue inventory stays disclosed.
    expect(within(block).getAllByRole('table')).toHaveLength(1)

    await user.click(within(block).getByRole('button', { name: 'Field inventory (1)' }))

    expect(within(block).getAllByRole('table')).toHaveLength(2)
    const inventory = within(block).getByRole('table', {
      name: 'Field inventory — /commands/GetServiceState?serviceId=<id>',
    })
    expect(within(inventory).getByRole('columnheader', { name: 'Status' })).toBeInTheDocument()
    expect(within(inventory).getByText('validated')).toBeInTheDocument()
  })

  it('renders no shape table when the survey carried no field', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')

    expect(within(block).queryByText('Observed shape')).not.toBeInTheDocument()
    expect(within(block).queryByRole('table')).not.toBeInTheDocument()
  })
})

describe('SourcesPage available and unmapped', () => {
  beforeEach(() => {
    mockPage()
  })

  it('marks a path the device serves and nothing here reads', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')

    expect(within(block).getByText('available and unmapped')).toBeInTheDocument()
    expect(within(block).getByText(/the survey just proved it is a candidate/)).toBeInTheDocument()
  })

  it('does not mark a path the device serves and this project reads', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')

    expect(within(block).getByText('ok')).toBeInTheDocument()
    expect(within(block).queryByText('available and unmapped')).not.toBeInTheDocument()
  })

  it('does not mark a path this firmware does not serve', () => {
    mockPage({
      summary,
      survey,
      sources: [
        {
          ...candidateSource,
          fields: [],
          proofs: [],
          note: '',
          probe: {
            class: 'not_found',
            http_status: 404,
            digest_required: false,
            bytes: 0,
            probed_at: PROBED_AT,
          },
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')

    expect(within(block).getByText('not_found')).toBeInTheDocument()
    expect(within(block).queryByText('available and unmapped')).not.toBeInTheDocument()
  })
})

describe('SourcesPage source blocks', () => {
  beforeEach(() => {
    mockPage()
  })

  it('renders the address, the state, the called marker and the provenance', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')

    expect(within(block).getByText('called')).toBeInTheDocument()
    expect(within(block).getByText('read by this project')).toBeInTheDocument()
    expect(within(block).getByText('Solar API v1')).toBeInTheDocument()
    expect(within(block).getByText('solarapi.powerflow')).toBeInTheDocument()
    expect(within(block).getByText('test/fixtures/solarapi/powerflow_produce_only.json')).toBeInTheDocument()
    expect(within(block).getByText('[VERIFIED]')).toBeInTheDocument()
    expect(within(block).getByText('1.19.7-1')).toBeInTheDocument()
    expect(
      within(block).getByText('captured 2026-10-16: P_PV = 3200 W, E_Total = 15089627.58 Wh'),
    ).toBeInTheDocument()
    expect(within(block).getByText('docs/SOURCES.md § Shape of the Solar API v1 response')).toBeInTheDocument()
    // Two of the four fields are ignored or unverified.
    expect(within(block).getByText('Fields ignored or unverified: 2 / 4')).toBeInTheDocument()
  })

  it('gives a distinct colour to each state, the badge carrying the word anyway', () => {
    render(<SourcesPage />)

    expect(within(sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')).getByText('called')).toHaveClass('bg-primary')
    expect(within(sourceBlock('/status/common')).getByText('captured')).toHaveClass('bg-chart-2')
    expect(within(sourceBlock('/commands/GetServiceState?serviceId=<id>')).getByText('candidate')).toHaveClass('bg-background')
  })

  it('appends the query to the address of a source that takes one', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')

    expect(within(block).getByText('Kept as a candidate until a capture proves the shape.')).toBeInTheDocument()
    // No firmware on this proof: nothing is invented for it. Asserting on the
    // version itself, not on the word "firmware" — the state's own definition
    // legitimately contains "never observed on this firmware".
    expect(within(block).queryByText('1.19.7-1')).not.toBeInTheDocument()
  })

  it('joins a capability that exists, with its reason when it is unavailable', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')
    // The source-level chip and the chip of the field that feeds the same
    // capability: both resolve through /api/v1/capabilities to the same label.
    expect(within(block).getAllByText('Instantaneous production')).toHaveLength(2)

    await user.click(within(block).getByRole('button', { name: 'Field inventory (4)' }))

    const table = within(block).getByRole('table')
    expect(within(table).getByText('Grid import / export')).toBeInTheDocument()
    expect(within(table).getByText('unavailable: No Fronius Smart Meter detected')).toBeInTheDocument()
  })

  it('shows a would_unlock id as a candidate, never as a capability', () => {
    render(<SourcesPage />)

    const block = sourceBlock('/commands/GetServiceState?serviceId=<id>')
    // A candidate id has no row in /api/v1/capabilities: it is not joined.
    expect(within(block).getByText(/Candidate capability/)).toBeInTheDocument()
    expect(within(block).getByText('inverter.state')).toBeInTheDocument()
    expect(within(block).queryByText('Instantaneous production')).not.toBeInTheDocument()

    expect(within(sourceBlock('/status/common')).queryByText(/Candidate capability/)).not.toBeInTheDocument()
  })

  it('renders a source whose fields, proofs, docs, fixture and note are absent', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    const block = sourceBlock('/status/common')

    expect(within(block).getByText('captured')).toBeInTheDocument()
    expect(within(block).getByText('Internal API')).toBeInTheDocument()
    expect(within(block).getByText('Inverter model, firmware and the configured time zone')).toBeInTheDocument()
    expect(within(block).queryByText('read by this project')).not.toBeInTheDocument()
    expect(within(block).getByText('Fields ignored or unverified: 0 / 0')).toBeInTheDocument()
    // Absent, not empty: no label is rendered for what the payload does not carry.
    expect(within(block).queryByText(/Captured fixture/)).not.toBeInTheDocument()
    expect(within(block).queryByText('Proofs')).not.toBeInTheDocument()
    expect(within(block).queryByText('References')).not.toBeInTheDocument()
    expect(within(block).queryByText(/Note:/)).not.toBeInTheDocument()

    // The inventory is still openable, and says so rather than showing nothing.
    expect(within(block).queryByRole('table')).not.toBeInTheDocument()
    await user.click(within(block).getByRole('button', { name: 'Field inventory (0)' }))
    expect(within(block).getByText('No field is inventoried for this source.')).toBeInTheDocument()
  })
})

describe('SourcesPage field inventory', () => {
  beforeEach(() => {
    mockPage()
  })

  it('expands on a keyboard-reachable control with aria-expanded and aria-controls', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')
    const toggle = within(block).getByRole('button', { name: 'Field inventory (4)' })
    const controls = toggle.getAttribute('aria-controls')

    expect(toggle.tagName).toBe('BUTTON')
    toggle.focus()
    expect(toggle).toHaveFocus()
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(controls).not.toBeNull()
    expect(document.getElementById(controls ?? '')).not.toBeNull()
    expect(within(block).queryByRole('table')).not.toBeInTheDocument()

    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const table = within(block).getByRole('table')
    expect(within(table).getByRole('columnheader', { name: 'Field' })).toBeInTheDocument()
    expect(within(table).getAllByRole('row')).toHaveLength(5)
    expect(within(table).getByText('Body.Data.Site.P_PV')).toBeInTheDocument()
    expect(within(table).getAllByText('float')).toHaveLength(4)
    expect(within(table).getByText('Derived from the day counter instead')).toBeInTheDocument()
  })

  it('labels every status it knows, and renders one it does not as it arrived', async () => {
    const user = userEvent.setup()
    mockPage({
      summary,
      sources: [
        {
          ...calledSource,
          fields: [
            { path: 'Body.Data.Site.P_PV', type: 'float', status: 'displayed' },
            { path: 'Body.Data.Site.E_Total', type: 'float', status: 'ignored', why: 'Unused' },
            { path: 'Body.Data.Site.P_Akku', type: 'float', status: 'unverified', why: 'Never observed' },
            { path: 'Body.Data.Site.X', type: 'int', status: 'proposed' },
          ],
        },
      ],
    })
    render(<SourcesPage />)

    const block = sourceBlock('/solar_api/v1/GetPowerFlowRealtimeData.fcgi')
    await user.click(within(block).getByRole('button', { name: 'Field inventory (4)' }))

    const table = within(block).getByRole('table')
    expect(within(table).getByText('displayed')).toBeInTheDocument()
    expect(within(table).getByText('ignored')).toBeInTheDocument()
    expect(within(table).getByText('unverified')).toBeInTheDocument()
    // An unknown status is shown as its raw token, and does not fail the page.
    expect(within(table).getByText('proposed')).toBeInTheDocument()
  })
})

describe('SourcesPage filters', () => {
  beforeEach(() => {
    mockPage()
  })

  it('narrows the list by state', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    await user.selectOptions(screen.getByLabelText('State'), 'candidate')

    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/commands/GetServiceState?serviceId=<id>' })).toBeInTheDocument()
    expect(screen.queryByRole('article', { name: '/status/common' })).not.toBeInTheDocument()
  })

  it('narrows the list by transport', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    await user.selectOptions(screen.getByLabelText('Transport'), 'internal_api')

    expect(screen.getByText('Sources shown: 2 / 3')).toBeInTheDocument()
    expect(screen.queryByRole('article', { name: '/solar_api/v1/GetPowerFlowRealtimeData.fcgi' })).not.toBeInTheDocument()
  })

  it('matches the path, what the source provides and the path of its fields', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)
    const search = screen.getByLabelText('Search')

    await user.type(search, 'PowerFlow')
    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/solar_api/v1/GetPowerFlowRealtimeData.fcgi' })).toBeInTheDocument()

    await user.clear(search)
    await user.type(search, 'inverter state')
    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/commands/GetServiceState?serviceId=<id>' })).toBeInTheDocument()

    await user.clear(search)
    await user.type(search, 'P_Akku')
    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/solar_api/v1/GetPowerFlowRealtimeData.fcgi' })).toBeInTheDocument()

    await user.clear(search)
    await user.type(search, 'nothing answers this')
    expect(screen.getByText('Sources shown: 0 / 3')).toBeInTheDocument()
    expect(screen.getByText('No source matches these filters')).toBeInTheDocument()
  })

  it('narrows the list by survey outcome, keeping the visible count honest', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    const filter = screen.getByLabelText('Survey')

    // Two of the three sources carry an `ok` probe.
    await user.selectOptions(filter, 'ok')
    expect(screen.getByText('Sources shown: 2 / 3')).toBeInTheDocument()
    expect(screen.queryByRole('article', { name: '/status/common' })).not.toBeInTheDocument()

    await user.selectOptions(filter, 'not_found')
    expect(screen.getByText('Sources shown: 0 / 3')).toBeInTheDocument()
    expect(screen.queryAllByRole('article')).toHaveLength(0)

    await user.selectOptions(filter, 'not_surveyed')
    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/status/common' })).toBeInTheDocument()

    await user.selectOptions(filter, 'surveyed')
    expect(screen.getByText('Sources shown: 2 / 3')).toBeInTheDocument()

    await user.selectOptions(filter, 'all')
    expect(screen.getByText('Sources shown: 3 / 3')).toBeInTheDocument()
  })

  it('combines the survey filter with the transport one', async () => {
    const user = userEvent.setup()
    render(<SourcesPage />)

    await user.selectOptions(screen.getByLabelText('Survey'), 'ok')
    await user.selectOptions(screen.getByLabelText('Transport'), 'internal_api')

    expect(screen.getByText('Sources shown: 1 / 3')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: '/commands/GetServiceState?serviceId=<id>' })).toBeInTheDocument()
  })
})

describe('SourcesPage legend', () => {
  beforeEach(() => {
    mockPage()
  })

  it('defines the four source states and the five field statuses', () => {
    render(<SourcesPage />)

    const legend = screen.getByRole('region', { name: 'Legend' })

    for (const state of ['called', 'captured', 'candidate', 'archived']) {
      expect(within(legend).getByText(state)).toBeInTheDocument()
    }
    for (const status of ['displayed', 'stored', 'validated', 'ignored', 'unverified']) {
      expect(within(legend).getByText(status)).toBeInTheDocument()
    }

    // `ignored` is a decision, `unverified` an absence of evidence: the legend
    // says so, and the two badges do not look the same.
    expect(within(legend).getByText('present in the answer and deliberately not used')).toBeInTheDocument()
    expect(within(legend).getByText('never observed carrying a value on the reference installation')).toBeInTheDocument()
    expect(within(legend).getByText('ignored')).toHaveClass('bg-muted')
    expect(within(legend).getByText('unverified')).toHaveClass('border-dashed')
  })

  it('defines the six survey outcomes and the never-surveyed case', () => {
    render(<SourcesPage />)

    const legend = screen.getByRole('region', { name: 'Legend' })

    for (const cls of ['ok', 'empty', 'not_json', 'auth_required', 'not_found', 'error']) {
      expect(within(legend).getByText(cls)).toBeInTheDocument()
    }
    // The seventh line is not a class: it is the absence of the question.
    expect(within(legend).getByText('never surveyed')).toHaveClass('border-dashed')
    expect(
      within(legend).getByText('not surveyed on this firmware — different from every outcome above, including not_found'),
    ).toBeInTheDocument()

    // Each class carries its meaning, and its own colour.
    expect(within(legend).getByText('ok')).toHaveClass('bg-primary')
    expect(within(legend).getByText('error')).toHaveClass('bg-destructive/10')
    expect(
      within(legend).getByText('answered with nothing: the endpoint exists and the component is not installed — a fact, not a failure'),
    ).toBeInTheDocument()
    expect(
      within(legend).getByText('answered 401 or 403 without credentials: the path exists behind authentication'),
    ).toBeInTheDocument()
  })
})

describe('sourcesEnvelopeSchema', () => {
  it('accepts the payload of the contract', () => {
    const result = sourcesEnvelopeSchema.safeParse({ data: catalogue, meta: META })
    expect(result.success).toBe(true)
  })

  it('tolerates a source without its optional keys', () => {
    const result = sourcesEnvelopeSchema.safeParse({ data: { summary, sources: [bareSource] }, meta: META })
    expect(result.success).toBe(true)
  })

  it('tolerates null collections, as a nil Go slice marshals to null', () => {
    const result = sourcesEnvelopeSchema.safeParse({
      data: { summary, sources: [{ ...bareSource, fields: null, proofs: null, docs: null }] },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('ignores unknown keys, so an enriched backend does not break the page', () => {
    const result = sourcesEnvelopeSchema.safeParse({
      data: { summary: { ...summary, fields_archived: 3 }, sources: [{ ...bareSource, transport_hint: 'digest' }] },
      meta: META,
    })
    expect(result.success).toBe(true)
  })

  it('rejects a source that cannot be identified', () => {
    const withoutState = {
      id: 'x',
      transport: 'solar_api',
      path: '/p',
      method: 'GET',
      provides: '',
      called: false,
    }
    expect(sourcesEnvelopeSchema.safeParse({ data: { summary, sources: [withoutState] }, meta: META }).success).toBe(false)
  })

  it('accepts a probe with its shape, its array lengths and its firmware', () => {
    const result = sourcesEnvelopeSchema.safeParse({
      data: {
        summary,
        survey,
        sources: [
          {
            ...bareSource,
            probe: {
              class: 'ok',
              http_status: 200,
              digest_required: true,
              bytes: 883,
              error: '',
              firmware: '1.19.7-1',
              probed_at: PROBED_AT,
              fields: [
                { path: 'Body.Data.DeviceStatus.InverterState', type: 'string', array_len: 0 },
                { path: 'Body.Data.Relay', type: 'object', array_len: 2 },
              ],
            },
          },
        ],
      },
      meta: META,
    })

    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.data.sources[0]?.probe?.fields?.[1]?.array_len).toBe(2)
    }
  })

  it('tolerates a probe without fields, without firmware and with an unknown class', () => {
    const result = sourcesEnvelopeSchema.safeParse({
      data: {
        summary,
        survey: { ...survey, last_probed_at: undefined, firmware: undefined },
        sources: [
          {
            ...bareSource,
            probe: {
              class: 'throttled',
              http_status: 429,
              digest_required: false,
              bytes: 12,
              probed_at: PROBED_AT,
            },
          },
        ],
      },
      meta: META,
    })

    expect(result.success).toBe(true)
  })

  it('tolerates an envelope with no survey at all, probe by probe', () => {
    const result = sourcesEnvelopeSchema.safeParse({ data: { summary, sources: [bareSource] }, meta: META })

    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.data.survey).toBeUndefined()
      expect(result.data.data.sources[0]?.probe).toBeUndefined()
    }
  })

  it('rejects a probe whose instant is not a timestamp', () => {
    const result = sourcesEnvelopeSchema.safeParse({
      data: {
        summary,
        sources: [
          {
            ...bareSource,
            probe: {
              class: 'ok',
              http_status: 200,
              digest_required: false,
              bytes: 1,
              probed_at: 'yesterday',
            },
          },
        ],
      },
      meta: META,
    })

    expect(result.success).toBe(false)
  })
})
