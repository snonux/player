import assert from 'node:assert/strict';
import test from 'node:test';
import { SpawnSyncReturns } from 'node:child_process';
import { parseReport, scenarioSpec, SCENARIO_SPECS, waitForServer } from './index';
import { spawn } from 'node:child_process';
import { once } from 'node:events';

function run(expected: number, skipped = 0, status = 0, errors: unknown[] = []): SpawnSyncReturns<string> {
  return {
    stdout: JSON.stringify({ stats: { expected, unexpected: 0, skipped, flaky: 0 }, errors }),
    stderr: '', status, signal: null, error: undefined, pid: 1, output: [],
  };
}

test('mapped scenario IDs select distinct executable specs', () => {
  assert.equal(SCENARIO_SPECS.S05, 'scenario-S05.test.ts');
  assert.equal(SCENARIO_SPECS.S14, 'scenario-S14.test.ts');
  assert.notEqual(SCENARIO_SPECS.S05, SCENARIO_SPECS.S14);
  assert.equal(SCENARIO_SPECS.S01, undefined);
  assert.equal(scenarioSpec('constructor'), undefined);
  assert.equal(scenarioSpec('toString'), undefined);
});

test('only a successful nonempty report can pass', () => {
  assert.equal(parseReport(run(1)).passed, true);
  assert.equal(parseReport(run(0)).passed, false);
  assert.equal(parseReport(run(1, 1)).passed, false);
  assert.equal(parseReport(run(1, 0, 1)).passed, false);
  assert.equal(parseReport(run(1, 0, 0, [{ message: 'setup failed' }])).passed, false);
  assert.equal(parseReport({ ...run(1), stdout: '' }).passed, false);
});

test('health precheck respects its deadline when a server stalls', async () => {
  const child = spawn(process.execPath, ['-e', [
    "const http = require('http');",
    'const server = http.createServer(() => {});',
    "server.listen(0, '127.0.0.1', () => process.stdout.write(String(server.address().port) + '\\n'));",
  ].join(' ')], { stdio: ['ignore', 'pipe', 'pipe'] });
  try {
    const [data] = await once(child.stdout!, 'data');
    const port = Number(String(data).trim());
    assert.ok(port > 0);
    const started = Date.now();
    assert.equal(waitForServer(300, `http://127.0.0.1:${port}`), false);
    assert.ok(Date.now() - started < 2000, 'stalled probe exceeded the deadline');
  } finally {
    child.kill();
  }
});
