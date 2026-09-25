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
  S01: 'scenario-S01.test.ts',
  S02: 'scenario-S02.test.ts',
  S03: 'scenario-S03.test.ts',
  S04: 'scenario-S04.test.ts',
  S05: 'scenario-S05.test.ts',
  S06: 'scenario-S06.test.ts',
  S07: 'scenario-S07.test.ts',
  S08: 'scenario-S08.test.ts',
  S09: 'scenario-S09.test.ts',
  S10: 'scenario-S10.test.ts',
  S11: 'scenario-S11.test.ts',
  S12: 'scenario-S12.test.ts',
  S13: 'scenario-S13.test.ts',
  S14: 'scenario-S14.test.ts',
  S15: 'scenario-S15.test.ts',
  S16: 'scenario-S16.test.ts',
  S17: 'scenario-S17.test.ts',
  S18: 'scenario-S18.test.ts',
  S19: 'scenario-S19.test.ts',
  S20: 'scenario-S20.test.ts',
  S21: 'scenario-S21.test.ts',
  S22: 'scenario-S22.test.ts',
  S23: 'scenario-S23.test.ts',
  S24: 'scenario-S24.test.ts',
  S25: 'scenario-S25.test.ts',
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

  if (expected === 0) return { passed: false, reason: collectFirstError(report) ?? 'No scenario tests passed (zero or skipped tests)' };
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
type TaskCommand = (args: string[], cwd: string) => SpawnSyncReturns<string>;

// Task titles stay one short line even when the reason is a multi-line
// Playwright error or a captured server log.
const MAX_TITLE_REASON_CHARS = 160;

/** Removes ANSI colour escapes that Playwright embeds in error messages. */
export function stripAnsi(text: string): string {
  return text.replace(/\u001b\[[0-9;]*m/g, '');
}

/** Returns the first non-empty line of reason, without colour, capped for a title. */
export function titleReason(reason: string): string {
  const line = stripAnsi(reason).split('\n').map(l => l.trim()).find(Boolean) ?? 'failed';
  return line.length > MAX_TITLE_REASON_CHARS ? `${line.slice(0, MAX_TITLE_REASON_CHARS - 1)}…` : line;
}

export function openAskTask(
  scenario: Scenario,
  reason: string,
  playwrightOutput: string,
  command: TaskCommand = (args, cwd) => spawnSync('ask', args, { cwd, encoding: 'utf8' }),
): boolean {
  const cwd = path.resolve(__dirname, '../../../..'); // player-server task scope
  const title = `E2E ${scenario.meta.id}: ${scenario.meta.title} — ${titleReason(reason)}`;
  const result = command(['add', '+testing', title], cwd);
  const id = result.stdout?.match(/^created task ([a-z0-9]+)\s*$/m)?.[1];
  if (result.status !== 0 || !id) {
    console.error(`[runner] Failed to open ask task: ${result.error?.message ?? result.stderr ?? result.stdout}`);
    return false;
  }
  const annotation = [
    'Agent workflow: read agent-task-management, applicable language best practices, SOLID guidance, and player-server/AGENTS.md before editing.',
    `Scenario: ${scenario.filePath}`,
    `Executable spec: test/e2e-web/tests/${scenarioSpec(scenario.meta.id)}`,
    'Reproduce using the isolated setup in test/e2e-llm/README.md, then run the selected scenario with PLAYER_SCENARIO_BINARY set.',
    `Failure after retry: ${stripAnsi(reason).slice(0, MAX_OUTPUT_CHARS)}`,
    // The JSON reporter starts with a long config preamble; results and
    // errors are at the end, so keep the tail.
    `Playwright output (last ${MAX_OUTPUT_CHARS} characters):\n${stripAnsi(playwrightOutput).slice(-MAX_OUTPUT_CHARS)}`,
  ].join('\n');
  const annotated = command(['annotate', id, annotation], cwd);
  if (annotated.status !== 0) {
    console.error(`[runner] Created task ${id}, but adding reproduction context failed: ${annotated.error?.message ?? annotated.stderr}`);
    return false;
  }
  return true;
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

  console.error(`[runner] FAIL  ${id}: ${title} — double-failure`);
  if (process.env.LLM_E2E_CREATE_TASKS !== 'false') {
    openAskTask(scenario, secondResult.reason, (second.stdout ?? '') + (second.stderr ?? ''));
  }
  return { status: 'failed', reason: secondResult.reason };
}

// ---------------------------------------------------------------------------
// Managed server setup check
// ---------------------------------------------------------------------------

function precheckManagedBinary(): void {
  const binary = process.env['PLAYER_SCENARIO_BINARY'];
  if (binary) {
    try { fs.accessSync(binary, fs.constants.X_OK); return; } catch { /* explain below */ }
  }
  console.error('[runner] PLAYER_SCENARIO_BINARY must point to an executable Player build.');
  console.error('[runner] See test/e2e-llm/README.md. Each scenario starts its own disposable server; do not start an external server.');
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

  // Missing setup should fail once, before spawning tests or creating tasks.
  if (scenarios.some(s => !s.meta.skip && scenarioSpec(s.meta.id))) precheckManagedBinary();

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
