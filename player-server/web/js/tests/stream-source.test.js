// Unit tests for streamSource.js: playback URL choice and the readiness probe
// of the server's compatibility stream, against the contract of the compat
// endpoint: HEAD answers 200/206 when ready, 503 + Retry-After (+
// X-Transcode-Status: transcoding|busy) while preparing, anything else is
// terminal. fetch, sleep and the clock are faked.
import {
  COMPAT_MAX_WAIT_MS,
  MAX_UNRECOGNISED_503,
  mediaErrorReason,
  mediaPlaybackUrl,
  waitForCompatStream,
} from '../streamSource.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

// A HEAD response: status and headers only, never a body.
function response(status, headers = {}) {
  return { status, headers: { get: (name) => headers[name] ?? null } };
}

const transcoding = () => response(503, { 'Retry-After': '5', 'X-Transcode-Status': 'transcoding' });
const busy = () => response(503, { 'Retry-After': '5', 'X-Transcode-Status': 'busy' });

// harness builds injectable fakes: a scripted fetch, a sleep that advances a
// fake clock, and records of what the probe did.
function harness(answers) {
  const h = { requests: [], sleeps: [], statuses: [], clock: 0 };
  h.options = {
    fetchImpl: async (url, init) => {
      h.requests.push({ url, init });
      const next = answers.length > 1 ? answers.shift() : answers[0];
      if (next instanceof Error) throw next;
      return typeof next === 'function' ? next() : next;
    },
    sleep: async (ms) => { h.sleeps.push(ms); h.clock += ms; },
    now: () => h.clock,
    onPreparing: (status) => h.statuses.push(status),
  };
  return h;
}

function testPlaybackUrlFollowsTranscodedHint() {
  assertEqual(mediaPlaybackUrl({ id: 3, file_name: 'a.mp4' }), '/api/media/3/stream', 'plain media plays the stream');
  assertEqual(mediaPlaybackUrl({ id: 4, file_name: 'a.mp4', transcoded: true }), '/api/media/4/compat', 'transcoded media plays compat');
  // The extension must not matter: only the server hint decides.
  assertEqual(mediaPlaybackUrl({ id: 5, file_name: 'a.avi', transcoded: false }), '/api/media/5/stream', 'avi without hint plays the stream');
}

function testMediaErrorReasons() {
  assert(mediaErrorReason({ code: 3 }).includes('cannot be decoded'), 'decode error reason');
  assert(mediaErrorReason({ code: 4 }).includes('not supported'), 'unsupported source reason');
  assert(mediaErrorReason({ code: 2 }).includes('network'), 'network error reason');
  assertEqual(mediaErrorReason(null), 'unknown playback error', 'missing MediaError');
}

async function testReadyAtOnce() {
  const h = harness([response(200)]);
  const result = await waitForCompatStream('/s/tok/compat', h.options);
  assertEqual(result.state, 'ready', 'a 200 is ready');
  assertEqual(h.requests.length, 1, 'one probe when ready');
  assertEqual(h.requests[0].url, '/s/tok/compat', 'probes the compat URL itself');
  // A HEAD probe consumes no share use; a GET (even for one byte) would.
  assertEqual(h.requests[0].init.method, 'HEAD', 'the probe is a HEAD request');
  assertEqual(h.requests[0].init.headers, undefined, 'the probe sends no Range or other headers');
  assertEqual(h.statuses.length, 0, 'no preparing state when ready');
  assertEqual((await waitForCompatStream('/x/compat', harness([response(206)]).options)).state, 'ready', 'a 206 is ready');
}

async function testPreparingThenReady() {
  const h = harness([transcoding(), busy(), response(200)]);
  const result = await waitForCompatStream('/api/media/1/compat', h.options);
  assertEqual(result.state, 'ready', '503s followed by 200 end ready');
  assertEqual(h.requests.length, 3, 'the same request is repeated until ready');
  assert(h.requests.every((r) => r.init.method === 'HEAD'), 'every retry is a HEAD request');
  assertEqual(h.statuses.join(','), 'transcoding,busy', 'X-Transcode-Status is passed on for both flavours');
  assertEqual(h.sleeps.join(','), '5000,5000', 'waits Retry-After seconds between probes');
}

async function testRetryDelayFallbacksAndClamp() {
  const noHints = harness([response(503), response(200)]);
  await waitForCompatStream('/c', noHints.options);
  assertEqual(noHints.sleeps[0], 5000, 'default retry delay without Retry-After');
  assertEqual(noHints.statuses[0], '', 'no status without X-Transcode-Status (generic preparing text)');

  const huge = harness([response(503, { 'Retry-After': '3600' }), response(200)]);
  await waitForCompatStream('/c', huge.options);
  assertEqual(huge.sleeps[0], 30000, 'retry delay is capped');

  const bogus = harness([response(503, { 'Retry-After': 'soon' }), response(200)]);
  await waitForCompatStream('/c', bogus.options);
  assertEqual(bogus.sleeps[0], 5000, 'non-numeric Retry-After falls back to the default');
}

async function testPreparingUntilCap() {
  const h = harness([transcoding]);
  const result = await waitForCompatStream('/api/media/1/compat', h.options);
  assertEqual(result.state, 'failed', 'endless 503 fails at the cap');
  assert(result.reason.includes('still preparing'), `cap reason, got ${result.reason}`);
  assertEqual(h.sleeps.length, COMPAT_MAX_WAIT_MS / 5000, 'retries until the cap and not beyond');
  assert(h.clock <= COMPAT_MAX_WAIT_MS, 'never waits longer than the cap');

  const short = harness([transcoding]);
  await waitForCompatStream('/c', { ...short.options, maxWaitMs: 12000 });
  assertEqual(short.requests.length, 3, 'custom cap: probes at 0s, 5s and 10s');
}

