# Scenario end-to-end runner

All 25 Markdown scenarios have dedicated Playwright implementations in
`../e2e-web/tests/scenario-S*.test.ts`. The runner maps IDs explicitly; it does
not interpret Markdown or count generic smoke tests as scenario coverage.
API assertions use real HTTP, browser checks use Chromium, database assertions
use SQLite, and RSS downloads use real MP3 bytes from the local fixtures.
Optional historical LLM screenshot oracles are not part of the pass criteria.

## Repeatable isolated setup

Prerequisites: Go, Node.js 18+, npm, ffmpeg/ffprobe, sqlite3, and Playwright
Chromium. From `player-server/`:

```sh
go build -o /tmp/player-scenarios ./cmd/player
cd test/e2e-web
npm ci
npm run install-browsers
cd ../e2e-llm/runner
npm ci
npm test
PLAYER_SCENARIO_BINARY=/tmp/player-scenarios node dist/index.js
```

Each test starts and stops its own Player process at `http://127.0.0.1:18081`,
with a fresh SQLite database and a copy of `testdata/media` under a unique
`/tmp/player-scenario-*` directory. It creates fixture artwork and an empty
set, sets the upload limit to 1 MiB, and bootstraps `admin` / `TestPassw0rd!`.
S01 performs bootstrap through the browser instead. Teardown removes the
entire sandbox even after assertion failures. S02, S17 and S23 also start a
local RSS fixture on an ephemeral port and stop it afterward. No production
service, database, or source media is mutated.

Do **not** start a server yourself. `PLAYER_URL` may change the unused local
HTTP origin (with an explicit port), but external/running servers are rejected.
Only one scenario worker can use a port at a time. `PLAYER_DB`, existing admin
credentials, and external RSS services are not used by this managed fixture.
Build the binary again after server or embedded web changes.

Run one scenario through the runner:

```sh
PLAYER_SCENARIO_BINARY=/tmp/player-scenarios node dist/index.js ../scenarios/S01-bootstrap.md
```

During development, avoid creating tracked tasks by invoking Playwright directly
from `test/e2e-web`, or set `LLM_E2E_CREATE_TASKS=false` for the runner:

```sh
PLAYER_SCENARIO_BINARY=/tmp/player-scenarios npx playwright test \
  --config playwright.scenarios.config.ts --reporter=line
PLAYER_SCENARIO_BINARY=/tmp/player-scenarios npx playwright test \
  scenario-S22.test.ts --config playwright.scenarios.config.ts --reporter=line
```

The runner retries each failure once in another fresh sandbox. Zero tests,
skipped tests, setup errors and failed assertions cannot pass. Unmapped IDs
are reported as unsupported. Twice-failed scenarios create an `ask` task tagged
`testing` in the server component project, with scenario, spec, setup reference,
and failure output in its annotation. `npm test` checks mapping, report parsing,
and task command arguments without creating tasks.

## Coverage

| ID | Executed assertions |
|---|---|
| S01 | Browser bootstrap/login, session, sole admin, closed bootstrap |
| S02 | RSS subscription/list/download, real audio, progress, completion |
| S03 | Bearer multipart upload, DB/detail, rescan, visible search card, cleanup |
| S04 | Public player/title, login boundary, share listing/revocation |
| S05 | Token metadata/secrecy, Bearer access/revocation, session logout |
| S06 | Config, browse response arrays, cover regeneration and image fetch |
| S07 | Favorite toggle, tag persistence/removal, note create/read/delete |
| S08 | Thumbnail regeneration, playback hints, stream range/download |
| S09 | Uploaded media active/trash/restore lifecycle |
| S10 | Batch positions in detail/DB and finished/reset transitions |
| S11 | Owner shares, unauthenticated thumbnail/download, revocation |
| S12 | User create/list/login/delete and rejected login |
| S13 | Permission matrix grant/revoke and cleanup |
| S14 | Rescan counters/completion, populated media, nonadmin denial |
| S15 | 401/403/404 boundaries, missing tag, empty-set cover error |
| S16 | Disjoint pages, invalid query defaults, search/type/favorite/set filters, sorting |
| S17 | Feed/episode shapes, missing/zero IDs, nonadmin subscribe denial |
| S18 | Single progress threshold, two in-progress entries, validation, finished/reset |
| S19 | Two users/two sets, viewer reads/personal writes, forbidden tags/delete, owner writes |
| S20 | HEAD and exact byte slices, suffix/open/multiple/invalid ranges, 304 validators |
| S21 | Missing multipart/file, extension, traversal, missing/forbidden set, 413, empty name, dedupe |
| S22 | Forced DB expiry on all public routes, unique tokens, invalid settings, max uses |
| S23 | Populated user-owned tables including podcast/accumulator, cascade, invalid session/token/login/share |
| S24 | Soft-delete across rescan, unique DB row, disk-orphan reconciliation |
| S25 | SQL/wildcard probes, literal XSS note/tag round-trip, traversal, intact user/media tables |

Scenario prose was corrected where it disagreed with the current API/source:
episode lists take a **set ID**; media detail wraps metadata in `media`; progress
reset removes the row; accumulation requires successive updates; tags require
owner access; token revocation returns 204; empty upload filenames are rejected
as missing multipart files; malformed byte ranges return 416 in Go's
`http.ServeContent`. Empty collections are asserted as arrays, not nullable
values. Browser search uses the `/` keyboard shortcut.
