import assert from 'node:assert/strict';
import { feedKeyFromURL } from '../local-podcasts-store.js';
import { localPodcastsMode, deviceLibraryMode, localFilesMode } from '../offline-context.js';

assert.equal(
  feedKeyFromURL('https://cdn.example.com/podcast.xml'),
  'feed:https://cdn.example.com/podcast.xml',
);
assert.throws(() => feedKeyFromURL('ftp://cdn.example.com/podcast.xml'));

globalThis.location = { search: '?local=podcasts' };
assert.equal(localPodcastsMode(), true);
assert.equal(deviceLibraryMode(), true);
assert.equal(localFilesMode(), false);
console.log('local-podcasts unit checks passed');
