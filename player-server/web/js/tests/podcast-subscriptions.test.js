import { pendingEdit, randomEditId, setSubscribed, subscriptionEdit } from '../podcast-subscriptions.js';

const failures = [];
const sent = [];
let reply = () => ({ acked: [], rejected: [] });

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

globalThis.location = { pathname: '/', href: '' };
globalThis.fetch = async (url, options) => {
  const body = JSON.parse(options.body);
  sent.push({ url, method: options.method, body });
  return new Response(JSON.stringify(reply(body)), { status: 200, headers: { 'content-type': 'application/json' } });
};
const ackAll = (body) => ({ acked: body.subscriptions.map((s) => s.edit_id), rejected: [] });

const feed = { id: 3, feed_key: 'feed:example.com/rss', feed_url: 'https://example.com/rss', title: 'Show' };

function testEditId() {
  const id = randomEditId();
  assert(/^[A-Za-z0-9_-]{22}$/.test(id), `edit id shape: ${id}`);
  assert(randomEditId() !== id, 'edit ids must be random');
}

async function testUnsubscribeSendsTombstone() {
  sent.length = 0;
  reply = ackAll;
  await setSubscribed(feed, false);
  const [req] = sent;
  assert(req && req.url === '/api/podcasts/sync' && req.method === 'POST', 'unsubscribe must post to the sync endpoint');
  const edit = req.body.subscriptions[0];
  assert(edit.subscribed === false && edit.feed_key === feed.feed_key, 'tombstone for the feed key');
  assert(edit.urls[0] === feed.feed_url && edit.fetch_url === feed.feed_url, 'canonical feed sends its url');
  assert(!sent.some((r) => r.method === 'DELETE'), 'unsubscribe must never delete');
  assert(pendingEdit(feed.feed_key) === undefined, 'acked edit is no longer pending');
}

async function testRetryReusesEdit() {
  sent.length = 0;
  globalThis.fetch = async (url, options) => {
    sent.push({ body: JSON.parse(options.body) });
    throw new TypeError('network down');
  };
  let failed = false;
  try { await setSubscribed(feed, true); } catch { failed = true; }
  assert(failed, 'network failure must surface');
  const first = sent[0].body.subscriptions[0];
  assert(pendingEdit(feed.feed_key)?.edit_id === first.edit_id, 'failed edit stays pending');
  const again = subscriptionEdit(feed, true);
  assert(again.edit_id === first.edit_id && again.observed_ms === first.observed_ms, 'retry replays the same edit');
  const other = subscriptionEdit(feed, false, first.observed_ms - 1000);
  assert(other.edit_id !== first.edit_id && other.observed_ms > first.observed_ms, 'a new value is a new, later edit');
}

async function testRejectionAndLegacyKey() {
  globalThis.fetch = async (url, options) => {
    const body = JSON.parse(options.body);
    sent.push({ body });
    return new Response(JSON.stringify({ acked: [], rejected: [{ edit_id: body.subscriptions[0].edit_id, reason: 'invalid_key' }] }),
      { status: 200, headers: { 'content-type': 'application/json' } });
  };
  sent.length = 0;
  const legacy = { id: 4, feed_key: 'legacy-feed:4', feed_url: 'https://user:pw@example.com/rss', title: 'Old' };
  let message = '';
  try { await setSubscribed(legacy, false); } catch (err) { message = err.message; }
  assert(message.includes('invalid_key'), `rejection reported: ${message}`);
  const edit = sent[0].body.subscriptions[0];
  assert(edit.urls.length === 0 && edit.fetch_url === '', 'legacy key sends no urls');
  assert(pendingEdit(legacy.feed_key) === undefined, 'rejected edit is dropped');
}

console.log('Running podcast subscription tests...');
testEditId();
await testUnsubscribeSendsTombstone();
await testRetryReusesEdit();
await testRejectionAndLegacyKey();
if (failures.length) {
  console.error(failures.join('\n'));
  process.exit(1);
}
console.log('podcast subscription tests passed');
