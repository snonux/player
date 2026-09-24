/**
 * index.ts — Explicit scenario Playwright runner.
 *
 * Reads YAML front-matter + Markdown scenario files from ../scenarios/,
 * invokes the Playwright CLI (npx playwright test --reporter=json),
 * parses JSON output to determine pass/fail, and retries once on failure.
 * On double-failure it opens an `ask` task so the issue is tracked.
 *
 * Usage:
 *   node dist/index.js                    # run all scenarios
 *   node dist/index.js scenarios/S01.md  # run one scenario
 *
 * Only explicitly mapped scenario specs are run. Unmapped scenarios are
 * reported as unsupported, never as tested or passed.
 */

import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'js-yaml';
import { spawnSync, SpawnSyncReturns } from 'child_process';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Parsed YAML front-matter from a scenario file. */
interface ScenarioMeta {
  id: string;
  title: string;
  tags?: string[];
  skip?: string;         // non-empty string means skip; value is the reason
  preconditions?: Record<string, unknown>;
  assertions?: unknown[];
}

/** Holds the parsed content of a scenario file. */
interface Scenario {
  meta: ScenarioMeta;
  steps: string;         // raw Markdown body (the numbered steps)
  filePath: string;
}

/** Subset of the Playwright JSON reporter output that we care about. */
interface PlaywrightReport {
  stats: {
    expected: number;
    unexpected: number;
    skipped: number;
    flaky: number;
  };
  errors?: Array<{ message?: string }>;
  suites?: PlaywrightSuite[];
}

type ScenarioResult = { status: 'passed' | 'failed' | 'unsupported'; reason: string };

interface PlaywrightSuite {
  title: string;
  specs?: PlaywrightSpec[];
  suites?: PlaywrightSuite[];
}

