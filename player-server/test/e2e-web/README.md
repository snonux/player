# Player — Web UI Smoke Tests (Playwright)

For M10 artist/album enrichment, start the deterministic provider and a
disposable music fixture with its optional agent URL. Run the focused journey
in installed Chrome, then Firefox, with one browser worker at a time:

```sh
printf ready >/tmp/player-m10-mode
python3 player-server/test/enrichment/provider.py --port 18910 --mode-file /tmp/player-m10-mode &
MUSIC_ENRICHMENT_URL=http://127.0.0.1:18910/metadata \
  python3 player-android/integration_test/support/music_fixture.py up player-server/player \
  --port 18810 --no-doubles --dir /path/to/fixture --evidence /tmp/player-m10-evidence
cd player-server/test/e2e-web
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_M10_ISOLATED=1 \
PLAYER_M10_MODE_FILE=/tmp/player-m10-mode PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-enrichment-live.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The journey exercises admin refresh for artist and album, provider image and
links, retained local cover and audio, no match, provider failure, offline
retry, restart persistence, and viewer/access permissions. Stop the fixture
with `music_fixture.py down /path/to/fixture` and stop the provider process
after Android acceptance. Artifacts stay outside the repository.

For M06 music information, create a disposable fixture on an unused port,
augment it once with the guarded technical tracks, then run installed Chrome
and Firefox sequentially:

```sh
python3 player-android/integration_test/support/music_fixture.py up player-server/player \
  --port 18816 --no-doubles --dir /path/to/fixture --evidence /tmp/player-m06-evidence
python3 player-android/integration_test/support/music_info.py /path/to/fixture
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-info.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

Run the Python commands from the repository root and Playwright from
`player-server/test/e2e-web`. The journey checks tagged source units against
the probe, live audio while Info opens and closes, focus and narrow themes,
lossy omissions, offline/restart recovery and permission denial. Keep the
fixture alive until Android acceptance finishes; then stop it with
`music_fixture.py down /path/to/fixture`. Evidence stays outside the repository.

For M40 artist Top songs, use an isolated `music_fixture.py up` server on an
unused port and run one installed browser at a time:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_M40_ISOLATED=1 \
PLAYER_M40_EVIDENCE=/tmp/player-m40-web-evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-artist-top-songs.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The runner creates a fresh three-song credited artist for each browser run. It
checks empty, server-wide fallback and personal ranking, ordered queue and
captured audio, keyboard and pointer navigation, narrow light/dark layouts,
restart persistence, access revocation and offline Retry. Artifacts stay
outside the repository. Stop the fixture with `music_fixture.py down`.

For M38 album discovery, use a disposable `music_fixture.py up` server on an
unused port and run one installed browser at a time:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_M38_ISOLATED=1 \
PLAYER_M38_EVIDENCE=/tmp/player-m38-web-evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-discovery-collections.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The runner adds two tagged 70-second albums only to that fixture, creates a
fresh scoped account, and seeds its qualified plays through the authenticated
playback-sync API. It verifies all six discovery sources, complete Play/View all
snapshots and actual audio, personal empty states, random Back, genre and
decade filters, narrow light/dark layouts, real ACL removal and offline Retry.
Artifacts stay outside the repository. Stop the fixture with
`music_fixture.py down`.

For M47 saved music collections, create a disposable `music_fixture.py up`
server and run the focused journey one browser at a time:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_M47_ISOLATED=1 \
PLAYER_M47_EVIDENCE=/tmp/player-m47-web-evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-offline-pins.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The test pins an album and an overlapping playlist, verifies original bytes,
clears a populated disposable cache, then plays saved audio in the app while
offline. It also checks reload, local Unpin/Remove behavior, narrow theme,
account isolation, and a real permission denial and retry. Evidence stays
outside the repository; stop the fixture with `music_fixture.py down`.

For M36 personal listening, start a disposable `music_fixture.py up` server on
an unused port and run the focused journey in installed Chrome and Firefox,
one browser at a time:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_M36_ISOLATED=1 \
PLAYER_M36_EVIDENCE=/tmp/player-m36-evidence \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-personal-listening.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The test adds an 85-second and an 18-second tagged MP3 only to that disposable
fixture. It verifies real audio and the 60-second personal threshold, a skipped
song, album/artist aggregates, separate accounts, keyboard/pointer controls,
narrow light/dark layouts, reload, real ACL removal and offline Retry. Artifacts
stay outside the repository. Stop the fixture with `music_fixture.py down`.

For the music equalizer's actual output journey, use the disposable music
fixture and installed Chrome/Firefox:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture PLAYER_EQ_EVIDENCE=/tmp/player-eq \
  PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx playwright test -c playwright.music.config.ts music-equalizer.test.ts --workers=1
```

The journey captures the real destination signal for flat, custom mid cut,
both presets, bypass/reset, buffered offline playback and reload persistence.
It also exercises native slider keys, Escape focus and narrow/light/dark
controls. WAVs and measured bands stay outside the repository. The null sink
must already exist; it prevents the probe tones reaching host speakers.

This directory contains a Playwright (TypeScript) smoke suite that exercises
the Player web UI end-to-end against a running server.

The A03a format journey reuses the disposable music fixture. Supply a JSON
array of indexed media objects (`id`, `file_name`) for the additive **A03a
Formats** folder, with TTML, YAML, ELRC, SRT, Malformed Fallback, Priority,
YAML Plain and Unavailable MP3 tracks. Timed cases share line starts at 0, 2
and 8 seconds; the full fixture definitions and retained setup command are
recorded in the [A03a implementation notes](../../../docs/music-a03a-implementation.md).

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
PLAYER_LYRIC_FORMAT_CASES=/tmp/a03a/indexed.json \
PLAYER_LYRIC_FORMAT_EVIDENCE=/tmp/a03a \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx playwright test -c playwright.music.config.ts music-lyric-formats --workers=1
```

