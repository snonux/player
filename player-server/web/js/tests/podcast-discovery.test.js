import assert from 'node:assert/strict';
import { searchPodcasts } from '../podcast-discovery.js';
import { API } from '../api.js';
let request;
globalThis.fetch = async (url, options) => {
  request = { url, options };
  return { ok: true, json: async () => [{ title: 'Space', feed_url: 'https://example.org/feed' }] };
};
const results = await searchPodcasts('space & science');
assert.equal(results[0].title, 'Space');
assert.equal(request.url, '/api/podcast-discovery?q=space%20%26%20science');
assert.equal(request.options.credentials, 'omit');
assert.equal(request.options.referrerPolicy, 'no-referrer');
globalThis.fetch = async () => ({ ok: false, json: async () => ({ error: 'Retry shortly' }) });
await assert.rejects(searchPodcasts('space'), /Retry shortly/);
console.log('podcast-discovery tests passed');

globalThis.location = { pathname: '/', search: '' };
globalThis.fetch = async (url, options) => {
  request = { url, options };
  return { ok: true, status: 200, headers: new Headers({ 'content-type': 'application/json' }), json: async () => ({ feed_key: 'feed:example.org/rss', status: 'refreshed' }) };
};
assert.equal((await API.refreshPodcastFeed('feed:example.org/rss')).status, 'refreshed');
assert.equal(request.url, '/api/podcasts/refresh');
assert.equal(request.options.method, 'POST');
assert.deepEqual(JSON.parse(request.options.body), { feed_key: 'feed:example.org/rss' });
console.log('podcast metadata refresh API contract passed');
