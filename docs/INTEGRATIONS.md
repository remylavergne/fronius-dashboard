# Third-party integrations

How to consume the REST API from outside the bundled interface. Every
integration below reads `GET /api/v1/live` (the latest sample; its exact
shape is in the [OpenAPI contract](../api/openapi.json)) — no integration
ever receives the inverter credentials, and the API itself has none, so
keep it inside the LAN (ADR 0005, `docs/adr/0005-authentication-and-remote-access.md`).

## gethomepage/homepage

[Homepage](https://gethomepage.dev) dashboards read the API through the stock
`customapi` widget: Homepage fetches the JSON **server-side** (no CORS, no
browser-side call), so nothing has to be rebuilt or forked. Two ready-made
configurations live in [`contrib/homepage/`](../contrib/homepage/):

| File                  | Placement in Homepage                                                | Shows                                                      |
| --------------------- | -------------------------------------------------------------------- | ---------------------------------------------------------- |
| `info-widget.yaml`    | `config/widgets.yaml` (top bar)                                      | PV power, day energy                                       |
| `service-widget.yaml` | `config/services.yaml`, as the `widget:` block of the froniusd entry | state, PV power, day/year energies, last sample, freshness |

### Which URL to use

The widget url must be reachable **from the Homepage server**, not from the
browser:

| Homepage runs as                                        | widget url                          |
| ------------------------------------------------------- | ----------------------------------- |
| on the NAS host network (typical Synology install)      | `http://127.0.0.1:8080/api/v1/live` |
| inside the `fronius-monitoring` compose project network | `http://froniusd:8080/api/v1/live`  |

The second is the clean Docker topology: add Homepage's container to the
external network `fronius-monitoring_default`
(`docker network connect fronius-monitoring_default <homepage-container>`,
or the equivalent compose `networks:` entry). The published port of froniusd
stays on `127.0.0.1:8080` either way — the container already binds
`0.0.0.0:8080` internally (`Dockerfile`), so a container-to-container call
does not need `FRONIUS_ALLOW_REMOTE` widened nor a public port.

### Null values

Several `/live` fields are null on installations without the matching
hardware: `power_grid_w`/`power_load_w`/`power_battery_w` need a Smart Meter
or battery, and the firmware of some installations does not publish
`energy_day_wh` (`docs/UNKNOWNS.md` #13, the derived value sits in
`energy_derived.day_wh` instead). A `customapi` mapping whose field is null
renders an empty cell — the provided files therefore comment out the
meter-dependent lines, and `info-widget.yaml` carries a commented derived
variant to switch to when the measured day energy never appears.

### Checked

- Both YAML files parse (`Bun.YAML.parse`).
- Every mapped `field` resolves against a live `/api/v1/live` capture taken
  from the simulator on 2026-10-18 (`data.mode = "produce-only"`,
  `data.power_pv_w = 0`, `data.energy_day_wh = 24595.4`, `meta.stale = false`,
  `data.energy_derived.day_wh = null` — the null case renders as an empty cell).
- The Homepage side of `remap`/`scale`/`suffix` follows the published
  [customapi service widget](https://gethomepage.dev/widgets/services/customapi/)
  and [customapi info widget](https://gethomepage.dev/widgets/info/customapi/)
  schemas.
- **Not yet checked against a running Homepage instance** — the shapes come
  from the published schema and the live JSON, not from a rendered card.
