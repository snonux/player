import { API } from '../api.js';

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

async function testDefaults() {
  const params = paramsFrom(await capturesUrl(() => API.podcastEpisodes(5)));
  assert(params.get('limit') === '50', 'default limit should be 50');
  assert(params.get('offset') === '0', 'default offset should be 0');
}

async function testSecondArgumentIsOffset() {
  const params = paramsFrom(await capturesUrl(() => API.podcastEpisodes(5, 0)));
  assert(params.get('limit') === '50', 'single numeric argument should keep default limit');
  assert(params.get('offset') === '0', 'single numeric argument should set explicit offset');
}

async function testOptionsObject() {
  const params = paramsFrom(await capturesUrl(() => API.podcastEpisodes(5, { limit: 25, offset: 75 })));
  assert(params.get('limit') === '25', 'options object should set limit');
  assert(params.get('offset') === '75', 'options object should set offset');
}

async function testOptionsObjectKeepsZeroLimit() {
  const params = paramsFrom(await capturesUrl(() => API.podcastEpisodes(5, { limit: 0 })));
  assert(params.get('limit') === '0', 'options object should preserve explicit zero limit');
  assert(params.get('offset') === '0', 'options object should default offset to 0');
}

async function testLegacyLimitOffset() {
  const params = paramsFrom(await capturesUrl(() => API.podcastEpisodes(5, 10, 20)));
  assert(params.get('limit') === '10', 'legacy third-argument form should preserve limit');
  assert(params.get('offset') === '20', 'legacy third-argument form should preserve offset');
}

console.log('Running podcast episodes API pagination tests...');
await testDefaults();
await testSecondArgumentIsOffset();
await testOptionsObject();
await testOptionsObjectKeepsZeroLimit();
await testLegacyLimitOffset();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All podcast episodes API pagination tests passed.');
  process.exit(0);
}
