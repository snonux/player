import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { API } from '../api.js';
import { state } from '../state.js';
import { selectByElement, currentElement } from '../selection.js';
import { setPodcastAccount } from '../podcast-episode-state.js';
import { initPodcastViews, renderPodcastVirtualView } from '../podcast-views.js';
import { syncPodcastsForGlobal } from '../podcast-global-sync.js';
const requestPodcasts = API.podcasts;

class Node {
  constructor(tag = 'div') {
    this.tagName = tag; this.dataset = {}; this.children = []; this.isConnected = true;
    this.classes = new Set(); this.replacements = 0;
    this.classList = { add: name => this.classes.add(name), remove: name => this.classes.delete(name),
      contains: name => this.classes.has(name), toggle: (name, value) => value ? this.classes.add(name) : this.classes.delete(name) };
  }
  set className(value) { this.classes = new Set(value.split(' ')); }
  set innerHTML(value) {
    if (this.contains(document.activeElement)) document.activeElement = null;
    this.html = value; this.children = []; this.replacements++;
    for (const match of value.matchAll(/<button\b([^>]+)>/g)) {
      const button = new Node('button');
      for (const [attribute, name] of [['data-action', 'action'], ['data-feed-key', 'feedKey']]) {
        const captured = new RegExp(`${attribute}="([^"]+)"`).exec(match[1]);
        if (captured) button.dataset[name] = captured[1];
      }
      this.children.push(button);
    }
  }
  get innerHTML() { return this.html || ''; }
  contains(target) { return this.children.includes(target); }
  focus() { document.activeElement = this; }
  remove() { this.isConnected = false; grid.children = grid.children.filter(child => child !== this); }
  scrollIntoView() {}
  addEventListener() {}
  appendChild(child) { this.children.push(child); }
  querySelectorAll(selector) {
    if (selector === '[data-feed-key]') return this.children.filter(child => child.dataset.feedKey);
    return [];
  }
  querySelector(selector) {
    const action = /\[data-action="([^"]+)"\]/.exec(selector)?.[1];
    return action ? this.children.find(child => child.dataset.action === action) : null;
  }
}
const grid = new Node(), sidebar = new Node(), all = new Node();
const elements = { 'media-grid': grid, 'podcast-nav-list': sidebar, 'podcast-nav': new Node(), 'podcast-all-episodes': all };
globalThis.document = Object.assign(new EventTarget(), {
  activeElement: null, getElementById: id => elements[id] || null, createElement: tag => new Node(tag),
  querySelectorAll: selector => selector.includes('podcast-nav-list') ? sidebar.querySelectorAll('[data-feed-key]')
    : selector.includes('media-card') || selector.includes('episode-card') ? grid.children.filter(child => child.classes.has('media-card')) : [],
});
let libraryReloads = 0;
// Execute app.js's actual event registration. Its loadMedia seam deliberately
// makes the real bug visible by recording every ordinary grid reload.
const app = readFileSync(new URL('../app.js', import.meta.url), 'utf8');
const registration = app.match(/document\.addEventListener\('library:changed', \(\) => loadMedia\(\)\);/)?.[0];
assert.ok(registration);
new Function('document', 'loadMedia', registration)(document, () => libraryReloads++);
let subscriptions = [{ feed_key: 'feed:example/rss', subscribed: true, subscription_ms: 1, title: 'Original' }];
const ep = { id: 1, feed_key: 'feed:example/rss', episode_key: 'guid:a', title: 'Episode', position_seconds: 10,
  completed: false, server_copy: { media_id: 7 } };
API.podcasts = async () => subscriptions;
API.subscribedEpisodes = async () => [{ ...ep }];
API.syncConfig = async () => ({ user_id: 1, podcast_sync: 1 });
let changes = {};
API.syncPodcast = async () => ({ epoch: 'one', next_since: 1, has_more: false, rejected: [], episodes: [], subscriptions: [], ...changes });
setPodcastAccount(1);
initPodcastViews();
const settle = () => new Promise(resolve => setTimeout(resolve, 5));
await settle();
let cursor = { epoch: 'one', since: 1 };
const poll = async () => {
  await syncPodcastsForGlobal(() => true, async page => { cursor = { epoch: page.epoch, since: page.next_since }; }, cursor);
  await settle();
};
const musicCard = new Node(); musicCard.className = 'media-card';
const musicButton = new Node('button'); musicCard.children.push(musicButton); grid.appendChild(musicCard);
state.virtualSet = ''; state.music = { album: 'one' };
selectByElement(musicCard); musicButton.focus();
const sidebarRenders = sidebar.replacements;
await poll(); await poll();
assert.equal(libraryReloads, 0); assert.equal(currentElement(), musicCard); assert.equal(document.activeElement, musicButton);
assert.equal(sidebar.replacements, sidebarRenders, 'unchanged background polls never rebuild sidebar');
subscriptions = [{ ...subscriptions[0], title: 'Updated subscription', subscription_ms: 2 }];
changes = { subscriptions, episodes: [{ ...ep, position: { seconds: 20, observed_ms: 200 } }] };
await poll();
assert.equal(libraryReloads, 0); assert.equal(currentElement(), musicCard); assert.equal(document.activeElement, musicButton);
assert.match(sidebar.innerHTML, /Updated subscription/, 'real sidebar event shows changed subscribed data');

