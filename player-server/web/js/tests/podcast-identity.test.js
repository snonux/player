import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { podcastFeedKey } from '../podcast-identity.js';
import { feedKeyFromURL } from '../local-podcasts-store.js';

// Read the exact Go/Dart contract vectors, rather than maintaining a JS copy.
const vectors = JSON.parse(readFileSync(new URL('../../../internal/podcast/testdata/identity-vectors.json', import.meta.url), 'utf8'));
for (const { input, key } of vectors.feed_keys) assert.equal(podcastFeedKey(input), key, `feed key for ${JSON.stringify(input)}`);
for (const input of vectors.invalid_feed_urls) assert.throws(() => podcastFeedKey(input), undefined, `reject ${JSON.stringify(input)}`);
for (const input of ['https://example.org/\ud800', 'https://example.org/\udc00', null, undefined, 42]) assert.throws(() => podcastFeedKey(input));
assert.equal(podcastFeedKey('https://example.org/a/../b?z=2&a=1#fragment'), 'feed:example.org/a/../b?z=2&a=1');
assert.equal(podcastFeedKey('https://example.org:00008443/café'), 'feed:example.org:8443/caf%C3%A9');
// Existing browser-device keys must not change when account identity is fixed.
assert.equal(feedKeyFromURL('https://example.org/feed'), 'feed:https://example.org/feed');
assert.equal(podcastFeedKey('https://example.org/feed'), 'feed:example.org/feed');
console.log(`podcast-identity passed ${vectors.feed_keys.length} positive and ${vectors.invalid_feed_urls.length} negative shared vectors`);