async function testUnrecognised503IsBounded() {
  // A proxy's 503 carries no X-Transcode-Status: retried, but not for 5 min.
  const proxy = harness([() => response(503, { 'Retry-After': '5' })]);
  const result = await waitForCompatStream('/c', proxy.options);
  assertEqual(result.state, 'failed', 'endless unrecognised 503 fails');
  assertEqual(result.status, 503, 'the failure carries the status');
  assert(result.reason.includes('unavailable'), `unrecognised 503 reason, got ${result.reason}`);
  assertEqual(proxy.requests.length, MAX_UNRECOGNISED_503 + 1, 'gives up after the bound');
  assert(proxy.clock < COMPAT_MAX_WAIT_MS / 2, 'gives up long before the cap');

  // A recognised answer in between resets the count: a blip does not add up.
  const answers = [];
  for (let i = 0; i < 4; i++) {
    for (let j = 0; j < MAX_UNRECOGNISED_503; j++) answers.push(response(503));
    answers.push(transcoding());
  }
  answers.push(response(200));
  const blips = harness(answers);
  assertEqual((await waitForCompatStream('/c', blips.options)).state, 'ready', 'blips below the bound are survived');
}

async function testTerminalStatuses() {
  const cases = [
    [500, 'could not convert'],
    [507, 'no space left'],
    [404, 'not found'],
    [400, 'no converted version'],
    [415, 'cannot be converted'],
    [401, 'not signed in'],
    [410, 'no longer valid'],
    [418, 'status 418'],
  ];
  for (const [status, expected] of cases) {
    const h = harness([response(status)]);
    const result = await waitForCompatStream('/c', h.options);
    assertEqual(result.state, 'failed', `status ${status} is terminal`);
    assertEqual(result.status, status, `status ${status} is reported`);
    assert(result.reason.includes(expected), `status ${status} reason should mention "${expected}", got "${result.reason}"`);
    assertEqual(h.requests.length, 1, `status ${status} is not retried`);
    assertEqual(h.sleeps.length, 0, `status ${status} does not wait`);
  }
}

async function testNetworkError() {
  const h = harness([new TypeError('Failed to fetch')]);
  const result = await waitForCompatStream('/c', h.options);
  assertEqual(result.state, 'failed', 'network error fails');
  assertEqual(result.status, 0, 'no answer is status 0');
  assert(result.reason.includes('did not answer'), 'network error reason');
}

async function testHangingRequestTimesOut() {
  const h = harness([]);
  // Never answers; only the abort signal ends it, like a real fetch.
  h.options.fetchImpl = (url, init) => new Promise((_, reject) => {
    h.requests.push({ url, init });
    init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
  });
  const started = Date.now();
  const result = await waitForCompatStream('/c', { ...h.options, timeoutMs: 30 });
  assertEqual(result.state, 'failed', 'a hanging probe fails instead of preparing forever');
  assert(result.reason.includes('did not answer'), `timeout reason, got ${result.reason}`);
  assertEqual(h.requests.length, 1, 'a timed out probe is not retried');
  assert(Date.now() - started < 2000, 'the timeout ends the request');
}

async function testCancelWhileWaiting() {
  const controller = new AbortController();
  const h = harness([transcoding]);
  h.options.sleep = async () => { controller.abort(); };
  const result = await waitForCompatStream('/c', { ...h.options, signal: controller.signal });
  assertEqual(result.state, 'cancelled', 'abort during the retry wait cancels');
  assertEqual(h.requests.length, 1, 'no further probe after cancel');
}

async function testCancelWhileFetching() {
  const controller = new AbortController();
  const h = harness([]);
  let requestSignal = null;
  // The caller's abort must reach the request (through the probe's own
  // signal) and be reported as a cancel, not as a failure.
  h.options.fetchImpl = (url, init) => new Promise((_, reject) => {
    requestSignal = init.signal;
    init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    setTimeout(() => controller.abort(), 0);
  });
  const result = await waitForCompatStream('/c', { ...h.options, signal: controller.signal });
  assertEqual(result.state, 'cancelled', 'abort during the request cancels');
  assert(requestSignal.aborted, 'the abort is forwarded to the request');
}

async function testDefaultSleepStopsOnAbort() {
  // With the real timer-based sleep, an abort must end a 30s wait at once.
  const controller = new AbortController();
  const h = harness([response(503, { 'Retry-After': '30', 'X-Transcode-Status': 'transcoding' })]);
  delete h.options.sleep;
  h.options.onPreparing = () => setTimeout(() => controller.abort(), 0);
  const started = Date.now();
  const result = await waitForCompatStream('/c', { ...h.options, now: Date.now, signal: controller.signal });
  assertEqual(result.state, 'cancelled', 'default sleep observes the abort');
  assert(Date.now() - started < 2000, 'abort does not wait for the retry delay');
}

console.log('Running stream source tests...');
testPlaybackUrlFollowsTranscodedHint();
testMediaErrorReasons();
await testReadyAtOnce();
await testPreparingThenReady();
await testRetryDelayFallbacksAndClamp();
await testPreparingUntilCap();
await testUnrecognised503IsBounded();
await testTerminalStatuses();
await testNetworkError();
await testHangingRequestTimesOut();
await testCancelWhileWaiting();
await testCancelWhileFetching();
await testDefaultSleepStopsOnAbort();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All stream source tests passed.');
  process.exit(0);
}
