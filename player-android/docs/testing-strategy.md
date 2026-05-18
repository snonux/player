# Android Testing Strategy — Proposal

**Status:** Proposal / Decision gate — requires user approval before implementation.
**Date:** 2026-05-18
**Scope:** `player-android/` Flutter skeleton (models, API client stub, stub UI)

---

## 1. Current State

The skeleton contains:

- `lib/models/` — nine Dart data classes with `fromJson`/`toJson`
- `lib/api/player_api_client.dart` — method signatures, all bodies are `throw UnimplementedError()`
- `lib/main.dart` — a single-screen `MaterialApp` stub
- `test/models_test.dart` — unit tests for every model (round-trip + defaults), plus `PlayerApiClient` constructor tests

There is no CI pipeline for the Flutter side yet, no widget tests, and no integration tests. The existing test file runs with `flutter test` (no device needed). `flutter analyze` is expected to pass cleanly; this should be confirmed before the CI task is implemented so the pipeline does not fail on the first run.

---

## 2. Question 1 — Scope at Skeleton Stage

**Should we add integration_test scaffolding now, or stay unit/widget only?**

### Option A — Unit + widget tests only (recommended)

Keep the test surface matched to what actually exists. Right now every meaningful testable unit is a data class. The API client has no real implementation to exercise. The UI is a single stub screen. Adding `integration_test/` at this stage means writing scaffolding for `UnimplementedError` stubs that immediately throw — which tests nothing and creates false confidence.

Widget tests for `main.dart` are marginally useful (smoke-test that the app compiles and the root widget renders) but that coverage is thin. They are quick to add when the first real screen lands.

**Recommendation:** Unit tests only now. Add widget tests when `main.dart` renders content from a real API response (i.e., the first route that reacts to live data is implemented). Add `integration_test/` scaffolding only when there is actual multi-screen navigation or user interaction to exercise — concretely, when a second named route is added to the app. Do not treat this as "Phase 4" in the abstract; treat it as a concrete code change that triggers the next test investment.

### Option B — integration_test scaffolding now

Adds boilerplate `integration_test/app_test.dart` that launches the app on an emulator and verifies a single string ("Player Android") appears. This does have real value: it exercises the Flutter/Gradle build chain, Gradle plugin wiring, and app startup — all of which can fail silently in a skeleton that otherwise only runs `flutter test`. However, it requires a running emulator (KVM on CI), which is the concern addressed separately in Question 2. If the emulator decision were free, this would be worth doing. Since it is not free, the trade-off goes the other way.

**Verdict:** Defer. The build-chain validation argument is valid but not strong enough to justify emulator setup on a skeleton. Revisit alongside the emulator decision when the first real screen lands.

---

## 3. Question 2 — Emulator on CI

**Three sub-options:** (a) KVM-enabled CI runner (GitHub Actions / Codeberg / Gitea cloud), (b) defer entirely until real app, (c) use the f3s homelab as a self-hosted runner.

### Sub-option A — Cloud CI with KVM

`flutter test` for unit tests needs no emulator and no KVM. Only `flutter drive` / `integration_test` requires a running Android emulator, which in turn requires KVM (hardware virtualisation) inside the CI runner.

- GitHub Actions offers `ubuntu-latest` runners with KVM available (enabled via the `enable-kvm` step in workflow YAML — confirmed available as of early 2026). An integration test pass on an Android emulator typically costs **8–15 CI minutes** per run (emulator boot ~3–4 min, tests ~1–2 min, overhead ~1–2 min).
- For Codeberg CI (Woodpecker), KVM availability depends on runner configuration — not guaranteed on shared runners.
- For unit-only `flutter test`: roughly **2–3 CI minutes** (Flutter setup ~1 min, pub get ~30 s, test run ~10–20 s).

### Sub-option B — Defer emulator until real app (recommended)

Since integration tests are deferred (Question 1), the emulator question is moot for now. `flutter test` for unit tests does not need an emulator. Defer the KVM decision until integration tests are actually written.

