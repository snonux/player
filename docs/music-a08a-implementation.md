# Music A08a — optional plugin runtime boundary (`wj3`)

## Scope

Optional extension **manifest / API version / capability** contract and a
minimal **bounded WASM runtime** behind injected interfaces. Default music
playback does not require plugins. Admins enable/disable/status through a
permissioned API and a plain Admin › Plugins surface. No marketplace.

## UI design (admin web only)

Plugins stay out of routine music flows (library, Now Playing, queue).

| State | Presentation |
| --- | --- |
| Section collapsed | Admin modal button **Plugins** (`aria-expanded=false`) |
| Empty | “No plugins installed…” + short filesystem install note |
| Loading | Status line “Loading plugins…” (`aria-live=polite`) |
| Row OK disabled | Name, version, `disabled`, **Enable** |
| Row active | `active`, **Disable** (confirm), **Run probe** when `probe` declared |
| Row error | `error` + concise message; Enable may retry / stay refused |
| Permission | Admin session only; non-admin routes 403 |
| Narrow / themes | Reuses existing admin list + theme tokens; no new chrome |

Android has no admin plugin panel (same as A08d note); shared status is the
admin API. Full Chrome/Firefox/emulator journeys deferred to integrated
verification.

## Contract

Each `PLUGIN_DIR/<id>/` contains:

- `manifest.json` — `id`, `name`, `version`, `api_version` (must be `1`),
  `capabilities` (A08a: `probe`), `entry` (plain wasm file name)
- entry WASM exporting one i32 function per capability name

Host limits (defaults): 2 MiB wasm, 256 memory pages (16 MiB), 2 s call timeout.

## API

See `player-server/docs/api.md` admin plugins section.

## Layout

| Path | Role |
| --- | --- |
| `internal/plugin` | Manifest, Loader/Module interfaces, wazero Loader, Manager |
| `internal/service/plugin.go` | Admin PluginService |
| `internal/api/handlers_plugins.go` | Permissioned routes |
| Admin modal Plugins section | Enable/disable/status/probe |

## Verification (implementation slice)

Targeted Go tests for manifest validation, enable/invoke/disable, bad
`api_version`, call timeout, and admin handlers. Full browser/emulator E2E
deferred per implementation-first workflow.
