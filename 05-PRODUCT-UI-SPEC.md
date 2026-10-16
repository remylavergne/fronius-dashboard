# Product / screen specification

## Screen 1 — Overview
- KPIs: instantaneous production (W/kW), energy of the day (kWh), total energy if exposed, inverter state, last refresh.
- PV power curve over 24 h, zoom/pan, tooltips with units, 24 h / 7 d / 30 d / custom selector.
- Energy breakdown **only if measurements available**; without a Smart Meter, hide household consumption, grid import/export and real self-consumption.
- Connection state and "stale data" badge.

## Screen 2 — Production & history
- W curves, kWh aggregates per day/week/month/year, period comparisons, production calendar, CSV export.
- Handling of missing data (visible gaps, no silent interpolation), summer/winter time, adaptive granularity.

## Screen 3 — Inverter
- Model, serial (masked in sharing mode), firmware, WebUI, connectivity, operational states, available diagnostics.
- DC/AC voltages/currents, frequency and temperatures **only if exposed and confirmed**.

## Screen 4 — Events and health
- Alert/state history, severity filters, period, acknowledgement **local only** at MVP, export.
- Collector state, last successes/failures, latency and missing-data rate.

## Screen 5 — Settings
- Inverter address, collection interval, retention, language, time zone, units, database backup/restore.
- Test connectivity in read-only mode; secure management of secrets.

## Screen 6 — Capabilities and control (later phase)
- Display verified and unverified rights, read-only mode, explanation of unavailable features.
- Write buttons only after authorization, review and dedicated tests; confirmation and audit log.

## Design system
- Desktop side navigation / mobile menu, shadcn cards, readable typography, AA contrast, skeletons, tooltips, legends, accessible palette.
- Recharts charts (line/area/bar), no pie chart without relevant data, PNG/CSV export if requested.
- Clickable tiles to details, period shortcuts, display of units and measurement sources.

## MVP acceptance criteria
- Real-time display on local network with documented latency and frequency.
- 24 h curve persistent after restart; empty periods handled cleanly.
- No invented consumption value; no password exposed to the browser.
- Works with inverter offline (clear message and preserved history).
- Responsive and keyboard accessible; automated UI + API tests.
