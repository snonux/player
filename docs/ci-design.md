# CI Pipeline Design Proposal

**Status:** Proposal / Decision gate — requires user approval before implementation.
**Date:** 2026-05-18
**Scope:** `codeberg.org/snonux/player` monorepo — `player-server/` (Go) and `player-android/` (Flutter)

---

## Summary of Recommendations

| Question | Decision |
|----------|----------|
| Provider | Woodpecker CI (primary); GitHub Actions optional mirror |
| Matrix | Go on Linux only; Flutter on Linux only |
| Stages | Unit (fast gate) → Integration + Web smoke (parallel) → LLM e2e (nightly, guarded) |
| Emulator | Deferred — `flutter analyze` + `flutter test` only (per c6 approval) |
| Secrets | Woodpecker project secrets; LLM jobs on protected branch only |
| Caching | Go module + build cache via Woodpecker volumes; Flutter pub cache via volume |

---

## 1. Provider

### The choice: Woodpecker CI on Codeberg

The repository lives at `codeberg.org/snonux/player`. Codeberg's native CI system is
Woodpecker CI, which reads pipeline definitions from `.woodpecker/` YAML files. Using
Woodpecker as the primary CI system is the path of least friction:

- No token-based mirroring required for basic push/PR triggers.
- Codeberg's shared Woodpecker runners are available for free-tier public repos.
- Configuration is near-identical to GitHub Actions conceptually (steps inside jobs
  inside pipelines) so migration later is low-cost.

### Trade-offs vs GitHub Actions

| Factor | Woodpecker CI | GitHub Actions |
|--------|---------------|----------------|
| Native Codeberg integration | Yes (no mirror needed) | Requires mirror push |
| Shared runner availability | Codeberg shared pool | Generous free tier |
| KVM for Android emulator | Not guaranteed on shared runners | Available on `ubuntu-latest` |
| Ecosystem / community docs | Smaller but sufficient | Very large |
| Cost at current scale | Free (shared pool) | Free (within monthly quota) |
| Self-hosted runner support | Yes (same agent) | Yes |

### Recommendation

Use **Woodpecker CI as the sole provider** for now. The pipeline files live in
`.woodpecker/`. Do not add a GitHub Actions mirror until there is a concrete reason
(e.g., emulator tests that require GitHub's KVM-enabled runners, or GitHub Packages
publishing). Adding both providers simultaneously doubles maintenance without adding
value at this stage.

If the emulator decision changes later and Codeberg shared runners cannot provide KVM,
revisit a targeted GitHub Actions mirror for Android integration tests only — keeping
the Go and Flutter unit pipelines on Woodpecker.

---

## 2. Matrix

### Go server: Linux only

The Go server (`player-server/`) targets Linux (Docker image, k8s deployment). There
is no Windows or macOS deployment path. Adding macOS/Windows to the test matrix would
increase CI time and cost without testing anything relevant to production.

The Go standard library and `modernc.org/sqlite` (pure-Go SQLite port used here) are
cross-platform, but that portability is incidental. Tests should run on the same OS
family as the deployment target.

**Decision: Linux only.** Single-OS matrix for Go.

### Flutter Android: Linux only

`flutter test` for unit tests runs on any OS with the Flutter SDK installed. The
Android SDK and emulator are deferred (see Section 4). Linux suffices for
`flutter analyze` and `flutter test`.

**Decision: Linux only.** Single-OS matrix for Flutter. Revisit if macOS becomes
necessary for real device testing (e.g., iOS), which is out of scope for this project.

### Go architecture: amd64 only

The f3s homelab's Raspberry Pi 3 nodes are aarch64, but they are not suitable CI
runner targets (arm64 cannot run an x86_64 Android emulator, and Go cross-compilation
testing is a different concern from functional testing). The Beelink S12 Pro nodes
(Intel N100, x86_64) are viable as self-hosted runners but are not needed at this
stage — shared Codeberg runners handle the load.

---

