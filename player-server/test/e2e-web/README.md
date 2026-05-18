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
# From player-server/ — use testmedia/ as the media library.
MEDIA_ROOT=./testmedia SECURE_COOKIES=false DB_PATH=/tmp/player-e2e.db ./player
```

Key environment variables for the test server:

| Variable        | Value          | Reason                                              |
|-----------------|----------------|-----------------------------------------------------|
| `MEDIA_ROOT`    | `./testmedia`  | Provides pre-existing sets so the grid test passes. |
| `SECURE_COOKIES`| `false`        | Allows the session cookie over plain HTTP.          |
| `DB_PATH`       | `/tmp/player-e2e.db` | Isolates the test database from production.  |

## Running the tests

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
