# Player — LLM-Driven End-to-End Tests

This directory contains the LLM-driven end-to-end test harness for Player.
Unlike the deterministic Playwright suite at `../e2e-web/`, this layer uses a
Claude agent to interpret natural-language scenario steps, execute them against
a live server, and judge the results. It is designed for cross-stack flows that
are too complex to express as sequences of CSS selectors.

For full background and design rationale, read
[`docs/llm-e2e-design.md`](../../../../docs/llm-e2e-design.md) at the project
root.

---

## Directory layout

```
test/e2e-llm/
├── README.md              # this file
├── runner/
│   ├── package.json       # Node/TypeScript runner dependencies
│   ├── tsconfig.json      # TypeScript configuration
│   └── index.ts           # harness entry point
└── scenarios/
    ├── S01-bootstrap.md   # Bootstrap → first user
    ├── S02-podcast.md     # Podcast subscribe → download → mark complete
    ├── S03-upload.md      # Upload via API → verify on web
    └── S04-share-link.md  # Share-link round-trip
```

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Node.js | 18+ | Required for the runner |
| npm | 8+ | Required for the runner |
| Playwright (Chromium) | Latest via Playwright | Install via `../e2e-web/` setup |
| `ask` CLI | Any | Installed globally; used for failure tasks |
| `sqlite3` CLI | Any | For DB assertion checks |
| `ANTHROPIC_API_KEY` | Valid key | Required for screenshot oracle (see below) |

The Player server must be running before any scenario is started. The harness
does not start or stop the server.

### First-time runner setup

```sh
# From this directory:
cd runner
npm install
npm run build
```

---

## Starting the server

The harness defaults to `http://localhost:8080`. Override with `PLAYER_URL`.

Start the server from `player-server/` so it can resolve embedded static assets:

```sh
# From player-server/ — use testmedia/ as the media library:
MEDIA_ROOT=./testmedia \
SECURE_COOKIES=false \
DB_PATH=/tmp/player-e2e-llm.db \
./player
```

Key server environment variables:

| Variable | Value | Reason |
|---|---|---|
| `MEDIA_ROOT` | `./testmedia` | Provides pre-existing media for upload/verify scenarios |
| `SECURE_COOKIES` | `false` | Allows session cookie over plain HTTP |
| `DB_PATH` | `/tmp/player-e2e-llm.db` | Isolates the test database from production |

---

## Environment variables (harness)

| Variable | Required | Default | Description |
|---|---|---|---|
| `PLAYER_URL` | No | `http://localhost:8080` | Base URL of the Player server under test |
| `LLM_E2E_SCREENSHOTS` | No | `false` | Set to `true` to enable Layer 5 screenshot + vision model assertion |
| `OLLAMA_BASE_URL` | No | `https://ollama.com` | Ollama API endpoint; override for a local instance |
| `OLLAMA_API_KEY` | Yes (for cloud) | — | API key from ollama.com; omit only for local unauthenticated instances |
| `OLLAMA_MODEL` | No | `qwen3-vl:235b-instruct` | Vision model on the cloud endpoint; override for a local model |
| `LLM_E2E_OPEN_ISSUE` | No | `false` | Set to `true` to open a Codeberg issue if a failure task is older than 24 h |
| `PLAYER_DB` | No | Inferred from server config | Path to the SQLite database file; used for DB assertion checks |

---

## Running all scenarios

```sh
# From runner/ (after npm run build):
node dist/index.js

# Or run directly during development:
npm run dev
```

This runs S01 through S04 in order. Each scenario is independent; a failure in
one scenario does not stop the remaining scenarios from running.

To run against a non-default server:

```sh
PLAYER_URL=http://myserver:9090 node dist/index.js
```

To enable screenshot assertions (Layer 5, requires an ollama.com API key):

```sh
LLM_E2E_SCREENSHOTS=true \
  OLLAMA_API_KEY=<your-ollama.com-key> \
  node dist/index.js
```

The oracle defaults to `llama3.2-vision` via `https://ollama.com`. To use a
different model or a local Ollama instance:

```sh
# Different cloud model:
LLM_E2E_SCREENSHOTS=true OLLAMA_API_KEY=<key> OLLAMA_MODEL=llava:13b node dist/index.js

# Local Ollama (no API key needed):
LLM_E2E_SCREENSHOTS=true OLLAMA_BASE_URL=http://localhost:11434 node dist/index.js
```

---

## Running a single scenario manually

Pass the scenario file path as the first argument:

```sh
# From runner/:
node dist/index.js ../scenarios/S01-bootstrap.md
```

Or via `npm run dev` during development:

```sh
npm run dev -- ../scenarios/S01-bootstrap.md
```

The runner prints a per-step summary to stdout and exits with code 0 on
success, 1 on failure.

---

## Adding a new scenario

Each scenario is a single Markdown file under `scenarios/`. The file has two
parts: a YAML front-matter block and a numbered Markdown step list.

### File name convention

```
S<nn>-<short-slug>.md
```

Examples: `S05-android-skeleton.md`, `S06-bulk-sync.md`.

### Skeleton

```markdown
---
id: S05
title: "Short human-readable title"
tags: [auth, web]          # free-form tags for filtering
preconditions:
  server_state: running     # "fresh" = empty DB, "running" = existing DB with data
  fixtures: []              # list of fixture keys the harness should load
assertions:
  - db: "SELECT count(*) FROM users WHERE role='admin'"
  - url_contains: /login.html
skip: false                 # set to "until-<reason>" to exclude from CI runs
---

1. Navigate to the root URL.
2. Confirm the page redirects to /expected-page.html.
3. Do something meaningful with the UI or API.
4. Confirm the result.
```

