import { completionEdit, pendingCompletionEdit, setEpisodeCompleted, setPodcastAccount } from '../podcast-episode-state.js';
setPodcastAccount(1);

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

const ep = { feed_key: 'feed:example.com/rss', episode_key: 'guid:ep-1' };
const requests = [];

if (!globalThis.crypto?.getRandomValues) {
  Object.defineProperty(globalThis, 'crypto', {
    value: {
      getRandomValues(buf) {
        for (let i = 0; i < buf.length; i++) buf[i] = (i * 17 + 3) & 0xff;
        return buf;
      },
    },
    configurable: true,
  });
}

globalThis.fetch = async (url, opts = {}) => {
  const body = opts.body ? JSON.parse(opts.body) : null;
  requests.push({ url, method: opts.method, body });
  const editId = body?.episodes?.[0]?.completion?.edit_id;
  return new Response(JSON.stringify({ acked: editId ? [editId] : [], rejected: [] }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
};

globalThis.location = { pathname: '/', href: '' };

console.log('Running podcast episode completion sync tests...');

const first = completionEdit(ep, true, 1000);
assert(first.completion.completed === true && first.completion.source === 'manual', 'manual completion edit');
assert(first.completion.edit_id?.length >= 16, 'edit_id present');
const again = completionEdit(ep, true, 2000);
assert(again.completion.edit_id === first.completion.edit_id, 'same value reuses pending edit');
assert(again.completion.observed_ms === first.completion.observed_ms, 'retry keeps clock');

const flip = completionEdit(ep, false, 2000);
assert(flip.completion.completed === false, 'uncomplete is a new value');
assert(flip.completion.edit_id !== first.completion.edit_id, 'new edit_id for new value');
assert(flip.completion.observed_ms > first.completion.observed_ms, 'later clock');

await setEpisodeCompleted(ep, true);
assert(requests[0]?.url === '/api/podcasts/sync' && requests[0]?.method === 'POST', 'posts sync');
assert(requests[0]?.body?.episodes?.[0]?.completion?.completed === true, 'sends completed true');
assert(pendingCompletionEdit(ep.feed_key, ep.episode_key) === undefined, 'acked edit cleared');

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('podcast episode completion sync tests passed');
process.exit(0);