This checks actual playback identity/time, keyboard and pointer line seeking,
paused and loaded-offline seek, plain/unavailable results and light/dark
contrast at a narrow viewport in installed Chrome and Firefox. It does not
replace the native Android journey.

## Prerequisites

| Requirement | Version |
|-------------|---------|
| Node.js     | 18+     |
| npm         | 8+      |
| Chromium    | installed via Playwright (see below) |

The Player server binary must be running before the tests are launched.
The tests do **not** start or stop the server themselves.

## First-time setup

```sh
# From this directory:
npm install
npm run install-browsers   # downloads Chromium for Playwright
```

## Starting the server

The tests default to `http://localhost:8080`. You can override the address with
the `PLAYER_URL` environment variable.

Start the server **from the `player-server/` directory** (not from the project root).
The binary resolves the embedded `web/` static asset directory relative to its working
directory, so running it from a different location will cause static files to 404.

```sh
# From player-server/ — use testdata/media/ as the media library.
MEDIA_ROOT=./testdata/media SECURE_COOKIES=false DB_PATH=/tmp/player-e2e.db ./player
```

Key environment variables for the test server:

| Variable        | Value          | Reason                                              |
|-----------------|----------------|-----------------------------------------------------|
| `MEDIA_ROOT`    | `./testdata/media`  | Provides pre-existing sets so the grid test passes. The library must hold only these sets: extra ones (e.g. a `test-images` set of the live suite) make several selectors match more than one element. |
| `SECURE_COOKIES`| `false`        | Allows the session cookie over plain HTTP.          |
| `DB_PATH`       | `/tmp/player-e2e.db` | Isolates the test database from production.  |

## Running the tests

The audio visualization suite runs in installed Google Chrome and Playwright
Firefox without a backend (it serves local web assets and generated audio):

```sh
npx playwright install firefox
npx playwright test -c playwright.visuals.config.ts
```

```sh
# Headless (default — suitable for CI):
npm test

# With a visible browser window (useful when debugging):
npm run test:headed

# Interactive Playwright UI:
npm run test:ui
```

To run against a different server:

```sh
PLAYER_URL=http://localhost:9090 npm test
```

## Test accounts

The suite creates the following accounts on first run (idempotent on re-runs):

| Account        | Password             | Role      |
|----------------|----------------------|-----------|
| `e2e-admin`    | `e2e-passw0rd!`      | admin     |
| `e2e-user`     | `e2e-user-passw0rd!` | regular   |

If the server already has a bootstrapped admin account you must either wipe the
database (`rm /tmp/player-e2e.db`) or change `ADMIN_USER` / `ADMIN_PASS` in
`tests/helpers/server.ts` to match the existing credentials.

## Test structure

```
test/e2e-web/
├── playwright.config.ts     # Playwright configuration (baseURL, timeouts, reporter)
├── package.json             # npm dependencies
├── tsconfig.json            # TypeScript configuration
├── README.md                # this file
└── tests/
    ├── helpers/
    │   └── server.ts        # API helpers: bootstrap, login, waitForServer
    └── smoke.test.ts        # Smoke suite (7 test groups)
```

## Smoke suite coverage

| # | Test                                     | What it checks                                  |
|---|------------------------------------------|-------------------------------------------------|
| 1 | bootstrap page is reachable              | `/bootstrap.html` renders                       |
| 2 | bootstrap form creates admin             | Form submit redirects to `/login.html`          |
| 3 | login form authenticates                 | Form submit lands on `/` with the header        |
| 4 | login with wrong password shows error    | Error message appears without page navigation   |
| 5 | sets sidebar opens                       | Sidebar shows at least one set                  |
| 6 | set list shows set names                 | Set names are non-empty strings                 |
| 7 | clicking a set loads the media grid      | Grid appears with at least one card             |
| 8 | progress API accepts position update     | POST /api/v1/progress returns 200               |
| 9 | admin button visible and opens panel     | Gear button is un-hidden for admin; modal opens |
| 10| admin panel hidden for regular user      | Gear button stays hidden                        |
| 11| admin API returns 403 for regular user   | GET /api/v1/admin/users → 403                   |
| 12| logout redirects to login                | Session ends, browser lands on `/login.html`    |

## Live-deployment suite

`tests/live-deployment.live.ts` runs against an already bootstrapped,
deployed instance instead of a throw-away local server. It is excluded from
`npm test` (the default config only matches `*.test.ts`) and has its own
config, `playwright.live.config.ts`.

Differences from the smoke suite:

- It never calls the bootstrap endpoint and contains no passwords. The
  accounts come from the environment, so it is safe to point at an
  internet-facing instance.
- It uses the installed Google Chrome (`channel: 'chrome'`). The Chromium
  bundled with Playwright has no H.264/AAC/MP3 decoders and cannot tell which
  formats a real browser plays.
- It expects the "all formats" library: sets `test-videos`, `test-audio` and
  `test-images`, each holding one `sample-<ext>.<ext>` file per extension in
  `internal/mediatype`. Stems must be unique per directory because thumbnails
  are named after the stem.
