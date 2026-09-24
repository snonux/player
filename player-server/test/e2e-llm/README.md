# Scenario end-to-end runner

This runner reads the Markdown scenarios in `scenarios/` and dispatches only
scenarios with an explicit executable Playwright spec. It does **not** interpret
the Markdown steps or YAML assertions. A scenario without a dedicated spec is
reported as `UNSUPPORTED`, not `PASS`; unsupported scenarios are excluded from
the passed count. The scenario files remain the specification for adding full
coverage later.

| Scenario | Executable workflow |
|---|---|
| S05 | API token creation, listing, Bearer authentication, revocation, logout |
| S14 | Admin rescan progress contract and regular-user authorization |

The mapping lives in `runner/index.ts`; the specs are
`../e2e-web/tests/scenario-S05.test.ts` and
`../e2e-web/tests/scenario-S14.test.ts`. Add a mapping only when its spec
actually exercises that scenario's workflow. Generic smoke tests are not
scenario coverage. The ordinary web `npm test` excludes scenario specs; this
runner invokes them through `../e2e-web/playwright.scenarios.config.ts`.

## Setup and execution

The runner requires Node.js 18+, npm, Playwright Chromium, and a running Player
server. Install the runner and web test dependencies separately:

```sh
cd player-server/test/e2e-llm/runner && npm ci && npm run build
cd ../../e2e-web && npm ci && npm run install-browsers
```

Start a test server from `player-server/` with an isolated database and media
root. Set `SECURE_COOKIES=false` for plain HTTP. S05 and S14 expect an existing
admin account; their default login is `admin` / `TestPassw0rd!`. Override it
with `LLM_E2E_ADMIN_USER` and `LLM_E2E_ADMIN_PASS` if needed. For example:

```sh
MEDIA_ROOT=./testdata/media SECURE_COOKIES=false \
  DB_PATH=/tmp/player-e2e-llm.db ./player
```

In a separate shell, bootstrap an empty test database with the default admin:

```sh
curl -f -X POST http://localhost:8080/api/v1/auth/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"TestPassw0rd!"}'
```

Run all scenario files or select one:

```sh
cd player-server/test/e2e-llm/runner
node dist/index.js
node dist/index.js ../scenarios/S05-auth-tokens.md
```

`PLAYER_URL` overrides the default `http://localhost:8080`. The runner first
checks `/healthz` if at least one supported scenario is selected. Each
supported scenario gets one retry after a failure. A missing spec, setup
error, zero-test result, skipped test, or failed assertion is a failure and
causes a nonzero exit. Unmapped or explicitly skipped scenarios appear in the
unsupported count and do not cause a nonzero exit by themselves. A scenario
that fails twice opens an `ask` task, when the CLI is available.

The separate [web E2E README](../e2e-web/README.md) covers Playwright setup
and general smoke tests. Those tests run independently of this runner.
