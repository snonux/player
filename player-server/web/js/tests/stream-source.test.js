// Unit tests for streamSource.js: playback URL choice and the readiness probe
// of the server's compatibility stream, against the contract of the compat
// endpoint (200/206 ready, 503 + Retry-After while transcoding or busy,
// anything else terminal). fetch, sleep and the clock are faked.
import { COMPAT_MAX_WAIT_MS, mediaErrorReason, mediaPlaybackUrl, waitForCompatStream } from '../streamSource.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function response(status, body = null, headers = {}) {
  return {
    status,
    headers: { get: (name) => headers[name] ?? null },
    json: async () => {
      if (body === null) throw new Error('no json body');
      return body;
    },
  };
}

const transcoding = () => response(503, { error: 'transcode in progress', retry_after_seconds: 5, status: 'transcoding' }, { 'Retry-After': '5' });
const busy = () => response(503, { error: 'busy', retry_after_seconds: 5, status: 'busy' }, { 'Retry-After': '5' });

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
  const h = harness([response(206)]);
  const result = await waitForCompatStream('/api/media/1/compat', h.options);
  assertEqual(result.state, 'ready', 'a 206 is ready');
  assertEqual(h.requests.length, 1, 'one probe when ready');
  assertEqual(h.requests[0].url, '/api/media/1/compat', 'probes the compat URL itself');
  assertEqual(h.requests[0].init.headers.Range, 'bytes=0-0', 'probe asks for one byte only');
  assertEqual(h.statuses.length, 0, 'no preparing state when ready');
  assertEqual((await waitForCompatStream('/x/compat', harness([response(200)]).options)).state, 'ready', 'a 200 is ready');
}

async function testPreparingThenReady() {
  const h = harness([transcoding(), busy(), response(206)]);
  const result = await waitForCompatStream('/api/media/1/compat', h.options);
  assertEqual(result.state, 'ready', '503s followed by 206 end ready');
  assertEqual(h.requests.length, 3, 'the same GET is repeated until ready');
  assertEqual(h.statuses.join(','), 'transcoding,busy', 'both 503 flavours keep the preparing state');
  assertEqual(h.sleeps.join(','), '5000,5000', 'waits Retry-After seconds between probes');
}

async function testRetryDelayFallbacksAndClamp() {
  const noHints = harness([response(503), response(206)]);
  await waitForCompatStream('/c', noHints.options);
  assertEqual(noHints.sleeps[0], 5000, 'default retry delay without hints');
  assertEqual(noHints.statuses[0], 'transcoding', 'default status without body');

  const bodyOnly = harness([response(503, { retry_after_seconds: 7 }), response(206)]);
  await waitForCompatStream('/c', bodyOnly.options);
  assertEqual(bodyOnly.sleeps[0], 7000, 'JSON hint used without header');

  const huge = harness([response(503, null, { 'Retry-After': '3600' }), response(206)]);
  await waitForCompatStream('/c', huge.options);
  assertEqual(huge.sleeps[0], 30000, 'retry delay is capped');

  const bogus = harness([response(503, null, { 'Retry-After': 'soon' }), response(206)]);
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

async function testTerminalStatuses() {
  const cases = [
    [response(500, { error: 'transcode failed' }), 'could not convert'],
    [response(507, { error: 'insufficient storage for transcode' }), 'no space left'],
    [response(404, { error: 'not found' }), 'not found'],
    [response(400, { error: 'media does not need transcoding; play the stream endpoint instead' }), 'does not need transcoding'],
    [response(415, { error: 'unsupported media type' }), 'unsupported media type'],
    [response(418), 'status 418'],
  ];
  for (const [res, expected] of cases) {
    const h = harness([res]);
    const result = await waitForCompatStream('/c', h.options);
    assertEqual(result.state, 'failed', `status ${res.status} is terminal`);
    assert(result.reason.includes(expected), `status ${res.status} reason should mention "${expected}", got "${result.reason}"`);
    assertEqual(h.requests.length, 1, `status ${res.status} is not retried`);
    assertEqual(h.sleeps.length, 0, `status ${res.status} does not wait`);
  }
}

async function testNetworkError() {
  const h = harness([new TypeError('Failed to fetch')]);
  const result = await waitForCompatStream('/c', h.options);
  assertEqual(result.state, 'failed', 'network error fails');
  assert(result.reason.includes('could not be reached'), 'network error reason');
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
  const h = harness([response(206)]);
  const fetchImpl = h.options.fetchImpl;
  // An aborted fetch rejects; the probe must report cancel, not a failure.
  h.options.fetchImpl = async (url, init) => {
    await fetchImpl(url, init);
    controller.abort();
    throw new DOMException('aborted', 'AbortError');
  };
  const result = await waitForCompatStream('/c', { ...h.options, signal: controller.signal });
  assertEqual(result.state, 'cancelled', 'abort during the request cancels');
  assertEqual(h.requests[0].init.signal, controller.signal, 'the signal is handed to fetch');
}

async function testDefaultSleepStopsOnAbort() {
  // With the real timer-based sleep, an abort must end a 30s wait at once.
  const controller = new AbortController();
  const h = harness([response(503, null, { 'Retry-After': '30' })]);
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
await testTerminalStatuses();
await testNetworkError();
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