- All 16 audio/video formats are asserted as playable. AVI, WMV, FLV and WMA
  (media the server flags as `"transcoded"`) play through the compatibility
  stream; their first play waits for the server-side transcode, so those tests
  poll for up to 90 seconds.

```sh
PLAYER_URL=https://player.example.org \
E2E_ADMIN_USER=... E2E_ADMIN_PASS=... \
E2E_USER=... E2E_USER_PASS=... \
npx playwright test -c playwright.live.config.ts
```

`live-foostore.sh` sets that environment from the
[foostore](https://github.com/snonux/foostore) password manager, so the
passwords are neither typed nor exported into the shell. It expects one entry
per account, named `pass/<host>-<username>.txt`, with the password in its
Password field:

```sh
./live-foostore.sh player.example.org ADMIN_USER USER            # whole suite
./live-foostore.sh player.example.org ADMIN_USER USER --grep auth
```

Arguments after the two user names go to Playwright. `FOOSTORE_ENTRY_DIR`
changes the `pass` group and `FOOSTORE_PASS_FILE` the passphrase file
(default `~/.master.pass`). The instance needs both an admin and a non-admin
account; the suite refuses to start without either.

The suite removes the favorites, notes, tags, progress, shares and
permissions it creates. Its upload test leaves one soft-deleted
`e2e-upload-*.png` in the admin trash (and on disk in `test-images/`).

## CI integration

On CI set `CI=true` (Playwright reads this automatically) to:
- enable one retry per test
- fail on any `test.only` left in the source

Example GitHub Actions step (after `npm ci` and `npm run install-browsers`):

```yaml
- name: Run Playwright smoke tests
  env:
    CI: true
    PLAYER_URL: http://localhost:8080
  run: npm test
  working-directory: player-server/test/e2e-web
```

## Legacy MPEG playback (Chrome and Firefox)

Build the server with `mage build` in `player-server/`, then start the isolated
fixture from the monorepo root:

```sh
python3 player-android/integration_test/support/all_formats_fixture.py \
  player-server/player
```

From this directory, use its printed `auth_file` and `origin`:

```sh
PLAYER_URL=http://127.0.0.1:18765 \
PLAYER_FORMAT_AUTH=/tmp/player-formats-EXAMPLE/auth.json \
npx playwright test -c playwright.mpeg.config.ts
```

This runs real MPEG-1 `.mpg` and MPEG-2 `.mpeg` through login, library play,
seeking, ranged compatibility/original downloads and single-use public share
playback in installed Google Chrome and Playwright Firefox. No private media
is used. The native checks use the same fixture; see
`player-android/docs/media-format-verification.md`.

## Music fixture suite (Chrome and Firefox)

`tests/music-baseline.test.ts` runs against the disposable music fixture
(`player-android/integration_test/support/music_fixture.py up`) with its own
config: installed Google Chrome (`channel: 'chrome'`) and the system Firefox
(`channel: 'moz-firefox'`, binary from `PLAYER_FIREFOX`, default
`/usr/lib64/firefox/firefox`). It covers sign-in with a wrong password,
a keyboard journey with focus restore and typing checks, a touch (Chrome) or
mouse (Firefox) journey as the regular user, and calibration of the audio
output capture (`tests/helpers/audio-tap.ts`).

```sh
PLAYER_MUSIC_FIXTURE=/path/printed/by/up \
  npx playwright test -c playwright.music.config.ts
```

Results, screenshots and WAV captures go to the fixture's evidence
directory. The full three-platform runbook is
`player-android/docs/music-e2e-verification.md`.

### F01 timed lyric seeking

Run the real timed-lyric journey in installed Chrome and Firefox against the
same disposable fixture:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-lyrics.test.ts --workers=1
```

It starts the tagged timed-lyric source, seeks with a focused line and a
pointer click, checks the current-line state, repeats a loaded-line seek while
offline, then verifies the Unicode plain-lyric source remains readable without
seek buttons. Screenshots and evidence stay with the fixture. Android timed-tap
acceptance remains a separate emulator requirement.

### M67 translated music UI

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-language.test.ts --workers=1
```

This narrow-screen journey starts real playback, changes the Settings language
to German, checks live labels/document language and retained playback, restores
the saved choice after reload, then switches back to English. Its screenshots
and result stay in the fixture evidence directory; Android locale acceptance
requires a fresh emulator APK. The companion playlist journey uses the viewer
account and a second real Settings tab to switch mounted create/edit dialogs,
retain typed drafts and focus, save by keyboard and verify verbatim authoritative
state with 0/1/2 German song totals. It captures narrow light/dark layouts,
overrides `navigator.language` with unsupported French, and injects a scoped
browser storage exception to check the visible session-only warning. These
controlled browser boundaries do not claim Android storage-failure execution.

### B9 artwork duration/size badge contrast

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-artwork-badge.test.ts --workers=1
```

This opens an actual audio card and checks the visible duration/size chip in
both saved themes. It composites the translucent chip over black artwork (the
worst case for its white scrim) and requires at least 4.5:1 text contrast;
screenshots and per-browser evidence remain in the fixture directory.

### M24 ReplayGain loudness controls

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-loudness.test.ts --workers=1
```

The fixture's tagged *Gain Check* tones are measured after the actual Web
Audio gain node: Off preserves their 18 dB level difference, Track applies the
known -9/+9 dB tags, Album preserves their dynamics, and Automatic falls back
to Track outside an album queue. Actual preamp output changes by +6/-6 dB.
An ordered album journey measures Automatic's 18 dB album dynamics and checks
that choosing Track explicitly overrides that context on the same source.
Album gain plus 12 dB preamp exceeds the known peak: the output must limit to
0 dBFS peak, retain its sine crest factor, and rise only 3.5 dB from the album
baseline. Untagged positive gain must leave output equal to Off. Each capture
seeks through the real timeline first so the short fixture cannot finish
during the multi-mode journey. Browser and native checks use separate fixture
accounts. Native ReplayGain acceptance uses the dedicated emulator/WAV runner
in `../../../player-android/test/e2e-live/README.md`.

### B3 Sets audio/image sequencing

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-audio-image-skip.test.ts --workers=1
```

This short regression starts the last audio file in a real album folder with
cover art, triggers its natural end, and proves auto-advance plus
`Shift+Previous`/`Shift+Next` keep the audio player active rather than opening
the image viewer.

For M12 track navigation, natural completion, audible source/progress and
failure/Retry journeys on both installed browsers:

```sh
PLAYER_MUSIC_FIXTURE=/path/printed/by/up \
  npx playwright test -c playwright.music.config.ts --grep M12
```

These retain Sets browse/folder/wrap/resume behavior while ensuring that
advancing from audio skips image cards. They do not assert a separate music
queue or future repeat/shuffle/persistence behavior.

M02 album catalog/detail and collection acceptance uses the same isolated
fixture (regenerate it so Restricted includes the hidden contribution to
Night Transit):

```sh
PLAYER_MUSIC_FIXTURE=/path/printed/by/up \
  npx playwright test -c playwright.music.config.ts --workers=1 --grep M02
```

The three journeys per browser cover library and song entry, actual artwork,
viewer-only totals vs an admin-only contribution, disc/track ordering, linked
artist album filtering, light/dark/narrow catalog and detail layouts,
selection/focus/typing, actual browser offline Retry and real ACL-empty states,
original-source audible natural A→B→C playback while browsing another album,
short-song start at zero, stop at collection end, and a real permission
revocation/restoration during playback. The failed next source stays current
and the existing Play/Pause retry reloads that source. The fixture account's
permission is restored in `finally`; use serial workers because ACL edits
are deliberately shared fixture state. A browser Back journey restores a
different Sets source and proves Next follows that set, while ordinary album
browsing retains the active collection snapshot. The design journey also
checks that a pending search cannot override explicit Albums navigation,
while intentional search still opens Sets results.

### M13 editable Now Playing queue (installed Chrome and Firefox)

After creating an isolated music fixture with `music_fixture.py up`, add its
1001-entry scale album. The helper requires the disposable marker, generates a
real tagged 440 Hz FLAC and hard links it at distinct library paths, rescans the
real server, and waits for 1001 real album media IDs. It uses no API or playback
double. Keep all fixture media, databases, WAVs and screenshots outside git.

```sh
python3 ../../../player-android/integration_test/support/music_queue_scale_fixture.py "$PLAYER_MUSIC_FIXTURE"
PLAYER_QUEUE_SCALE=1 npx playwright test -c playwright.music.config.ts music-queue.test.ts
```

The five journeys per browser cover focus and keyboard/button/menu alternatives,
Q/Escape opener return, current removal/Clear and Undo without automatic restart,
edited Next and three natural distinct tones plus finished media IDs while
browsing another album, retained inactive queue versus actual Sets Next, real
ACL denial/retry, offline edits, reload-empty behavior, all 1001 entries and
narrow light/dark long-title/artist queue captures.
The first source is paused immediately before slow UI edits. Screenshots cover
light/dark desktop and narrow modal layouts. A `--list` run only validates test
discovery; it does not verify the feature. Set `PLAYER_QUEUE_SCALE=1` for actual
acceptance so the scale scenario cannot be silently skipped.

### M14 explicit song insertion

`tests/music-insertion.test.ts` uses the disposable music fixture and installed
Chrome/Firefox configuration above. With a fresh current server binary/fixture,
run `PLAYER_MUSIC_FIXTURE=<fixture> PLAYER_E2E_PULSE_SINK=<owned-null-sink>
npx playwright test -c playwright.music.config.ts --grep M14 --workers=1`.
The five main journeys and focused batch case exercise actual selected/single Play now, FIFO Play next,
append, duplicate occurrences, current source/position preservation, Undo,
natural A→C→B source tones and saved completion IDs, focus/typing/native Space,
Sets actions, real revoked permission/retry, empty/reload/offline cached modules,
and light/dark/narrow/long labels. Evidence and WAVs go outside git to the fixture's
evidence directory. A listed test or Node check does not count as browser proof.

M14 verification checkpoint (2026-10-21): installed Chrome 144.0.7559.132 and
Firefox 156.0 have accepted all five main journeys plus the focused batch case each across retained runs.
The latter proves Append preserves an actively playing source and displayed
selection order, then batch Play now retains the remainder and audibly starts B→C. The
permission oracle uses an untouched stream and verifies a real revoked 403;
queue retry uses explicit visible/open-state controls. Initial cached-target
and closed-panel oracle failures remain in the external evidence. See
[the M14 verification record](../../../docs/music-m14-implementation.md) for
versions, exact commands, audio/ID evidence and the separate native UI checkpoint.


M03 artist acceptance uses `tests/music-artists.test.ts` with the same disposable
music fixture and `playwright.music.config.ts` (both installed browsers):
`PLAYER_MUSIC_FIXTURE=/home/paul/tmp/zj3-fixture npx playwright test -c
playwright.music.config.ts --workers=1 --grep M03`. It exercises default Artists,
all four credit roles, separate structured people, artist/discography Back
selection, light/dark/narrow layouts, native typing, actual offline Retry,
real permission revocation/empty results and source-tone playback. M02 linked
artist assertions now follow the full artist detail and Back to the originating
album. Keep logs, screenshots, audio and test outputs outside the repository.

M26 persistent playlists uses the disposable Music fixture in installed Chrome
and Firefox. After starting the fixture with the current server binary:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fresh/fixture \
  npx playwright test -c playwright.music.config.ts music-playlists
```

The journey covers create/rename/comments, minimal album addition, account
isolation in actual UI, a fresh browser client, authoritative totals, decoded
artwork and source audio, delete cancel/confirm, offline form retention,
cached app-shell module loading and dialog completion ownership. Evidence is
written to the fixture's external evidence directory.

M27 playlist song editing extends the same fixture/config with
`music-playlist-songs`: selected additions, a 47-occurrence long list,
filtered later-duplicate play/move/remove/Undo, actual saved-order persistence,
offline and revoked permission failures, keyboard focus, narrow themes and a
real delayed-response regression preventing duplicate pending saves. Run only
against the disposable current server with the installed Chrome/Firefox config;
artifacts and source audio stay in the fixture's external evidence directory.

M27 acceptance (2026-10-21): installed Chrome144 and Firefox156 each passed all
three scenarios across retained runs. The initial core journeys/Chrome pending
case passed; boundary-only reruns corrected the documented Escape focus oracle;
the final Firefox pending case used a controlled original-fetch response delay
to avoid BiDi interception limitations. Original failures, source WAVs, themes
and authoritative saved state are retained under `/home/paul/tmp/nl3-evidence`.
See [M27 acceptance](../../../docs/music-m27-implementation.md) for exact commands,
combined outcomes and fault-injection limits.

### M16 local queue recovery (installed Chrome and Firefox)

After root gates/review and a fresh server build, create the isolated music
fixture on port 18809 with evidence outside git. Use the existing installed
Chrome/Firefox music configuration and Pulse sink setup. From this directory:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/yk3-fixture \
PLAYER_E2E_PULSE_SINK=player_yk3 \
npx playwright test -c playwright.music.config.ts music-queue-recovery --workers=1
```

The two main cases per browser build a repeated-occurrence playlist through the real
API, play/reorder/pause through the UI, reload and assert original occurrence
IDs/index/exact short-song position with paused nonadvancing media. Only explicit
Resume starts original source audio, measured with the existing PCM tap. They
exercise light/dark/narrow/focus/typing, explicit other-media URL priority,
actual logout/account roundtrip, Clear/reload, real permission revocation and
corrupt local data. Error testing first aborts metadata transport to expose Retry
for a known authenticated scope, then uses actual `context.setOffline(true)` for
Retry/retained snapshot/no-autoplay before online Retry. The initial abort is
fault injection; the offline phase is the browser's actual network boundary.
Snapshot reads/imported queue state are evidence only; successful playback and
queue edits use visible controls. Corrupt JSON is explicit negative-state setup.
Do not call a --list parse check actual browser acceptance.

A focused third case (`--grep 'normal Next'`) sets up only the required recovered
later duplicate, resumes it and uses the visible Next control. It requires the
following occurrence, Long Road Home source/runtime and original tone, without
replaying account/theme/error phases.

### M15 whole collection playback and enqueue

With a fresh current disposable Music fixture and installed Chrome/Firefox:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/kl3-fixture \
PLAYER_E2E_PULSE_SINK=player_kl3 \
npx playwright test -c playwright.music.config.ts music-collections --workers=1
```

The three cases per browser exercise album/artist/playlist detail Play, Play
next and append, whole-batch Undo, catalog multi-selection in reverse click
order, duplicate playlist occurrences, source PCM, keyboard navigation and
native typing, light/dark/narrow layouts, real browser offline failure and
visible shortcut feedback. A one-shot delayed original fetch checks duplicate
submission and superseded source ownership; it changes timing only, not server
metadata or playback responses. Runtime queue reads are evidence; feature
mutations use visible controls. A `--list` parse pass is not feature acceptance.

## Music M20 repeat modes

`music-repeat.test.ts` drives installed Chrome and installed Firefox using the
Music fixture and `playwright.music.config.ts`. Four cases per browser cover
Off natural stop and same-source Replay, All wrap with actual distinct output
and edited occurrence order, One same-occurrence time reset versus manual
Next/Previous, paused reload/mode/account isolation, Clear/Undo, native typing
and Y focus, light/dark narrow layouts, real offline Retry, and the actual
detached window's completion/finished writes and popup Y. Natural tests let
short tracks reach their real end; no synthetic ended events or playback mocks.
PCM WAVs, snapshots, versions and screenshots go to the fixture evidence path.

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/dk3-fixture PLAYER_E2E_PULSE_SINK=player_dk3 \
  npx --no-install playwright test -c playwright.music.config.ts music-repeat --workers=1
```

Use a fresh disposable fixture built from the current server. This command is
an acceptance runner; a successful `--list` only validates runner discovery.

## Rich music OS controls (M54)

The M54 suite needs a disposable fixture created with `--session-artwork` and
an existing Linux desktop session bus. It uses installed **headful** Chrome and
Firefox, serially. Headless metadata queries do not count as OS acceptance.
The optional fixture mode leaves default fixtures unchanged; its three transport
sources last 45 seconds and the first two have distinct album/artist tags,
tones and blue/orange covers.

```sh
# From player-android/, after building a fresh external server binary:
python3 integration_test/support/music_fixture.py up /tmp/qk3-evidence/player \
  --port 18812 --no-doubles --session-artwork \
  --dir /tmp/qk3-fixture --evidence /tmp/qk3-evidence/actual-attempt1

# From player-server/test/e2e-web/, after creating an owned task audio sink:
PLAYER_MUSIC_FIXTURE=/tmp/qk3-fixture PLAYER_E2E_PULSE_SINK=player_qk3 \
  npx playwright test -c playwright.media-session.config.ts
```

Every MPRIS command verifies the exported bus owner PID, executable, exact
profile, process start and ancestry to this test worker. No command targets
other players or uses `playerctl --all`. Evidence includes actual exported
metadata, copies of OS-readable cached artwork decoded with Pillow, system
Next/Previous/Play/Pause effects on real source tones and fresh per-ID progress,
full/mini screenshots, keyboard focus/typing, detached ownership, denied art,
actual browser offline transport/retry and logout/close clearing. A physical Cast
receiver is not part of this local test; sender payload tests do not verify one.

Observed Chrome 144 preconditions are explicit: after a paused detached window
returns, the player stays paused and says “Playback returned here. Press Play
to resume.” A new source that failed while offline also needs the existing
visible Play after reconnect before Chrome exposes an active OS session.
The failed OS-only attempts are retained; they are not reported as passing.
After that visible action, the suite verifies real OS metadata/artwork and
transport. Firefox also verifies OS-only paused-return resume and failed-source
retry. Existing visualizations are preserved; selecting their visible Off
option allows the full-player artwork to be inspected alongside mini/OS art.

The accepted qk3 run used Chrome 144 and Firefox 156 on 2026-10-22. Normal
transport/artwork passed in Chrome attempt 1 and Firefox attempt 3; supported
Chrome paused return and denied art were accepted from attempt 7, Firefox's
strengthened boundary case from attempt 8, and Chrome's remaining offline/clear
case from attempt 9. Failed attempts remain under
`/home/paul/tmp/qk3-evidence`; this is not an all-attempts-pass report. The shared
server version was 0.8.2. See `docs/music-m54-implementation.md` from the repo root
for full gates, review findings, native evidence and platform limits.

For example, the final focused Chrome continuation used this command from this
directory, against the retained task fixture and already owned sink:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/qk3-fixture-attempt2 \
PLAYER_MUSIC_EVIDENCE=/home/paul/tmp/qk3-evidence/actual-attempt9 \
PLAYER_E2E_PULSE_SINK=player_qk3 \
  npx --no-install playwright test -c playwright.media-session.config.ts \
  --project=chrome --grep 'Chrome failed untouched source' --workers=1
```

Use the main test title or `--grep 'detached OS owner'` for the respective
normal/boundary phases; preserve failed outputs before a bounded unfinished
retry. All task runtimes were subsequently stopped after identity checks; both
fixtures and all evidence were preserved. Physical Cast and headset checks were
not run and are not implied by these local OS control checks.

## M68 appearance presentation (Chrome and Firefox)

With the disposable Music fixture running, execute the focused appearance journey
in each installed browser:

```sh
PLAYER_MUSIC_FIXTURE=/path/to/fixture \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-presentation.test.ts --workers=1
```

The journey starts an actual fixture track, turns visuals off, changes every
dark/light accent combination and artwork shape/display option, verifies focus
return after Settings closes, restart persistence, and an offline local-preference
change. It saves narrow-screen screenshots and the platform result under the
fixture evidence directory. The fixture track intentionally has no track artwork,
so the test also checks that the existing missing-art fallback remains honest;
native artwork and keep-awake acceptance require the Android emulator run.

### M18 playback shuffle

`tests/music-shuffle.test.ts` uses the existing isolated music configuration:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/zk3-fixture \
PLAYER_E2E_PULSE_SINK=player_zk3 \
npx playwright test -c playwright.music.config.ts tests/music-shuffle.test.ts
```

It drives installed Chrome and Firefox with production randomness: queue
remainder toggle/Off, reshuffle/Undo, current source and position, independent
browsing and unchanged `r`, a full audible occurrence cycle with duplicate
media, optional genre/year/max validation, no-match/offline retry, delayed real
response cancellation, Escape focus, account/reload recovery, native typing,
and light/dark narrow layouts. It asserts occurrence multisets rather than
requiring each random draw to differ. Sources are measured through the existing
PCM tap. No fixture or browser is started by `--list`.

The accepted 2026-10-22 final-server runs used installed Chrome 144.0.7559.132
and Firefox 156, headful, one worker and zero retries. Under
`/home/paul/tmp/zk3-evidence`, Chrome recovery was accepted in
`browser-attempt1-chrome`, core/options in `browser-attempt2-chrome`, and real
settled light/dark themes in `browser-attempt4-chrome-themes`. Firefox's three
functional scenarios passed in `browser-attempt3-firefox`, with settled themes
in `browser-attempt5-firefox-themes`. Real permission-boundary scenarios passed
in `browser-attempt6-chrome-permission` and `browser-attempt7-firefox-permission`.
Each captures the exact existing fixture Music grant, revokes it for direct
Shuffle and options, checks actual resolver 404 errors preserve queue/current
source/position, restores the prior grant in finally, then retries with actual
source PCM and resolver 200. Both pending Escape and Cancel release the
actual disabled opener before restoring exact focus, then reject late responses.

Attempt1's other failures were retained driver navigation mistakes (closed
Albums sidebar and an expanded floating player). Early screenshots changed the
theme attribute directly and mislabeled a dark queue as light; they are retained,
not accepted theme evidence. The accepted theme case uses the visible theme
control, persisted preference and settled computed colors; Cancel contrast is
18.522:1 in light and 12.991:1 in dark. Original PNGs, actual source WAVs,
versions, PID/start/executable/profile ownership and phase results remain per
attempt. The browser-process stop audit confirms all owned browser processes
have exited; server/emulator/sink cleanup belongs to the coordinating agent.

Use the fresh fixture's `up <server> --shuffle-playback` flag for M18: all three
Night Transit source files are generated at 45 seconds and must pass actual
ffprobe/server/manifest duration preflight before browser/native playback.
# Rich release metadata (M05)

Start the disposable music fixture with `--release-metadata`, then run:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/0k3-fixture npx playwright test -c playwright.music.config.ts tests/music-release.test.ts --workers=1
```

This drives installed Chrome and Firefox through release/credit disclosures,
Tab/Enter/Space and focus, light/dark/narrow layouts, source playback, real
retag/clear/failed-probe rescans, restart persistence and permission denial.
The fixture must be the original marked directory, since tests retag and
restart its server. Exact setup and native counterpart are recorded in
[`music-m05-implementation.md`](../../../docs/music-m05-implementation.md).

### M04 multi-disc albums and compilations

Run `tests/music-discs.test.ts` with the disposable `--multi-disc` fixture, or
root-owned guarded augmentation of the retained fixture described in
[the M04 runbook](../../../docs/music-m04-implementation.md). It exercises
actual disc headings/subtitles/art, separate credited artist links and Back,
global song indices, source tones across the disc boundary, nonwrapping end,
light/dark/narrow, keyboard/typing/Info, real offline/ACL Retry, absent/broken
images and M04 rescan/clear/failed-probe/server-restart persistence. Shared
foreground/two-frame viewport readiness matches M05 without forcing styles.

### Metadata-aware search (M07)

Use the existing disposable Music fixture; no extra generation flag is needed.
The default tagged tracks include titles differing from filenames, structured
artists, Unicode, missing metadata, long text and inaccessible music. From this
directory, root runs actual installed Chrome and Firefox:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/9k3-fixture \
  npx playwright test -c playwright.music.config.ts tests/music-search.test.ts --workers=1
```

Four cases per browser cover Songs/Albums/Artists/Files, title/artist/album and
Unicode scopes, exact artist punctuation, no results, keyboard typing/Enter/Escape,
stale-request rejection, URL refresh, entity Back selection, Files resume semantics,
real source audio, long light/dark/narrow layouts, queue isolation, real offline
Retry, ACL changes and same-database server restart. The restart case uses the
marked disposable fixture's ownership-aware helper. Never use production data.
The hasTouch case uses trusted touchscreen taps on the Files open
button and row action menu, verifies the menu does not start playback prematurely,
and checks actual media source progression. Evidence records versions, source
audio, screenshots, API state and failures in the external fixture evidence directory. `--list` only checks discovery; it is
not browser acceptance. See [M07 implementation](../../../docs/music-m07-implementation.md).

The installed Playwright BiDi touch method is empty. For system Firefox, the
runner's `genuine-touch.cjs` helper preserves actionability checks and sends
standard touch pointer actions through the browser's existing owned BiDi session.
It fails if that transport is unavailable. Chrome retains `Locator.tap()`.
Trusted touch events, source progression and menu behavior remain required;
the runner records bounded input diagnostics without patching Playwright.

### F03 existing file filters

After final server build, augment the shared marked disposable music fixture
with `python3 player-android/integration_test/support/music_filters.py
"$PLAYER_MUSIC_FIXTURE"` from the monorepo root. Then run, from this directory:

```sh
PLAYER_MUSIC_FIXTURE=/absolute/new/fixture npx --no-install playwright test \
  -c playwright.music.config.ts tests/music-filters.test.ts --workers=1
```

Installed Chrome and Firefox exercise existing Sets query/type/favourite/sort,
tag AND matching, single/multi-set scope, exact inclusive minutes/MiB and zero,
combination/Clear, URL reload, tag editing/focus, light/dark/narrow and actual
offline recovery. See [F03 contract and native runner](../../../docs/music-f03-implementation.md).
Do not run the helper on production or reuse an already augmented namespace.

### Playback speed (fk3): integrated verification

Cheap component-local check: `node --experimental-default-type=module web/js/tests/playback-speed.test.js`
from `player-server/`. It checks control/state contracts with fakes and does
not verify audible progression or browser layouts.

Run against the existing disposable music fixture:

```sh
PLAYER_MUSIC_FIXTURE=/tmp/player-music-fixture \
  npx playwright test -c playwright.music.config.ts music-playback-speed.test.ts
```

Integrated acceptance on 2026-10-24 used installed Chrome 144 and Firefox 156.
The core journey measured real 2× audio progression, `<` stepping to 1.5×,
1× reset and reload persistence. The additional journey selected all five
Android-equivalent rates and checked the audio element, transferred 2× to a
popup, changed it to 1.25× and closed the popup through its real `beforeunload`
handoff, then restored the minimized player and reset 1×. It also checked
anonymous-share speed/reset, narrow light/dark layouts and Escape restoring
focus to **More player controls**. Screenshots and JSON evidence remain outside
the repository under `/home/paul/tmp/player-integrated-verify-evidence/`.

Native Android acceptance used version 0.8.2 (Android version code 4022) on API 34: touch
all five existing buttons and verify actual OS media-session rates; seek to
zero, touch Play/Pause at 2×, then reset 1× and use Back to Library. The settled
paused endpoint advanced 4.690 media seconds in 2.495 wall seconds (1.88×,
including control overhead). The emulator had no audio output, so this is
position/session proof, not PCM capture. Native screenshots, state and the
reproducible command script are under
`/home/paul/tmp/player-native-retry-evidence/fk3-native/` and
`/home/paul/tmp/player-native-retry-evidence/fk3_native_speed.py`.

Cast intentionally does not support speed; the disabled control and explanation
are covered by the focused module checks. No physical receiver was used for
this local speed acceptance. Retry, spoken-audio preference and detached track
switching remain covered by the implementation's focused module regressions;
they were not additional real-device journeys in this run.

### M17 saved server sessions

Against a disposable music fixture, run the core browser journey first:

```sh
PLAYER_MUSIC_FIXTURE=/absolute/fixture PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-saved-session.test.ts
```

Both installed browsers create an actual repeated-track playlist, reorder and
seek through controls, save explicitly, and continue in a second signed-in
context. Assertions cover exact occurrence/current/offset, paused adoption,
actual Resume source/progress, offline competing saves, Keep/Replace, delayed
real responses during newer playback, focus/typing and desktop/narrow themes.
The destination refreshes its known server revision after the source goes
offline, before making the competing edit; preceding Resume/Pause progress
mutations must not become an accidental extra conflict in fixture setup.

Next run the native M17 journey documented in
`../../../player-android/test/e2e-live/README.md`. It leaves a native saved
session for the reverse-transfer browser check. Prepare a separate viewer saved
queue with occurrence IDs `101,102,103`, an available first and last track, and
a temporarily accessible Restricted middle/current track at 5.25 seconds.
Revoke the temporary Restricted grant before testing; preserve the original
queue for later fixture cleanup. This makes permission revalidation use actual
server permissions. Then run:

```sh
PLAYER_MUSIC_FIXTURE=/absolute/fixture \
PLAYER_NATIVE_SESSION=/absolute/native-core-evidence/results.json \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-saved-session-boundaries.test.ts
```

This verifies Android's saved occurrences/current/offset in both browsers and
filters the viewer's revoked current item without autoplay or rewriting the
remote session. Run browser and native scenarios sequentially: they intentionally
share the admin session. Evidence stays outside the repository. These M17 checks
use authoritative queue state plus actual media source/progress; they do not
claim sound-output measurement.

## M50 selectable streaming quality

Use a newly owned `music_fixture.py up` fixture with a fresh database. The
normal server exposes all four selected profiles; do not substitute a fixture
that only offers compatibility AAC. Run installed Chrome and Firefox serially:

```sh
PLAYER_MUSIC_FIXTURE=/absolute/fixture PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx playwright test --config playwright.music.config.ts music-quality.test.ts --workers 1
```

The normal tests exercise Original, AAC128/256, MP3192 and Opus96 through the
visible quality controls, compare actual playback blobs with response bytes,
ffprobe codecs/containers/actual bitrates, exact Range bytes, seek, real output,
keyboard focus/Pause/Resume, next-open transitions, defaults across reload/account
changes, narrow light/dark controls and actual offline/profile-list failures.
Nominal encoder bitrate is a target; measured VBR and muxed bitrates can differ.

The unavailable-encoder test is skipped unless `PLAYER_M50_ENCODER_FAILURE=1`.
That scenario requires an explicitly owned disposable server with a cold cache
and an actually unavailable Opus encoder. It must receive a genuine server500;
an intercepted synthetic500 does not establish encoder failure. Restore the
ordinary server configuration afterwards. Retain failed attempts separately;
test-results is temporary and can be replaced by subsequent Playwright runs.

# M53 paired server addresses

Paired addresses and optional SSID routing are Android profile settings under
the U01 contract. Browsers retain their page origin. Run the shared browser
journey against the disposable fixture and a separately hosted alias of that
same server (use a different hostname, since cookies are not isolated by port):

```sh
PLAYER_MUSIC_FIXTURE=/absolute/fixture \
PLAYER_M53_ALTERNATE=http://localhost:18773 \
PLAYER_M53_EVIDENCE=/absolute/evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
npx playwright test --config playwright.music.config.ts \
  tests/music-endpoints.test.ts --workers=1
```

The test checks actual playback/output, canonical account and request origins,
alternate-origin login isolation, keyboard typing/focus/playback, real browser
offline Retry/recovery and narrow light/dark layouts in both installed browsers.
The alias must serve the fixture backend; another fixture is a different server.
No cross-origin browser routing or CORS configuration is needed for this scope.
Native pairing/security/SSID/offline acceptance and retained evidence are in
[`music-m53-implementation.md`](../../../docs/music-m53-implementation.md).

## Podcast episode conflict acceptance

Against the marked, isolated podcast fixture, run after the Alpha catalog is
subscribed and the publisher and Player server are running:

```sh
PLAYER_PODCAST_FIXTURE=/absolute/podcast-5i3 \
PLAYER_PODCAST_STAGE=episode-conflict \
  npx playwright test -c playwright.podcasts.config.ts
```

Each installed browser uses its own short Alpha episode. One visible browser
session completes it while its real successful sync response is held; a second
session uncompletes it and plays its own server copy to save a position. After
the older response arrives, the first session pulls the newer completion and
position. The stage creates a server audio copy for those two episodes and
writes evidence under `evidence/{chrome,firefox}/<run>/` in the fixture.
