// Unit tests for shareViewing.js: the share page's "open viewing" request,
// against the contract of POST /s/{token}/view: 204 when the viewing is open
// (newly or already), 410 when the share is expired or used up, 404 when it
// is unknown or revoked. fetch is faked.
import {
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

// fakeFetch answers with the given status (or throws the given Error) and
// records the requests.
function fakeFetch(answer) {
  const requests = [];
  const fetchImpl = async (url, init) => {
    requests.push({ url, init });
    if (answer instanceof Error) throw answer;
    return { status: answer, ok: answer >= 200 && answer < 300 };
  };
  return { requests, fetchImpl };
}

function testViewUrl() {
  assertEqual(shareViewUrl('/s/abc123'), '/s/abc123/view', 'share page path');
  assertEqual(shareViewUrl('/s/abc123/'), '/s/abc123/view', 'trailing slash');
  assertEqual(shareViewUrl('/player/s/abc123'), '/player/s/abc123/view', 'server under a path prefix');
}

async function testRequestShape() {
  const f = fakeFetch(204);
  const result = await openShareViewing('/s/tok', { fetchImpl: f.fetchImpl });
  assertEqual(result.state, 'open', 'a 204 opens the viewing');
  assertEqual(f.requests.length, 1, 'exactly one request');
  assertEqual(f.requests[0].url, '/s/tok/view', 'the view endpoint of this share');
  // Link previewers and prefetchers send GET or HEAD; only a POST may spend
  // a use.
  assertEqual(f.requests[0].init.method, 'POST', 'the viewing is opened with POST');
  // The response sets the viewing cookie, and a reload must send it back.
  assertEqual(f.requests[0].init.credentials, 'same-origin', 'cookies travel with the request');
  assertEqual(f.requests[0].init.cache, 'no-store', 'never answered from a cache');
  assertEqual(f.requests[0].init.body, undefined, 'no body');
}

async function testOutcomes() {
  const cases = [
    [200, 'open', undefined],
    [204, 'open', undefined],
    [410, 'gone', SHARE_GONE_MESSAGE],
    [404, 'gone', SHARE_GONE_MESSAGE],
    [500, 'failed', SHARE_FAILED_MESSAGE],
    [503, 'failed', SHARE_FAILED_MESSAGE],
    [405, 'failed', SHARE_FAILED_MESSAGE],
  ];
  for (const [status, state, message] of cases) {
    const result = await openShareViewing('/s/tok', { fetchImpl: fakeFetch(status).fetchImpl });
    assertEqual(result.state, state, `state for HTTP ${status}`);
    assertEqual(result.message, message, `message for HTTP ${status}`);
  }
  const failed = await openShareViewing('/s/tok', { fetchImpl: fakeFetch(500).fetchImpl });
  assertEqual(failed.status, 500, 'a failure reports its status');
}

async function testNetworkErrorDoesNotReject() {
  const result = await openShareViewing('/s/tok', { fetchImpl: fakeFetch(new TypeError('Failed to fetch')).fetchImpl });
  assertEqual(result.state, 'failed', 'a network error is a failure, not an exception');
  assertEqual(result.status, 0, 'no answer is status 0');
  assertEqual(result.message, SHARE_FAILED_MESSAGE, 'the visitor is told to reload');
}

console.log('Running share viewing tests...');
testViewUrl();
await testRequestShape();
await testOutcomes();
await testNetworkErrorDoesNotReject();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All share viewing tests passed.');
  process.exit(0);
}