**Estimated CI cost (unit tests only):** 2–3 minutes per run.

### Sub-option C — f3s Homelab as self-hosted runner

The Beelink S12 Pro nodes (f0–f3) run Intel N100 CPUs which support VT-x/VT-d. The Rocky Linux bhyve VMs (r0–r2) could in principle run a self-hosted Flutter/Android CI runner with KVM passthrough, though nested virtualisation (bhyve guest running KVM) adds complexity and is not currently configured.

The Raspberry Pi 3 nodes (pi0–pi3) are `aarch64` — they cannot host an x86_64 Android emulator. They are not suitable for this purpose.

The Beelink x86 nodes are more capable, but:

- Setting up a self-hosted runner with Android SDK, Flutter SDK, and KVM emulator is a multi-hour setup task.
- The homelab is a personal cluster; burdening it with a long-running CI workload for a skeleton app is disproportionate.
- Self-hosted runners require ongoing maintenance (SDK updates, runner agent updates).

**Verdict:** Not worth setting up at skeleton stage. Revisit if the app becomes active and cloud CI minutes are a concern.

### Recommended CI approach

For now: a simple cloud CI job that runs `flutter analyze` and `flutter test`. No emulator, no KVM, no self-hosted runner. When integration tests land, revisit whether to use GitHub Actions KVM runners or a homelab x86 node.

**Open decision — CI platform:** The monorepo currently has no CI pipeline for either subproject. The CI platform (GitHub Actions, Codeberg Woodpecker, or another) should be agreed before the implementation task (Section 8, Task 1) is started. The choice does not change any other decision in this document; it only affects workflow file syntax. This is an explicit open item to resolve at approval time.

---

## 4. Question 3 — integration_test vs Patrol

**Patrol** (by LeanCode) is a Flutter testing library layered on top of the standard `integration_test` package. It adds:

- `patrolTest` / `PatrolIntegrationTester` — richer assertions
- Native interaction layer (tapping system dialogs, permission prompts, notification shade) — this is Patrol's primary differentiator
- `patrol` CLI for running tests

**Trade-offs:**

| Factor | integration_test (stdlib) | Patrol |
|--------|--------------------------|--------|
| Native dialog handling | No (requires `flutter_driver` workarounds) | Yes (core feature) |
| Assertion DSL / test isolation | Basic `expect` + widget finders | Richer `PatrolIntegrationTester`; better test isolation semantics |
| Extra dependency | No | Yes (`patrol`, `patrol_cli`) |
| Setup complexity | Low | Medium (Patrol CLI, Gradle config) |
| Community/docs | Flutter-official | Good but smaller |
| Needed at skeleton stage? | No | No |

**Recommendation:** Use standard `integration_test` if/when integration tests are added. Patrol's native dialog support matters for permission prompts (camera, storage, notifications) — the current Player Android app has none of those yet. Do not add Patrol until there is a concrete need for native dialog interaction. Adding a dependency to gain a capability that cannot be exercised is YAGNI.

---

## 5. Question 4 — LLM Access to Emulator

**Two modes:** (a) direct ADB/UIAutomator access (Claude or another LLM drives the emulator), (b) only `flutter test` exit codes visible to the LLM.

### Option A — Direct ADB/UIAutomator access

This would allow an LLM agent to inspect the UI, tap elements, and read screen content directly. It requires:

- A running emulator (or real device) reachable from the agent environment
- `adb` and `uiautomator` on the agent's path
- Screen-capture + OCR or accessibility tree traversal to interpret state

This is architecturally complex, slow to set up, and brittle (emulator timing, screen resolution). For a skeleton app with a single stub screen, there is nothing worth inspecting interactively.

### Option B — flutter test exit codes only (recommended)

The agent (Claude Code or equivalent) runs `flutter test` and `flutter analyze`, reads stdout/stderr, and interprets failures from text output. This is sufficient for all unit and widget tests. Exit codes plus human-readable failure output give full signal.

