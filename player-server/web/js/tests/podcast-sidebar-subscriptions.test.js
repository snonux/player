import assert from 'node:assert/strict';
import { API } from '../api.js';
import { listAccountPodcastSubscriptions, resetPodcastSubscriptions } from '../podcast-subscriptions.js';

const calls = [];
const hidden = { feed_key: 'feed:publisher.example/rss', title: 'My subscription', fetch_url: 'https://publisher.example/rss', subscribed: true };
API.syncPodcast = async body => {
  calls.push(body);
  return body.since === 0
    ? { epoch: 'one', next_since: 1, has_more: true, subscriptions: [hidden] }
    : { epoch: 'one', next_since: 2, has_more: false, subscriptions: [
      { feed_key: 'feed:catalog.example/rss', title: '', fetch_url: '', subscribed: true },
      { feed_key: 'feed:unsubscribed.example/rss', subscribed: false },
    ] };
};
const feeds = await listAccountPodcastSubscriptions([
  { feed_key: 'feed:catalog.example/rss', title: 'Catalog title', feed_url: 'https://catalog.example/rss', image_url: '/cover.jpg' },
  { feed_key: 'feed:other.example/rss', title: 'Another user\'s feed', subscribed: true },
]);
assert.equal(feeds.length, 2, 'own subscriptions remain visible without catalog/set access');
assert.equal(feeds[0].title, hidden.title);
assert.equal(feeds[0].feed_url, hidden.fetch_url);
assert.equal(feeds[1].title, 'Catalog title');
assert.equal(feeds[1].image_url, '/cover.jpg', 'readable catalog metadata remains available');
assert.deepEqual(calls.map(body => [body.epoch, body.since]), [['', 0], ['one', 1]]);
assert(calls.every(body => !body.subscriptions && !body.episodes), 'sidebar reads never send register edits');

API.syncPodcast = async () => {
  resetPodcastSubscriptions();
  return { epoch: 'one', next_since: 1, has_more: false, subscriptions: [hidden] };
};
await assert.rejects(listAccountPodcastSubscriptions(), /Account changed/, 'a response from another account must not populate the sidebar');
let count = 0;
API.syncPodcast = async () => ({ epoch: 'one', next_since: 1, has_more: true, subscriptions: count++ ? [] : [hidden] });
await assert.rejects(listAccountPodcastSubscriptions(), /finish reading/, 'a stuck pull cursor must fail rather than loop');
console.log('podcast sidebar reads own paged subscriptions without granting catalog permissions; account and cursor guards passed');
