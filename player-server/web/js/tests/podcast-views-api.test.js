import { API } from '../api.js';
import { readLibraryURL, libraryURL } from '../navigation-url.js';

const failures = [];
const requested = [];

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

globalThis.fetch = async (url) => {
  requested.push(url);
  return new Response('[]', {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
};
globalThis.location = { pathname: '/', href: '' };

async function capturesUrl(fn) {
  requested.length = 0;
  await fn();
  return requested[0];
}

function paramsFrom(url) {
  return new URL(url, 'http://player.test').searchParams;
}

console.log('Running podcast subscribed-episodes API and URL tests...');

{
  const params = paramsFrom(await capturesUrl(() => API.subscribedEpisodes()));
  assert(params.get('filter') === 'uncompleted', 'default filter is uncompleted');
  assert(params.get('limit') === '100', 'default limit 100');
  assert(params.get('offset') === '0', 'default offset 0');
  assert(!params.has('feed_key'), 'all-episodes omits feed_key');
}

{
  const params = paramsFrom(await capturesUrl(() => API.subscribedEpisodes({
    feedKey: 'feed:example.com/rss',
    filter: 'completed',
    limit: 25,
    offset: 50,
  })));
  assert(params.get('feed_key') === 'feed:example.com/rss', 'feed_key query');
  assert(params.get('filter') === 'completed', 'completed filter');
  assert(params.get('limit') === '25' && params.get('offset') === '50', 'page params');
}

{
  const state = {
    music: null,
    selectedSetIds: [],
    folderPath: '',
    virtualSet: 'podcasts',
    podcastFeedKey: 'feed:example.com/rss',
    podcastFilter: 'all',
    mediaPage: 2,
    filters: {},
  };
  const url = libraryURL(state);
  const route = readLibraryURL(url.slice(1));
  assert(route.virtual === 'podcasts', 'view=podcasts');
  assert(route.podcastFeedKey === 'feed:example.com/rss', 'feed_key round-trip');
  assert(route.podcastFilter === 'all', 'pfilter round-trip');
  assert(route.page === 2, 'page round-trip');
}

{
  const route = readLibraryURL('?view=podcasts');
  assert(route.virtual === 'podcasts' && route.podcastFeedKey === '' && route.podcastFilter === 'uncompleted',
    'all-episodes defaults');
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('podcast subscribed-episodes API and URL tests passed');
process.exit(0);
