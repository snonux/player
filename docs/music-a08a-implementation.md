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
| `player-android/lib/api/server_plugins_api.dart` | Small optional admin client using shared authenticated Dio and credential epochs |
| `player-android/lib/screens/server_plugins_screen.dart` | Native advanced settings, confirmations, status and self-check |

## Verification

Integrated acceptance on 2026-10-24–25 used the isolated local server
on port 18771. The shared server on 18766 and the running emulator were not
restarted. The server executable identifies as `c7b6aea-dirty` (unchanged Go
implementation); web assets include the reviewed restart-control fix. Final
native release APK: source `347e502`, version 0.8.2 / code 4022, SHA-256
`f47f89fa594097b3df556d1205e7d032190c0e6a8c4cf4a0e03229c938625b6c`.

| Actual journey | Chrome 144.0.7559.132 | Firefox 156.0 | Native Android 14 / API 34 |
| --- | --- | --- | --- |
| Installed names/version, status and declared check | Pass | Pass | Pass; human permission descriptions |
| Enable, probe/self-check result 42, disable and cancellation | Pass; keyboard and pointer | Pass; keyboard and pointer | Pass; touch, Cancel and Android Back |
| Unsupported API version, loop, 257-page memory, 2 MiB + 1 byte module rejection | Pass | Pass | Pass |
| Core music during changes/errors | Real media 46 and captured 440 Hz audio | Real media 46 and captured 440 Hz audio | Real M68 Pattern source and captured QEMU 440 Hz audio |
| Persisted enabled flag after server restart | Pass; declared probe remains available | Pass; declared probe remains available | Pass; ready label and real lazy self-check |
| Offline/reconnect and non-admin boundaries | Pass; API permission matrix also checked | Pass; API permission matrix also checked | Pass; failed confirmed offline write was not replayed on reconnect; non-admin Settings hides entry |
| Narrow light/dark and navigation/focus | Pass | Pass | Pass; labeled 48 dp controls, Back and explicit cold unlock |
| Normal configuration returns `[]` and shows empty state | Pass | Pass | Pass; fresh music also played without installed plugins |

The emulator is `Player_FDroid_Test_API34`, x86_64, emulator 33.1.24.0
(build 11237101). The lazy-restart native check was first proved on `7cc40f7`;
the subsequent Back-only fix did not change that path. All confirmation,
resource, offline, permission, cold-unlock and empty-state checks passed on the
replacement `347e502` APK. Native semantic labels/bounds were inspected;
TalkBack itself was not driven.

Two confirmed acceptance defects were fixed: web hid the probe action for a
persisted enabled module awaiting lazy loading, and native Android Back could
leave the confirmation action locked after a late closing-dialog rebuild.
Explicit route ownership fixes the latter; its regression test fails on the old
code and passes on the fix. The suspected web focus issue was disproved in both
browsers: the existing modal observer restores the replacement control.

The loop fixture hits its **enable-time probe bound of about 500 ms**; this is
distinct from the default 2 s invocation bound. The parent API matrix passed 38
cases, including both aliases, admin/viewer/anonymous access, real result 42,
disabled/undeclared/missing cases and resource errors while health and
byte-exact media Range responses remained available.

Focused verification: 11 native widget/client cases, targeted analysis and the
web plugin restart regression passed. Final full Flutter analysis and all 1696
tests passed; the full web JS suite passed before the Back-only native change.
The existing Go race/coverage/build evidence is reused because Go was unchanged.

Evidence and exact commands remain outside git:

- `/home/paul/tmp/wj3-ui/`: installed-browser scripts, `final-browsers.log`,
  `normal-empty.log`, per-browser status/focus records, screenshots and PCM;
  `restart-before-fix.json` preserves the actual missing-control proof.
- `/home/paul/tmp/wj3-native/`: installed-build provenance, native scripts and
  stage logs, `android/results.json`, screenshots/semantic trees and PCM;
  `back-late-rebuild-before.log` preserves the failing regression and
  `dialog-after-fix.log` the actual Cancel/Back reopening proof.
- `/home/paul/tmp/wj3-evidence/`: API matrix and normal-server restoration proof.

Native acceptance is consolidated across the retained stage logs, not claimed
as one clean run. Early driver assumptions are retained. One size-limit PCM
capture estimated 452.1 Hz and failed the unchanged 440 ± 10 Hz check; its WAV
and analysis remain as `size-limit-enable-attempt1.*`. Repeating only that case
passed with the same analyser and tolerance.

Restoration passed through public queue CAS and native Continue/UI: all four
preexisting scoped queues and appearance/loudness values match their original
snapshots exactly, no extra queue scope remained, the original 18766/admin
account is selected with nonplaying transport, OS night mode and reverse
mappings match, and the original snapshots themselves remained unchanged.
`android/restoration.json` records those comparisons. The parent restored the
owned 18771 server to its normal configuration without `PLUGIN_DIR`.