When integration tests land, `flutter drive` or `patrol test` also produces structured text output (pass/fail per test, stack traces). This is enough for an LLM to identify failures and suggest fixes without needing raw emulator access.

One legitimate future scenario is LLM-as-test-author rather than LLM-as-test-runner: an agent writing new integration tests might benefit from ADB/screenshot access to observe actual app state and produce correct assertions. That use case is real but does not justify the setup cost at skeleton stage, where there is no real app state to observe. When integration test authoring begins, this can be revisited as a separate tooling decision.

**Recommendation:** Exit codes + text output only. Do not set up ADB/UIAutomator access for LLM use. Revisit only if there is a concrete scenario where text output is insufficient (for example, screenshot-based visual regression testing or LLM-authored integration tests that need to observe dynamic UI state).

---

## 6. Chosen Approach (Summary)

| Dimension | Decision |
|-----------|----------|
| Test scope now | Unit tests only (`flutter test`, no device) |
| Widget tests | Add when first real screen lands |
| integration_test scaffolding | Defer until real navigation exists |
| CI runner | Cloud (platform TBD at approval — see open decision in Section 3), no emulator, no KVM |
| Homelab CI | Not now; revisit if cloud minutes become a concern |
| Test framework | Standard `flutter_test` + `integration_test` (stdlib); no Patrol until native dialogs needed |
| LLM access | `flutter test` exit codes + stdout only; no ADB/UIAutomator |

### Why this is the right call at skeleton stage

The Flutter project has nine model classes, one stub API client, and one stub screen. Every meaningful behaviour is in the JSON serialisation logic — which the existing unit tests already cover. Adding emulator infrastructure, integration test scaffolding, or LLM/ADB wiring before there is a real app to exercise would invert the effort-to-value ratio and create maintenance burden on a codebase that is still mostly `throw UnimplementedError()`.

The chosen approach keeps CI fast and cheap (2–3 minutes per run), keeps the dependency surface minimal, and defers complexity to the point where it pays for itself.

---

## 7. Estimated CI Cost

| Phase | What runs | Estimated duration |
|-------|-----------|--------------------|
| Now (skeleton) | `flutter analyze` + `flutter test` (unit only) | ~2–3 min/run |
| After first real screen | + widget tests (`flutter test`, no device) | ~3–4 min/run |
| After real navigation | + `flutter drive` on KVM-backed emulator (integration tests require a device) | ~10–18 min/run |
| After real navigation + native dialogs | + Patrol (replaces stdlib `integration_test`), KVM runner | ~12–18 min/run |

Note: `flutter drive` and `integration_test` unconditionally require a connected device or running emulator. There is no device-free mode for integration tests. The jump from row 2 to row 3 therefore coincides with the emulator-on-CI decision.

---

## 8. Follow-up Tasks to Create After Approval

The following tasks should be created via `ask add` once this proposal is approved:

1. **Add CI pipeline for player-android** — Create a Woodpecker (or GitHub Actions) workflow that runs `flutter analyze` and `flutter test` on push to main. No emulator. Trigger: any push touching `player-android/`.

2. **Add widget smoke test for main.dart** — When `main.dart` renders content derived from a real API response (i.e., the first route reacts to live data rather than showing a static stub), add `test/widget_test.dart` that pumps the root widget and verifies it renders without throwing. Concretely: when the second named route is added to the app, this task becomes active. The current single-screen `MaterialApp` stub is not a sufficient trigger. This task should be created alongside the first real UI implementation task.

3. **(Deferred) Add integration_test scaffolding** — Create after real navigation and user flows exist. Decide at that point whether Patrol is warranted based on whether permission dialogs are needed.

4. **(Deferred) KVM emulator CI evaluation** — Evaluate GitHub Actions KVM runner vs homelab x86 self-hosted runner when integration tests are ready. At that point, assess actual CI minute spend.

