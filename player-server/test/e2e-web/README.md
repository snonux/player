# Player — Web UI Smoke Tests (Playwright)

This directory contains a Playwright (TypeScript) smoke suite that exercises
the Player web UI end-to-end against a running server.

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
