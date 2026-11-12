import assert from 'node:assert/strict';

const store = new Map();
globalThis.localStorage = { getItem: k => store.get(k) ?? null, setItem: (k, v) => store.set(k, String(v)), removeItem: k => store.delete(k) };
const { editLocalPodcast } = await import('../local-podcast-state.js');
const { localPodcastResumeSeconds, localPodcastResumeHref, requestedLocalPodcastResume } = await import('../local-podcast-resume.js');

const ep = { feed_key: 'feed:https://pod.example/feed.xml', episode_key: 'guid:a&b' };
assert.equal(localPodcastResumeSeconds(ep), 0, 'unplayed episodes start fresh');
editLocalPodcast(ep, 'position', 754.5);
assert.equal(localPodcastResumeSeconds(ep), 754.5, 'the saved position resumes');
editLocalPodcast(ep, 'completion', true);
assert.equal(localPodcastResumeSeconds(ep), 0, 'a completed episode starts over');
editLocalPodcast(ep, 'completion', false);
editLocalPodcast(ep, 'deletion', true);
assert.equal(localPodcastResumeSeconds(ep), 0, 'a deleted episode is not resumed');

const href = localPodcastResumeHref(ep);
assert.match(href, /^\/index\.html\?local=podcasts#resume=/, 'only the exact public query reaches the server');
assert.deepEqual(requestedLocalPodcastResume(new URL(href, 'http://x').hash), ep, 'the link names the episode');
assert.equal(requestedLocalPodcastResume(''), null);
assert.equal(requestedLocalPodcastResume('#resume=%7Bbad'), null);
assert.equal(requestedLocalPodcastResume('#resume=%5B1%2C2%5D'), null);
console.log('local podcast resume checks passed');