## 3. Pipeline Stages

The pipeline has four stages with clear entry conditions and failure semantics.

### Stage 1 — Unit (fast gate)

**Trigger:** every push and pull request to any branch.

**Jobs (run in parallel):**

| Job | Command | Working dir | Estimated duration |
|-----|---------|-------------|--------------------|
| `server-unit` | `go test -race -count=1 ./...` | `player-server/` | ~10–15 s |
| `android-unit` | `flutter analyze && flutter test` | `player-android/` | ~2–3 min |

**Gate semantics:** Both jobs must pass before any downstream stage starts. A failure
in either blocks integration and smoke stages. This keeps the feedback loop fast: the
Go suite returns in under 15 seconds, so developers see a pass/fail signal on push
within two minutes (dominated by Flutter SDK setup).

**Why this order matters:** Unit tests have no external dependencies (no running server,
no network, no database file). They can be parallelised freely and fail fast. Making
them mandatory before anything else prevents wasted time running integration tests
against broken code.

### Stage 2 — Integration + Web smoke (parallel, after Stage 1)

**Trigger:** push to `main`; pull requests targeting `main`.

Not every feature branch needs integration tests on every push. Scoping this to PRs
targeting `main` (and pushes to `main`) strikes the right balance: fast feedback on
unit tests during development, thorough integration checking at merge time.

**Jobs (run in parallel after Stage 1 passes):**

#### Job: `server-integration`

Runs the Go integration tests against a real SQLite database.

```
go test -race -count=1 -tags=integration ./...
```

Working dir: `player-server/`
Estimated duration: ~20–30 s (full API surface including auth methods and bulk sync).
No running server process needed — integration tests start their own in-process server.

#### Job: `web-smoke`

Runs the Playwright smoke suite against a real running server with test media.

```
# Start server with test database and testdata/media
MEDIA_ROOT=./testdata/media SECURE_COOKIES=false DB_PATH=/tmp/player-e2e-ci.db \
  ./player &

# Wait for server readiness (done inside the Playwright globalSetup hook)
cd test/e2e-web
npm ci
npm run install-browsers
CI=true npm test
```

Working dir: `player-server/`
Estimated duration: ~3–5 min (Chromium install ~1 min, suite ~2–3 min).
Requires building the server binary first (add a build step before the server is
started, or use a compiled artifact from Stage 1's build output if the runner persists
workspaces).

**Trade-off for web-smoke:** The Playwright suite requires a running server, a real
SQLite database, and Chromium. This makes it heavier than unit tests and unsuitable
as a universal gate. Running it only on PRs to `main` and on `main` itself is the
right scope. The suite is Chromium-only by design (see `playwright.config.ts` —
adding Firefox/WebKit is deferred until cross-browser compatibility is a stated
requirement).

### Stage 3 — LLM e2e (nightly, cost-guarded)

**Trigger:** nightly cron on `main`; optionally on manual dispatch only.

This stage covers any LLM-assisted end-to-end tests (currently a future concern, but
included in the design to answer the secrets and cost questions explicitly).

**Structure:**

- Runs only on `main` branch (not on PRs).
- Guarded by a branch protection check so secrets are not exposed to untrusted forks.
- A per-run cost estimate should be logged to the job output so spend is visible.

**Estimated cost:** Depends on the LLM API used and test volume. A simple benchmark:
if the suite makes 50 API calls at GPT-4o pricing (~$0.005/call), one nightly run
costs ~$0.25. This is low but should be logged and monitored as the suite grows.

**Cost-control mechanism:** Set a hard token/call budget in the test harness. If the
budget is exceeded, the test fails rather than silently consuming quota. The nightly
job should emit a cost summary line that can be parsed by a monitoring step.

**Current state:** No LLM e2e tests exist yet. This stage is a placeholder in the
pipeline design so the implementation task (Section 6) wires it in correctly from
the start. For now, the nightly job can be a stub that exits 0 until real tests
are added.

---

## 4. Emulator Job