state.virtualSet = 'podcasts'; state.music = null; state.podcastFilter = 'all'; state.mediaPage = 0;
await renderPodcastVirtualView(grid);
const card = grid.children.find(child => child.classes.has('episode-card'));
selectByElement(card); card.querySelector('[data-action="play"]').focus();
changes = { episodes: [{ ...ep, position: { seconds: 30, observed_ms: 300 } }] };
await poll();
assert.match(card.innerHTML, /Resume 00:30/, 'real visible episode card adopts pulled progress');
assert.equal(currentElement(), card); assert.equal(document.activeElement.dataset.action, 'play');
const cardRenders = card.replacements;
await poll(); assert.equal(card.replacements, cardRenders, 'identical episode data does not replace focused controls');
sidebar.querySelectorAll('[data-feed-key]')[0].focus();
subscriptions = [{ ...subscriptions[0], title: 'Updated again', subscription_ms: 3 }]; changes = { subscriptions };
await poll();
assert.match(sidebar.innerHTML, /Updated again/); assert.equal(document.activeElement.dataset.feedKey, ep.feed_key);
assert.equal(currentElement(), card); assert.equal(libraryReloads, 0);

// Exercise the actual API helper through the production sidebar event listener,
// including quiet 401 handling and recovery with identical subscription rows.
API.podcasts = requestPodcasts;
globalThis.location = { pathname: '/', search: '', hash: '', href: 'playing' };
let status = 200, heldResponse;
const requests = [];
globalThis.fetch = async url => {
  requests.push(url);
  if (heldResponse) return heldResponse;
  return new Response(JSON.stringify(status === 200 ? subscriptions : { error: 'Expired session' }),
    { status, headers: { 'Content-Type': 'application/json' } });
};
const sidebarEvent = async () => {
  document.dispatchEvent(new Event('podcast:subscriptions-changed')); await settle();
};
await sidebarEvent();
const retained = sidebar.querySelectorAll('[data-feed-key]')[0]; retained.focus();
const successfulHTML = sidebar.innerHTML, successfulRenders = sidebar.replacements;
status = 401;
await sidebarEvent();
assert.equal(requests.at(-1), '/api/podcasts');
assert.equal(location.href, 'playing', 'actual background sidebar API 401 cannot redirect playback');
assert.equal(sidebar.innerHTML, successfulHTML); assert.equal(document.activeElement, retained);
status = 200; await sidebarEvent();
assert.equal(sidebar.replacements, successfulRenders, 'same success after background failure retains working controls');
assert.equal(sidebar.querySelectorAll('[data-feed-key]')[0], retained);

// An initial error has no successful fingerprint and must recover normally.
sidebar.innerHTML = ''; delete sidebar.dataset.syncFingerprint;
status = 503; await sidebarEvent();
assert.match(sidebar.innerHTML, /Expired session/); assert.equal(sidebar.dataset.syncFingerprint, undefined);
status = 200; await sidebarEvent();
assert.equal(sidebar.querySelectorAll('[data-feed-key]').length, 1);
assert.match(sidebar.innerHTML, /Updated again/);

// A failed old-account continuation cannot erase a newer successful sidebar.
let rejectOld;
heldResponse = new Promise((_resolve, reject) => { rejectOld = reject; });
document.dispatchEvent(new Event('podcast:subscriptions-changed')); await settle();
setPodcastAccount(2);
heldResponse = null; subscriptions = [{ ...subscriptions[0], title: 'Account two', subscription_ms: 4 }];
await sidebarEvent();
const newAccountHTML = sidebar.innerHTML;
rejectOld(new TypeError('old account failed')); await settle();
assert.equal(sidebar.innerHTML, newAccountHTML); assert.match(sidebar.innerHTML, /Account two/);
assert.equal(location.href, 'playing');

// Same-account initial/event overlap also rejects stale successes and errors.
for (const fail of [false, true]) {
  let resolveEarlier, rejectEarlier;
  heldResponse = new Promise((resolve, reject) => { resolveEarlier = resolve; rejectEarlier = reject; });
  document.dispatchEvent(new Event('podcast:subscriptions-changed')); await settle();
  heldResponse = null;
  subscriptions = [{ ...subscriptions[0], title: `Newest ${fail}`, subscription_ms: fail ? 6 : 5 }];
  await sidebarEvent();
  sidebar.querySelectorAll('[data-feed-key]')[0].focus();
  const newestHTML = sidebar.innerHTML, newestFocus = document.activeElement;
  if (fail) rejectEarlier(new TypeError('older same-account refresh failed'));
  else resolveEarlier(new Response(JSON.stringify([{ ...subscriptions[0], title: 'Older response' }]),
    { headers: { 'Content-Type': 'application/json' } }));
  await settle();
  assert.equal(sidebar.innerHTML, newestHTML, 'older same-account continuation cannot replace newer controls');
  assert.equal(document.activeElement, newestFocus);
}

status = 401;
await assert.rejects(API.podcasts(), error => error.status === 401);
assert.match(location.href, /login.html/, 'ordinary interactive podcast request retains its existing auth flow');
setPodcastAccount(null); await settle();
console.log('Actual app/sidebar events preserve focus, quietly handle API 401, recover controls and reject old-account failures.');
