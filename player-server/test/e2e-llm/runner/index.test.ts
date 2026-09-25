import assert from 'node:assert/strict';
import test from 'node:test';
import { SpawnSyncReturns } from 'node:child_process';
import { parseReport, scenarioSpec, SCENARIO_SPECS, openAskTask, titleReason } from './index';

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
  assert.equal(Object.keys(SCENARIO_SPECS).length, 25);
  for (let i = 1; i <= 25; i++) {
    const id = `S${String(i).padStart(2, '0')}`;
    assert.equal(scenarioSpec(id), `scenario-${id}.test.ts`);
  }
  assert.equal(new Set(Object.values(SCENARIO_SPECS)).size, 25);
  assert.equal(scenarioSpec('S26'), undefined);
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

const scenario = { meta: { id: 'S05', title: 'title with `quotes` $(literal)' }, steps: '', filePath: '/test/scenarios/S05.md' };

test('failure tasks use a valid separate tag, component scope and durable reproduction annotation', () => {
  const commands: Array<{ args: string[]; cwd: string }> = [];
  const success = openAskTask(scenario, 'assertion failed', 'config-preamble' + 'x'.repeat(5000), (args, cwd) => {
    commands.push({ args, cwd });
    return { ...run(1), stdout: args[0] === 'add' ? 'created task abc12\n' : 'ok abc12\n' };
  });
  assert.equal(success, true);
  assert.deepEqual(commands[0].args, ['add', '+testing', 'E2E S05: title with `quotes` $(literal) — assertion failed']);
  assert.ok(commands[0].cwd.endsWith('/player-server'));
  assert.deepEqual(commands[1].args.slice(0, 2), ['annotate', 'abc12']);
  for (const text of ['agent-task-management', 'AGENTS.md', '/test/scenarios/S05.md', 'scenario-S05.test.ts', 'PLAYER_SCENARIO_BINARY', 'assertion failed']) assert.ok(commands[1].args[2].includes(text));
  assert.ok(commands[1].args[2].endsWith('x'.repeat(4000)));
  assert.ok(!commands[1].args[2].includes('x'.repeat(4001)));
  assert.ok(!commands[1].args[2].includes('config-preamble'), 'the report preamble is dropped in favour of the tail');
});

test('task titles use one short uncoloured line of the failure reason', () => {
  assert.equal(titleReason('\u001b[31mExpected: 404\u001b[39m\nReceived: 500'), 'Expected: 404');
  assert.equal(titleReason('\n\n  Server exited: boom\nlog line'), 'Server exited: boom');
  assert.equal(titleReason(''), 'failed');
  const long = titleReason('y'.repeat(500));
  assert.equal(long.length, 160);
  assert.ok(long.endsWith('…'));
});

test('task command failures are reported without inventing an ID or success', () => {
  for (const result of [{ ...run(1), status: 1, stderr: 'offline' }, { ...run(1), stdout: 'unexpected output' }]) {
    let calls = 0;
    assert.equal(openAskTask(scenario, 'failure', '', () => { calls++; return result; }), false);
    assert.equal(calls, 1);
  }
  let calls = 0;
  assert.equal(openAskTask(scenario, 'failure', '', () => {
    calls++;
    return calls === 1 ? { ...run(1), stdout: 'created task abc12\n' } : { ...run(1), status: 1, stderr: 'annotation failed' };
  }), false);
  assert.equal(calls, 2);
});
