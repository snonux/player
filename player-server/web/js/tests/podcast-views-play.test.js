import {
  episodePlayableMedia,
  mediaSlotsForEpisodes,
  rememberEpisodeMedia,
} from '../podcast-views.js';
import { state, setMedia } from '../state.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

console.log('Running podcast subscription-view play slots tests...');

const undownloaded = {
  id: 1,
  feed_key: 'feed:a/',
  episode_key: 'guid:1',
  title: 'Fresh',
  server_copy: null,
};
const downloaded = {
  id: 2,
  feed_key: 'feed:a/',
  episode_key: 'guid:2',
  title: 'Saved',
  server_copy: { media_id: 99 },
};

assert(episodePlayableMedia(undownloaded) === null, 'undownloaded has no playable slot');
const stub = episodePlayableMedia(downloaded);
assert(stub && stub.id === 99 && stub.type === 'audio' && stub.is_podcast === true,
  'downloaded episode becomes an audio media stub for playMediaById');
assert(stub.title === 'Saved', 'stub keeps the episode title');

const fromDownload = episodePlayableMedia(undownloaded, { id: 42, type: 'audio', file_name: 'ep.mp3' });
assert(fromDownload.id === 42 && fromDownload.file_name === 'ep.mp3',
  'download response is preferred over server_copy');

const slots = mediaSlotsForEpisodes([undownloaded, downloaded]);
assert(slots.length === 2 && slots[0] === null && slots[1]?.id === 99,
  'page slots align with episode card indices');

setMedia([]);
rememberEpisodeMedia(1, { id: 42, type: 'audio', title: 'Just downloaded' });
assert(state.media[0] === null && state.media[1]?.id === 42,
  'rememberEpisodeMedia pads state.media so playMediaById finds the index');
assert(state.media.findIndex((m) => m && String(m.id) === '42') === 1,
  'playMediaById can resolve the downloaded id from state.media');

// Simulate the All-episodes render path: empty list then slots for the page.
setMedia([]);
setMedia(mediaSlotsForEpisodes([downloaded, undownloaded]));
assert(state.media[0]?.id === 99 && state.media[1] === null,
  'subscription view replaces the cleared browse list with playable slots');

// Production playMediaById must tolerate null holes (undownloaded earlier cards).
setMedia(mediaSlotsForEpisodes([undownloaded, downloaded]));
assert(
  (() => {
    try {
      return state.media.findIndex((m) => m && String(m.id) === '99') === 1;
    } catch (error) {
      failures.push(`null holes must not throw: ${error}`);
      return false;
    }
  })(),
  'lookup through null holes finds the downloaded media id');

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('podcast subscription-view play slots tests passed');
process.exit(0);