interface PlaywrightSpec {
  title: string;
  ok: boolean;
  tests?: Array<{ status: string; results?: Array<{ error?: { message?: string } }> }>;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Playwright config is at test/e2e-web/ — three levels up from runner/dist/.
const PLAYWRIGHT_CONFIG = path.resolve(__dirname, '../../../e2e-web/playwright.scenarios.config.ts');

// Scenario files are at test/e2e-llm/scenarios/ — two levels up from runner/dist/.
const SCENARIOS_DIR = path.resolve(__dirname, '../../scenarios');

// Each entry is a dedicated executable implementation of that scenario's
// workflow. A smoke test is not evidence that an unrelated scenario passed.
export const SCENARIO_SPECS: Readonly<Record<string, string>> = {
  S05: 'scenario-S05.test.ts',
  S14: 'scenario-S14.test.ts',
};

export function scenarioSpec(id: string): string | undefined {
  return Object.hasOwn(SCENARIO_SPECS, id) ? SCENARIO_SPECS[id] : undefined;
}

// How long to wait between a first failure and the retry, in ms.
const RETRY_DELAY_MS = 5_000;

// Max characters of Playwright output forwarded in the ask task description.
// Keeps token costs manageable when the orchestrator reads the annotation.
const MAX_OUTPUT_CHARS = 4_000;

// ---------------------------------------------------------------------------
// Scenario file parsing
// ---------------------------------------------------------------------------

/**
 * parseFrontMatter splits a file into YAML front-matter and a Markdown body.
 * Front-matter is delimited by leading and trailing `---` lines.
 * Returns null if the file does not start with `---`.
 */
function parseFrontMatter(content: string): { meta: ScenarioMeta; steps: string } | null {
  const lines = content.split('\n');
  if (lines[0].trim() !== '---') return null;

  const closeIdx = lines.findIndex((l, i) => i > 0 && l.trim() === '---');
  if (closeIdx === -1) return null;

  const yamlText = lines.slice(1, closeIdx).join('\n');
  const body = lines.slice(closeIdx + 1).join('\n').trim();

  const meta = yaml.load(yamlText) as ScenarioMeta;
  if (!meta?.id || !meta?.title) {
    throw new Error(`Scenario front-matter missing required fields 'id' and 'title'`);
  }
  return { meta, steps: body };
}

/**
 * loadScenario reads and parses one scenario file.
 */
function loadScenario(filePath: string): Scenario {
  const content = fs.readFileSync(filePath, 'utf8');
  const parsed = parseFrontMatter(content);
  if (!parsed) {
    throw new Error(`${filePath}: does not start with YAML front-matter (--- delimiter)`);
  }
  return { ...parsed, filePath };
}

/**
 * discoverScenarios returns all .md files in SCENARIOS_DIR sorted by filename.
 * A specific file path can be passed to restrict the run to one scenario.
 */
function discoverScenarios(specificFile?: string): Scenario[] {
  if (specificFile) {
    // Resolve relative to cwd so callers can pass e.g. scenarios/S01.md
    const abs = path.isAbsolute(specificFile)
      ? specificFile
      : path.resolve(process.cwd(), specificFile);
    return [loadScenario(abs)];
  }

  if (!fs.existsSync(SCENARIOS_DIR)) {
    console.warn(`[runner] Scenarios directory not found: ${SCENARIOS_DIR}`);
    return [];
  }

  return fs
    .readdirSync(SCENARIOS_DIR)
    .filter(f => f.endsWith('.md'))
    .sort()
    .map(f => loadScenario(path.join(SCENARIOS_DIR, f)));
}

// ---------------------------------------------------------------------------
// Playwright invocation
// ---------------------------------------------------------------------------

/**
 * runPlaywright invokes only the dedicated spec for a supported scenario.
 */
function runPlaywright(spec: string): SpawnSyncReturns<string> {
  // The Playwright config is in e2e-web/; we run npx from there so that
  // node_modules/.bin/playwright is available without a separate install.
  const cwd = path.dirname(PLAYWRIGHT_CONFIG);

  return spawnSync(
    'npx',
    ['playwright', 'test', spec, '--reporter=json', '--config', PLAYWRIGHT_CONFIG],
    { cwd, encoding: 'utf8', maxBuffer: 10 * 1024 * 1024 },
  );
}

// ---------------------------------------------------------------------------
// Result parsing
// ---------------------------------------------------------------------------

/**
 * parseReport extracts pass/fail information from Playwright JSON reporter
 * output. Returns { passed, reason } where reason is populated on failure.
 */
export function parseReport(run: SpawnSyncReturns<string>): { passed: boolean; reason: string } {
  if (run.error) return { passed: false, reason: `Playwright could not start: ${run.error.message}` };
  if (run.signal) return { passed: false, reason: `Playwright terminated by ${run.signal}` };
  const stdout = run.stdout ?? '';
  let report: PlaywrightReport;
  try {
    // Try the full output first (clean JSON). When Playwright emits debug lines
    // before the JSON object, find the first newline-prefixed `{` instead —
    // this avoids false matches on `{` inside warning text.
    const trimmed = stdout.trim();
    const jsonStr = trimmed.startsWith('{')
      ? trimmed
      : (() => {
          const nlIdx = stdout.indexOf('\n{');
          return nlIdx !== -1 ? stdout.slice(nlIdx + 1) : '';
        })();
    if (!jsonStr) return { passed: false, reason: 'No JSON object in Playwright output' };
    report = JSON.parse(jsonStr) as PlaywrightReport;
  } catch {
    return { passed: false, reason: `Cannot parse Playwright JSON: ${stdout.slice(0, 200)}` };
  }

  if (!report.stats || !Number.isInteger(report.stats.expected) ||
      !Number.isInteger(report.stats.unexpected) || !Number.isInteger(report.stats.skipped)) {
    return { passed: false, reason: 'Playwright report has invalid statistics' };
  }
  const { unexpected, expected, skipped } = report.stats;

  if (expected === 0) return { passed: false, reason: 'No scenario tests passed (zero or skipped tests)' };
  if (skipped > 0) return { passed: false, reason: `${skipped} scenario test(s) skipped` };
  if (report.errors?.length) return { passed: false, reason: collectFirstError(report) ?? 'Playwright setup error' };
  if (run.status !== 0) return { passed: false, reason: collectFirstError(report) ?? `Playwright exited ${run.status}` };

  if (unexpected === 0) {
    return { passed: true, reason: '' };
  }

  // Collect the first error message from the report hierarchy for the reason.
  const reason = collectFirstError(report) ?? `${unexpected} test(s) failed`;
  return { passed: false, reason };
}

/**
 * collectFirstError walks the Playwright report tree to find the first
 * failure error message. Returns undefined if none is found.
 */
function collectFirstError(report: PlaywrightReport): string | undefined {
  // Top-level errors (e.g. global setup failures).
  if (report.errors && report.errors.length > 0) {
    return report.errors[0].message ?? undefined;
  }
  if (!report.suites) return undefined;

  // Walk suites recursively.
  const walkSuites = (suites: PlaywrightSuite[]): string | undefined => {
    for (const suite of suites) {
      if (suite.specs) {
        for (const spec of suite.specs) {
          if (!spec.ok && spec.tests) {
            for (const t of spec.tests) {
              if (t.results) {
                for (const r of t.results) {
                  if (r.error?.message) return r.error.message.slice(0, 300);
                }
              }
            }
          }
        }
      }
      if (suite.suites) {
        const found = walkSuites(suite.suites);
        if (found) return found;
      }
    }
    return undefined;
  };

  return walkSuites(report.suites);
}

// ---------------------------------------------------------------------------
// Failure handling
// ---------------------------------------------------------------------------

/**
 * openAskTask creates a tracked task via the `ask` CLI when a scenario fails
 * on both the initial run and its retry. The task title includes the scenario
 * title and the failure reason so it is actionable without further context.
 */
function openAskTask(scenario: Scenario, reason: string, playwrightOutput: string): void {
  const truncated = playwrightOutput.slice(0, MAX_OUTPUT_CHARS);
  const title = `+test-llm-e2e LLM e2e failure: ${scenario.meta.title} — ${reason}`;

  // Use spawnSync to avoid shell injection from scenario titles that contain
  // backticks, dollar signs, or quotes.
  const result = spawnSync('ask', ['add', title], { stdio: 'inherit' });
  if (result.status !== 0) {
    console.error(`[runner] Failed to open ask task for scenario ${scenario.meta.id}`);
  }

  // Print the truncated output so CI logs capture it even if ask is unavailable.
  console.error(`[runner] Playwright output (truncated to ${MAX_OUTPUT_CHARS} chars):`);
  console.error(truncated);
}

// ---------------------------------------------------------------------------
// Per-scenario run (with one retry)
// ---------------------------------------------------------------------------

/**
 * runScenario executes a scenario once, retries on failure after a short
 * delay, and calls openAskTask on double-failure.
 * Returns a distinct passed, failed, or unsupported result.
 */
function runScenario(scenario: Scenario): ScenarioResult {
  const { id, title, skip } = scenario.meta;

  if (skip) {
    console.log(`[runner] SKIP  ${id}: ${title} — ${skip}`);
    return { status: 'unsupported', reason: skip };
  }

  const spec = scenarioSpec(id);
  if (!spec) {
    const reason = 'no executable scenario spec';
    console.log(`[runner] UNSUPPORTED ${id}: ${title} — ${reason}`);
    return { status: 'unsupported', reason };
  }
  if (!fs.existsSync(path.join(path.dirname(PLAYWRIGHT_CONFIG), 'tests', spec))) {
    console.error(`[runner] FAIL  ${id}: ${title} — mapped spec ${spec} is missing`);
    return { status: 'failed', reason: `mapped spec ${spec} is missing` };
  }

  console.log(`[runner] RUN   ${id}: ${title}`);

  // First attempt.
  const first = runPlaywright(spec);
  const firstResult = parseReport(first);

  if (firstResult.passed) {
    console.log(`[runner] PASS  ${id}: ${title}`);
    return { status: 'passed', reason: '' };
  }

  console.warn(`[runner] FAIL  ${id}: ${title} — ${firstResult.reason}`);
  console.warn(`[runner] Retrying in ${RETRY_DELAY_MS / 1000}s…`);

  // Wait before retry to let transient server issues settle.
  // spawnSync('sleep') is a reliable synchronous pause that works on any POSIX
  // system (including the CI container) without relying on Atomics or timers.
  spawnSync('sleep', [String(RETRY_DELAY_MS / 1000)]);

  // Single retry.
  const second = runPlaywright(spec);
  const secondResult = parseReport(second);

  if (secondResult.passed) {
    console.log(`[runner] PASS  ${id}: ${title} (passed on retry — flaky)`);
    return { status: 'passed', reason: 'passed on retry' };
  }

  console.error(`[runner] FAIL  ${id}: ${title} — double-failure, opening ask task`);
  openAskTask(scenario, secondResult.reason, second.stdout ?? '');
  return { status: 'failed', reason: secondResult.reason };
}

// ---------------------------------------------------------------------------
// Server precheck
// ---------------------------------------------------------------------------

// PLAYER_URL is the base URL the harness uses (matches the README + Playwright
// config). Centralising the constant keeps the precheck and any future direct
// HTTP calls aligned with the rest of the harness.
const PLAYER_URL = process.env['PLAYER_URL'] || 'http://localhost:8080';

// How long to wait for /healthz before declaring the server missing. The
// Playwright suite waits up to 15 s per scenario internally; doing the same
// up-front means we surface a missing server as a single clear error instead
// of one Playwright failure (and one bogus ask task) per scenario.
const PRECHECK_TIMEOUT_MS = 15_000;
const PRECHECK_POLL_INTERVAL_MS = 200;

/**
 * waitForServer polls `${PLAYER_URL}/healthz` until it responds 2xx or the
 * deadline passes. Returns true if the server became healthy, false otherwise.
 * Using a synchronous loop (spawnSync sleep) keeps the runner's overall
 * control flow synchronous, matching how runScenario invokes Playwright.
 */
export function waitForServer(timeoutMs: number, playerURL = PLAYER_URL): boolean {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const remainingMs = Math.max(1, deadline - Date.now());
    // Use curl for the probe so we don't need to depend on a Node fetch
    // shim — keeps the harness usable on older Node where fetch is missing.
    // Bound a connected-but-stalled server to the same overall deadline.
    const probe = spawnSync(
      'curl',
      ['-fsS', '--max-time', (remainingMs / 1000).toFixed(3), '-o', '/dev/null',
        '-w', '%{http_code}', `${playerURL}/healthz`],
      { encoding: 'utf8', timeout: remainingMs },
    );
    if (probe.status === 0 && probe.stdout.trim().startsWith('2')) return true;
    const sleepMs = Math.min(PRECHECK_POLL_INTERVAL_MS, Math.max(0, deadline - Date.now()));
    if (sleepMs > 0) spawnSync('sleep', [String(sleepMs / 1000)]);
  }
  return false;
}

