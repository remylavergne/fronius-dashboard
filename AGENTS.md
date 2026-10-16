# Instructions for the AI development agent

You are developing a local supervision application for a Fronius Primo GEN24 3.6 Plus, Go backend, React/TypeScript/shadcn/ui frontend. Read **all** the files in this folder before coding, starting with `README.md` and `TODO.md`.

## Workflow

1. Establish the delivery plan and the missing ADRs; propose assumptions rather than inventing them.
2. Implement **first** a Fronius simulator from anonymized fixtures; no hardware required for the tests.
3. Implement a read-only Solar API client and the normalized model; add internal Digest auth only if a metric justifies it.
4. Implement the persistence, the Go API, then the React frontend and the charts.
5. Write tests and documentation at each step; maintain `TODO.md` and a `CHANGELOG.md` log.
6. Before any interaction with the real inverter, explain precisely the planned requests and ask for confirmation. **GET only** by default.
7. Comments, documentation, interface and commits are in English.

## Requirements

- No dependency on the manufacturer's Angular in the produced application.
- Never assert that frontend permissions are firmware permissions.
- Never expose secrets to React nor record them in the repository.
- Do not compute consumption/import/export without a Smart Meter or other validated measurement.
- Commands that modify the inverter are out of MVP scope.
- Idiomatic Go code, strict TS, simple and testable architecture; lint, format, tests, CI.
- Any untested endpoint is explicitly `unverified`.
- Any new information must be documented with its source and its firmware version.

## Definition of Done

Tested feature (unit + integration), typed, documented, errors and empty states handled, accessibility verified, no secrets in the logs, compatible with offline data.

## Reference sources

The file `references/fronius-original.js` contains
the original JavaScript of the Fronius GEN24 interface.

It is the primary source for:

- Identifying the HTTP endpoints
- Analyzing the authentication mechanisms
- Understanding the permissions
- Identifying the data structures
- Reconstructing the existing features

The agent must consult this file before implementing
a feature based on the proprietary Fronius API.

Never modify this original file.
