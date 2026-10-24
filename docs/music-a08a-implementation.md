# Music A08a — optional plugin runtime boundary (`wj3`)

## Scope

Optional extension **manifest / API version / capability** contract and a
minimal **bounded WASM runtime** behind injected interfaces. Default music
playback does not require plugins. Admins enable/disable/status through a
permissioned API and a plain Admin › Plugins surface. No marketplace.

## UI design

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

After a server restart, the persisted enabled flag can precede lazy module
loading. Both UIs show this as enabled and ready for a self-check/probe, keeping
the declared check action available; they do not mislabel it as switched off.

### Native advanced server settings

Settings → Administration → **Server plugins** opens a separate native page.
The existing AppBar Back returns to Settings; music controls and source are
untouched. This replaces the earlier API-only Android scope: all three clients
must expose and exercise the feature.

The page uses themed Material cards, wrapping text/actions, 16 px page padding
and native buttons (48 px minimum touch targets), including at narrow widths
and large text. Each card stacks name/version, description, human status, and
**Permissions: Self-check only. No library, file or network access.** for the
current probe contract. **Run self-check** invokes probe and shows its result.
**Enable** and **Disable** require a named confirmation; Cancel/Back preserves
the state. Disabled plugins cannot run a check. Invalid manifests retain a
readable error and may retry Enable. No marketplace or installation form.

| Native state | Presentation / transition |
| --- | --- |
| Opening / refreshing | Loading label and spinner; actions unavailable |
| Empty | No server plugins installed; normal playback needs none |
| Active / disabled / error | Text status alongside name/version; error wraps |
| Offline / failed request | Connection explanation and Refresh; never queue or replay a mutation |
| Permission denied | Administrator access required; hide plugin rows/actions |
| Mutation in progress | All page actions disabled; refresh saved status after completion |
| Profile/account changed | Discard rows/results and pending confirmation; reject stale request epoch |

Refresh reloads authoritative status, including after a failed operation whose
server outcome may be unknown. Late responses cannot update a different
account/profile page. Shared Dio retains its auth and endpoint routing; POSTs
are never automatically retried. Light/dark, narrow, touch, Back, offline,
permission and playback acceptance require the actual installed APK.

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