/**
 * precheckServer fails fast with a clear, actionable message if the server
 * is not reachable. Without this, each scenario would individually fail with
 * the same "did not become healthy" error and openAskTask would file one
 * spurious task per scenario — drowning the queue in duplicates of the same
 * root cause. Returning a non-zero exit before the scenario loop avoids both.
 */
function precheckServer(): void {
  if (waitForServer(PRECHECK_TIMEOUT_MS)) return;

  console.error(
    `[runner] ERROR: Player server at ${PLAYER_URL} is not reachable on /healthz ` +
      `after ${PRECHECK_TIMEOUT_MS / 1000}s.`,
  );
  console.error('[runner] Start it from player-server/ before running the e2e suite:');
  console.error(
    '[runner]   MEDIA_ROOT=./testdata/media SECURE_COOKIES=false ' +
      'DB_PATH=/tmp/player-e2e-llm.db ./player',
  );
  console.error('[runner] Override the target URL with PLAYER_URL.');
  process.exit(2);
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

/**
 * main discovers scenarios (or uses the one passed as argv[2]), runs each in
 * sequence, and exits non-zero if any scenario failed after its retry.
 */
function main(): void {
  const specificFile = process.argv[2];
  const scenarios = discoverScenarios(specificFile);

  if (scenarios.length === 0) {
    console.error('[runner] No scenario files found.');
    process.exit(2);
  }

  // Fail fast with a clear hint if the server is down. Without this the
  // runner would spawn Playwright per scenario, time out on each, and file
  // a duplicate ask task per failure — exactly what produced the legacy
  // "Server at http://localhost:8080 did not become healthy" task pile.
  if (scenarios.some(s => !s.meta.skip && scenarioSpec(s.meta.id))) precheckServer();

  console.log(`[runner] Running ${scenarios.length} scenario(s)…`);

  let failures = 0;
  let passed = 0;
  let unsupported = 0;
  for (const scenario of scenarios) {
    const result = runScenario(scenario);
    if (result.status === 'passed') passed++;
    if (result.status === 'failed') failures++;
    if (result.status === 'unsupported') unsupported++;
  }

  console.log(`\n[runner] Results: ${passed} passed, ${failures} failed, ${unsupported} unsupported.`);

  process.exit(failures > 0 ? 1 : 0);
}

if (require.main === module) main();