### Front-matter fields

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique identifier, e.g. `S05` |
| `title` | string | Human-readable title used in logs and `ask` task names |
| `tags` | list | Free-form tags; not currently used for filtering but useful for documentation |
| `preconditions.server_state` | `fresh` or `running` | `fresh` means the harness creates a temp empty DB; `running` means it uses the existing one |
| `preconditions.fixtures` | list | Named fixture keys the harness loads before running steps |
| `assertions` | list | Post-condition checks the harness runs after all steps complete |
| `skip` | bool or string | `false` to run normally; a string like `"until-integration-test-added"` to skip in CI |

### Assertion types

| Key | Example | What it checks |
|---|---|---|
| `db` | `db: "SELECT id FROM users WHERE role='admin'"` | Runs a read-only SQLite query; passes if at least one row is returned |
| `url_contains` | `url_contains: /login.html` | Checks the browser's current URL |
| `selector_visible` | `selector_visible: ".media-card"` | Playwright DOM check |
| `status_code` | `status_code: "GET /api/v1/health 200"` | HTTP status assertion |

### Writing good steps

- Write steps as numbered plain-English sentences. The agent interprets them; no
  special syntax is needed inside the Markdown body.
- Each step should do one thing: navigate, fill a form, call an API, or assert.
- Avoid writing assertions in the steps if you can express them in the YAML
  `assertions` block — the YAML block is cheaper (no agent reasoning required).
- Visual checks ("does this look right?") belong in steps, not in the YAML block;
  they trigger the Haiku screenshot oracle when `LLM_E2E_SCREENSHOTS=true`.

---

## Assertion layers and cost

The harness uses layered assertions from cheapest to most expensive. Use the
cheapest layer that can detect a failure:

| Layer | Method | Cost |
|---|---|---|
| 1 | HTTP status codes | Zero — checked on every API call |
| 2 | JSON field assertions | Cheap — parse response body |
| 3 | DB state checks (`sqlite3` CLI) | Cheap — direct SQL query |
| 4 | Playwright selector checks | Cheap — deterministic DOM query |
| 5 | Screenshot + Ollama vision check | Self-hosted cost only — gated behind `LLM_E2E_SCREENSHOTS=true` |

Layer 5 sends a PNG to an Ollama vision model (`llama3.2-vision`) with a yes/no
question such as "Is there a media card visible in the grid?" Using a self-hosted
Ollama instance means no per-call API cost beyond compute.

---

## Cost budget

| Run type | Scenarios | Estimated cost |
|---|---|---|
| Full nightly (S01-S04) | 4 | ~$0.18 per run |
| Single scenario | 1 | ~$0.04-0.05 per run |
| Full nightly with retries | 4 + retries | ~$0.27 per run |

At one nightly run the expected spend is **~$5.50/month** (Sonnet for
orchestration, Haiku for screenshots).

**Alert threshold:** If a single run exceeds **$0.50**, treat it as anomalous.
Common causes are a context window blowup (Playwright output not capped) or
excessive retries. The runner caps Playwright CLI output forwarded to the agent
at 4 000 characters to help control this.

**Monthly soft budget:** $10/month for the entire LLM e2e layer. If you
consistently see runs above $0.50, check for long scenario files, verbose
fixtures, or scenarios that retry repeatedly.

---

## Failure handling

The runner uses a one-retry policy:

```
Run scenario
├── Pass  → log "PASS", continue to next scenario
└── Fail
    ├── Wait 5 s, retry once (fresh browser context)
    │   ├── Pass  → log "FLAKY — passed on retry", continue
    │   └── Fail  → open ask task, log "FAIL", continue to next scenario
```

When a scenario fails after the retry, the runner opens a task:

```sh
ask add "LLM e2e failure: <scenario title> — <one-line reason>"
```

The task description includes the Playwright HTML report path and a snippet of
the failure output so a developer can reproduce the failure without re-reading
logs.

The runner does **not**:
- retry more than once (cost control)
- stop the run on a single failure (other scenarios still run)
- send email or Slack notifications
- auto-fix failing scenarios

To open a Codeberg issue automatically when a failure task is older than 24 h,
set `LLM_E2E_OPEN_ISSUE=true`. This is off by default.

---

## Nightly schedule

The nightly run is configured via the `/schedule` skill. It runs a Claude Code
agent with the prompt:

> Run the LLM e2e suite at `player-server/test/e2e-llm/` against the
> production server and report results.

The agent reads the scenario files, executes them via the runner, and annotates
an ongoing task with the result. To set up or modify the schedule, use:

```sh
/schedule
```

---

## Relationship to the Playwright smoke suite

| Dimension | Playwright smoke (`../e2e-web/`) | LLM e2e (this suite) |
|---|---|---|
| Assertion style | Deterministic CSS selectors | Natural-language steps + layered assertions |
| When to run | Every PR, fast | Nightly, slower |
| Best for | "Did the page render? Did the API return 200?" | Cross-stack flows, auth boundaries, multi-step sequences |
| Cost | Zero (no LLM calls) | ~$0.18/run |
| Visual checks | No | Yes, via Haiku (opt-in) |

The two suites are complementary. The Playwright smoke suite is the first line
of defence for regressions. The LLM e2e suite catches higher-level integration
failures that selector-based tests cannot easily express.
