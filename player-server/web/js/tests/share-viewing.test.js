// Unit tests for shareViewing.js: the share page's "open viewing" request,
// against the contract of POST /s/{token}/view: 204 when the viewing is open
// (newly or already), 410 when the share is expired or used up, 404 when it
// is unknown or revoked. fetch and the retry pause are faked.
import {
  GONE_RETRY_DELAY_MS,
  SHARE_FAILED_MESSAGE,
  SHARE_GONE_MESSAGE,
  openShareViewing,
  shareViewUrl,
} from '../shareViewing.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

// harness answers the requests with the given statuses in turn (the last one
// repeats; an Error is thrown instead) and records requests and pauses.
function harness(...answers) {
  const h = { requests: [], sleeps: [] };
  h.options = {
    fetchImpl: async (url, init) => {
      h.requests.push({ url, init });
      const answer = answers.length > 1 ? answers.shift() : answers[0];
      if (answer instanceof Error) throw answer;
      return { status: answer, ok: answer >= 200 && answer < 300 };
    },
    sleep: async (ms) => { h.sleeps.push(ms); },
  };
  return h;
}

function testViewUrl() {
  assertEqual(shareViewUrl('/s/abc123'), '/s/abc123/view', 'share page path');
  assertEqual(shareViewUrl('/s/abc123/'), '/s/abc123/view', 'trailing slash');
  assertEqual(shareViewUrl('/player/s/abc123'), '/player/s/abc123/view', 'server under a path prefix');
}

async function testRequestShape() {
  const h = harness(204);
  const result = await openShareViewing('/s/tok', h.options);
  assertEqual(result.state, 'open', 'a 204 opens the viewing');
  assertEqual(h.requests.length, 1, 'exactly one request');
  assertEqual(h.sleeps.length, 0, 'no pause when the viewing opens at once');
  assertEqual(h.requests[0].url, '/s/tok/view', 'the view endpoint of this share');
  // Link previewers and prefetchers send GET or HEAD; only a POST may spend
  // a use.
  assertEqual(h.requests[0].init.method, 'POST', 'the viewing is opened with POST');
  // The response sets the viewing cookie, and a reload must send it back.
  assertEqual(h.requests[0].init.credentials, 'same-origin', 'cookies travel with the request');
  assertEqual(h.requests[0].init.cache, 'no-store', 'never answered from a cache');
  assertEqual(h.requests[0].init.body, undefined, 'no body');
}

async function testOutcomes() {
  const cases = [
    [200, 'open', undefined, 1],
    [204, 'open', undefined, 1],
    // A 410 is asked about once more; still 410: gone.
    [410, 'gone', SHARE_GONE_MESSAGE, 2],
    // Unknown or revoked: final at once.
    [404, 'gone', SHARE_GONE_MESSAGE, 1],
    [500, 'failed', SHARE_FAILED_MESSAGE, 1],
    [503, 'failed', SHARE_FAILED_MESSAGE, 1],
    [405, 'failed', SHARE_FAILED_MESSAGE, 1],
  ];
  for (const [status, state, message, requests] of cases) {
    const h = harness(status);
    const result = await openShareViewing('/s/tok', h.options);
    assertEqual(result.state, state, `state for HTTP ${status}`);
    assertEqual(result.message, message, `message for HTTP ${status}`);
    assertEqual(h.requests.length, requests, `requests for HTTP ${status}`);
  }
  const failed = await openShareViewing('/s/tok', harness(500).options);
  assertEqual(failed.status, 500, 'a failure reports its status');
}

// Two tabs of one browser open a single-use link at once: one POST wins, the
// other gets 410. By the time the loser asks again the winner's cookie is in
// the browser, and the same viewing serves both tabs.
async function testGoneIsRetriedOnce() {
  const won = harness(410, 204);
  const result = await openShareViewing('/s/tok', won.options);
  assertEqual(result.state, 'open', 'the second answer counts');
  assertEqual(won.requests.length, 2, 'one retry');
  assertEqual(won.sleeps.length, 1, 'one pause before the retry');
  assertEqual(won.sleeps[0], GONE_RETRY_DELAY_MS, 'the default pause');
  assertEqual(won.requests[1].init.method, 'POST', 'the retry is the same POST');
  assertEqual(won.requests[1].init.credentials, 'same-origin', 'the retry carries the cookie that arrived meanwhile');

  // Never more than one retry, whatever keeps coming back.
  const lost = harness(410, 410, 204);
  assertEqual((await openShareViewing('/s/tok', lost.options)).state, 'gone', 'a second 410 is final');
  assertEqual(lost.requests.length, 2, 'no third request');

  // The retry's own outcome is reported as it is.
  assertEqual((await openShareViewing('/s/tok', harness(410, 404).options)).state, 'gone', '410 then 404');
  assertEqual((await openShareViewing('/s/tok', harness(410, 500).options)).state, 'failed', '410 then 500');
  assertEqual((await openShareViewing('/s/tok', harness(410, new TypeError('offline')).options)).state, 'failed', '410 then no answer');

  const custom = harness(410, 204);
  await openShareViewing('/s/tok', { ...custom.options, retryDelayMs: 5 });
  assertEqual(custom.sleeps[0], 5, 'the pause is configurable');
}

async function testNetworkErrorDoesNotReject() {
  const h = harness(new TypeError('Failed to fetch'));
  const result = await openShareViewing('/s/tok', h.options);
  assertEqual(result.state, 'failed', 'a network error is a failure, not an exception');
  assertEqual(result.status, 0, 'no answer is status 0');
  assertEqual(result.message, SHARE_FAILED_MESSAGE, 'the visitor is told to reload');
  assertEqual(h.requests.length, 1, 'a network error is not retried');
}

console.log('Running share viewing tests...');
testViewUrl();
await testRequestShape();
await testOutcomes();
await testGoneIsRetriedOnce();
await testNetworkErrorDoesNotReject();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All share viewing tests passed.');
  process.exit(0);
}