Per the c6 decision (captured in `player-android/docs/testing-strategy.md`):

- The Android CI job runs `flutter analyze` and `flutter test` only.
- No emulator, no KVM, no `flutter drive`, no `integration_test` scaffolding.
- This decision holds until real navigation (a second named route) lands in the app.

The emulator question is therefore **deferred**. The pipeline design accommodates this
by keeping Stage 1's `android-unit` job emulator-free and making no assumptions about
KVM availability on the Woodpecker shared runners.

When the emulator decision is revisited (concrete trigger: first real multi-screen
navigation is implemented), the choices are:

1. **GitHub Actions KVM runners** — `ubuntu-latest` provides KVM; add a targeted
   GitHub Actions workflow for Android integration tests while keeping everything else
   on Woodpecker.
2. **f3s homelab Beelink x86 self-hosted runner** — Intel N100 supports VT-x; a
   self-hosted Woodpecker agent on one of the bhyve VMs could provide KVM. Setup
   cost is high (Android SDK, Flutter SDK, emulator AVD, runner agent maintenance).
   Viable long-term if cloud CI minutes become a concern.
3. **Raspberry Pi 3 nodes** — not viable for x86_64 Android emulator; ruled out.

**For now:** no emulator job. The `android-unit` step in Stage 1 is the full extent
of Android CI.

---

## 5. Secrets

### Where secrets live

Woodpecker CI stores secrets at the project level (per-repository) via the Woodpecker
UI or API. Secrets are injected as environment variables into pipeline steps that
explicitly request them via the `secrets:` key in the step definition.

For this project, two categories of secrets exist:

#### Category A — Always available (build-time only)

No secrets are needed for the Go or Flutter unit/integration tests. These jobs use
only in-repo code, no network APIs, and no external services. Category A is empty
at this stage.

#### Category B — LLM API keys (nightly only, protected branch only)

LLM API keys (e.g., `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`) are required only for
the Stage 3 LLM e2e job.

**Access control:**

- Define the secret in Woodpecker with the "Protected" flag enabled. Woodpecker's
  protected secret mode restricts the secret to pipelines running on the repository's
  own pushes (not pull requests from forks). This prevents a malicious PR from
  exfiltrating the key by adding a print step.
- The LLM e2e job should additionally check `CI_COMMIT_BRANCH == "main"` as a
  belt-and-suspenders guard; if not on `main`, the job skips rather than errors.
- The nightly cron trigger is scoped to `main`, reinforcing the branch guard.

**Why not GitHub Actions secrets:** If a GitHub Actions mirror is added later,
GitHub's `secrets` context provides an equivalent mechanism with the same fork
protection semantics. The design is portable.

**No secrets in `.woodpecker/` YAML files.** All sensitive values come from the
Woodpecker secret store. The YAML files are committed to the repository and must
never contain raw keys.

---

## 6. Caching

Caching reduces CI setup time significantly. The two dominant costs are Go module
download and Flutter SDK + pub package download.

### Go module cache

Woodpecker supports Docker volume mounts between pipeline steps. The Go module cache
lives at `$GOPATH/pkg/mod` (typically `/root/go/pkg/mod` in the CI container).

**Mechanism:** Use a named Woodpecker volume (e.g., `go-mod-cache`) mounted at
`/root/go/pkg/mod`. Steps that run `go test` or `go build` populate the cache
on first run; subsequent runs reuse it. The build cache (`$GOCACHE`, typically
`/root/.cache/go-build`) should also be persisted via a named volume
(`go-build-cache`).

**Cache invalidation:** Go modules are content-addressed. No explicit invalidation is
needed — `go.sum` ensures the right versions are fetched. The volume accumulates
entries over time; a periodic volume purge (manual, or on a monthly timer) keeps disk
usage bounded.

**Estimated savings:** Go module download for this project (~15 deps) takes ~20–30 s
on a cold cache. With a warm cache, `go test` starts in under 2 s.

### Flutter pub cache

Flutter's package cache lives at `$PUB_CACHE` (typically `$HOME/.pub-cache`). A
named Woodpecker volume mounted at `/root/.pub-cache` provides the same benefit as
the Go module cache.

The Flutter SDK itself is larger (~500 MB). If the CI runner image does not bundle
Flutter, the SDK download dominates. Options:

1. **Pre-built Docker image with Flutter SDK baked in** — eliminates SDK download on
   every run. Maintain a `Dockerfile` in the repo (or use a community image like
   `cirrusci/flutter:stable`) as the CI container image.
2. **Volume cache for the SDK** — mount the SDK installation directory as a named
   volume. Works but is more brittle (SDK version pinning requires cache invalidation
   on version bump).

**Recommendation:** Use a pre-built Flutter Docker image for the `android-unit` job.
The `pubspec.lock` is committed, so `flutter pub get` with a warm pub cache runs in
under 5 s.

### SQLite test fixtures

The Go integration tests use `:memory:` SQLite databases opened fresh per test
(`newTestStore` in `sqlite_test.go`). There are no persistent fixture files to cache —
each test is hermetic by design.

The Playwright smoke tests use a temporary file at `/tmp/player-e2e-ci.db`. This file
is discarded at the end of the job. No caching applies.

**No SQLite fixture caching is needed.** The hermetic design is correct and must be
preserved.

### Playwright browser cache

Chromium installation via `npm run install-browsers` downloads ~150 MB. This can be
cached by persisting the Playwright browser directory
(`/root/.cache/ms-playwright`). A named Woodpecker volume or a cache key based on
the `@playwright/test` version in `package-lock.json` handles this.

**Mechanism:** Named volume `playwright-chromium` mounted at
`/root/.cache/ms-playwright`. The `install-browsers` step becomes a no-op when the
binary is already present.

### Cache summary table

| Cache | Volume name | Path | Key / Invalidation |
|-------|-------------|------|--------------------|
| Go modules | `go-mod-cache` | `/root/go/pkg/mod` | Content-addressed; no explicit invalidation |
| Go build | `go-build-cache` | `/root/.cache/go-build` | Content-addressed; periodic purge |
| Flutter pub | `flutter-pub-cache` | `/root/.pub-cache` | `pubspec.lock` change |
| Flutter SDK | Baked into Docker image | — | Image rebuild on SDK version bump |
| Playwright Chromium | `playwright-chromium` | `/root/.cache/ms-playwright` | `@playwright/test` version change |

---

## 7. Concrete Pipeline Shape

The following is a prose description of the `.woodpecker/` YAML structure. This
section describes *what to implement*, not the YAML itself (implementation is a
follow-up task).

### Files

```
.woodpecker/
├── server.yml        # Go server pipeline: unit, integration, web-smoke
├── android.yml       # Flutter Android pipeline: analyze + unit
└── nightly.yml       # LLM e2e pipeline: nightly cron on main
```

Three separate pipeline files avoid coupling the Go and Flutter pipelines. Each file
declares its own trigger conditions and can be enabled/disabled independently.

### `server.yml` trigger conditions

- Push to any branch: runs Stage 1 (unit) only.
- Push to `main` or PR targeting `main`: runs Stage 1, then Stage 2 (integration +
  web-smoke) after Stage 1 passes.

### `android.yml` trigger conditions

- Push to any branch touching `player-android/**`: runs `flutter analyze` and
  `flutter test`.
- Path-scoped trigger so Go-only changes do not trigger the Flutter runner.

### `nightly.yml` trigger conditions

- Woodpecker cron schedule: `0 2 * * *` (02:00 UTC daily, on `main`).
- Manual dispatch (Woodpecker's `manual` trigger) for ad-hoc runs.
- Protected secret access: LLM API keys only injected on `main`.

### Dependency graph

```
Push to any branch:
  [server-unit] ─┐
  [android-unit] ─┤  (parallel, independent)
                  └─ done

Push to main / PR to main:
  [server-unit] ─────────────────────────────────────────────────┐
  [android-unit] ─────────────────────────────────────────────────┤
                                                                   │ (both pass)
                                             ┌─────────────────────┘
                              [server-integration] ──┐ (parallel)
                              [web-smoke] ────────────┘

Nightly (main only):
  [server-unit] ──────────────────────────┐
                                          │ (passes)
                                 [llm-e2e] (with LLM API keys)
```

---

## 8. Honest Trade-offs and Risks

### Risk: Codeberg shared runner availability

Codeberg's shared Woodpecker runners are a shared pool. Under load, jobs may queue.
This is acceptable for a personal/hobby project but could become frustrating during
active development sprints.

**Mitigation:** The f3s homelab Beelink x86 nodes can run a self-hosted Woodpecker
agent. If queue times become a problem, registering one of r0/r1/r2 as a self-hosted
runner is a straightforward solution — the runner agent is a single Go binary.

### Risk: Flutter SDK version drift

Pinning Flutter SDK via a Docker image means rebuilding the image when Flutter
releases a new version. If the image is stale, CI uses an old SDK that may not match
local development.

**Mitigation:** Use a CI image tagged to a specific Flutter version (e.g.,
`ghcr.io/cirruslabs/flutter:3.x.y`) and update the tag in the pipeline YAML when
bumping Flutter locally. This is a one-line change.

### Risk: Web smoke test flakiness

The Playwright suite is configured with `retries: 1` on CI and generous timeouts
(15 s navigation, 10 s action). Flakiness can still occur if the Go server takes
longer than expected to start or if testdata/media scanning is slow.

**Mitigation:** The Playwright `globalSetup` hook already waits for the server to
respond before tests run (`waitForServer` helper in `tests/helpers/server.ts`). The
server startup step in the pipeline YAML should also include a readiness probe
(`curl --retry 10 --retry-delay 1 http://localhost:8080/`) before launching the test
runner.

### Risk: LLM e2e cost creep

If the nightly LLM e2e suite grows without a cost budget, API spend can accumulate
unnoticed.

**Mitigation:** The test harness must log total tokens used and estimated cost per
run. Add a step that parses this and fails the job if cost exceeds a configured
threshold (e.g., $1.00/run). The threshold is a commit-level configuration value,
not a secret.

---

## 9. Follow-up Implementation Tasks

The following tasks should be created via `ask add` after this proposal is approved.
Each is scoped to produce one concrete artifact.

1. **Create `.woodpecker/server.yml`** — Go server pipeline: Stage 1 unit tests on
   every push; Stage 2 integration + web-smoke on `main` and PRs to `main`. Include
   Go module and build cache volumes. Include server readiness probe before
   Playwright.

2. **Create `.woodpecker/android.yml`** — Flutter Android pipeline: `flutter analyze`
   and `flutter test` on every push touching `player-android/**`. Use a pinned
   Flutter Docker image. Mount Flutter pub cache volume.

3. **Create `.woodpecker/nightly.yml`** — Nightly LLM e2e stub pipeline: runs
   `0 2 * * *` on `main`, injects LLM API key from protected Woodpecker secret,
   currently exits 0 as a placeholder. Add cost logging step.

4. **Build and publish a Flutter CI Docker image** — `Dockerfile.flutter-ci` in the
   repo root pinning Flutter SDK version; publish to Codeberg Container Registry or
   GitHub Container Registry. Update `android.yml` to use this image.

5. **Register a self-hosted Woodpecker runner on f3s (optional)** — If shared runner
   queue times become a problem, register one of the bhyve VMs (r0/r1/r2) as a
   self-hosted agent. Document the setup in `docs/ci-runner-setup.md`.

6. **Add Playwright Chromium cache volume to `server.yml`** — Named volume for
   `/root/.cache/ms-playwright`; invalidated by `@playwright/test` version. This is
   a separate task from task 1 because it requires testing cache hit behaviour.
