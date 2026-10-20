import assert from 'node:assert/strict';
import { API } from '../api.js';
import {
  AutoExplorer,
  normalizeCountries,
  normalizeStation,
  normalizeStations,
  normalizeTags,
  radioSearchQuery,
  stationDetails,
} from '../radio-core.js';

// Query building leaves empty filters to the server and clamps the page size.
assert.equal(radioSearchQuery(), '');
assert.equal(radioSearchQuery({ q: '  jazz fm ', tag: 'jazz', country: 'DE', order: 'votes', limit: 50, offset: 100 }),
  '?q=jazz+fm&tag=jazz&country=DE&order=votes&limit=50&offset=100');
assert.equal(radioSearchQuery({ order: 'bogus', limit: 500, offset: 0 }), '?limit=100');

// Station validation: unplayable entries are dropped, fields get their types.
const raw = {
  uuid: 'a', name: ' Jazz ', stream_url: '/api/radio/stations/a/stream', favicon_url: '/api/radio/stations/a/favicon',
  country: 'Germany', tags: ['jazz', '', 3, ' smooth '], codec: 'MP3', bitrate: 128, votes: -2, bookmarked: true,
};
const station = normalizeStation(raw);
assert.equal(station.name, 'Jazz');
assert.deepEqual(station.tags, ['jazz', 'smooth']);
assert.equal(station.votes, 0);
assert.equal(station.bookmarked, true);
assert.equal(normalizeStation({ ...raw, favicon_url: 'javascript:alert(1)' }).favicon_url, '');
assert.equal(normalizeStation({ ...raw, stream_url: 'http://elsewhere/stream' }), null);
assert.equal(normalizeStation({ ...raw, uuid: '' }), null);
assert.equal(normalizeStation(null), null);
assert.deepEqual(normalizeStations([raw, { ...raw, name: 'dup' }, null, { uuid: 'b', stream_url: '/s' }]).map((s) => s.name),
  ['Jazz', 'Unnamed station']);
assert.throws(() => normalizeStations({}));
assert.equal(stationDetails(station), 'Germany · MP3 128 kbit/s · jazz, smooth');
assert.deepEqual(normalizeTags([{ name: 'rock', station_count: 5 }, { name: '' }, null]), [{ name: 'rock', station_count: 5 }]);
assert.deepEqual(normalizeCountries([{ code: 'de', name: 'Germany', station_count: 9 }, { code: 'XYZ' }]),
  [{ code: 'DE', name: 'Germany', station_count: 9 }]);

// API routes and methods.
const requests = [];
globalThis.location = { pathname: '/', href: '' };
globalThis.fetch = async (url, options) => {
  requests.push([url, options.method, options.credentials]);
  if (options.method === 'DELETE') return new Response(null, { status: 204 });
  return new Response(JSON.stringify(options.method === 'PUT' ? raw : []), { status: 200, headers: { 'content-type': 'application/json' } });
};
await API.radioStations({ q: 'x', limit: 50 });
await API.radioTags();
await API.radioCountries();
await API.radioBookmarks();
assert.deepEqual(await API.bookmarkStation('a/b'), raw);
assert.equal(await API.unbookmarkStation('a'), null);
assert.deepEqual(requests, [
  ['/api/radio/stations?q=x&limit=50', 'GET', 'include'],
  ['/api/radio/tags?limit=100', 'GET', 'include'],
  ['/api/radio/countries', 'GET', 'include'],
  ['/api/radio/bookmarks', 'GET', 'include'],
  ['/api/radio/bookmarks/a%2Fb', 'PUT', 'include'],
  ['/api/radio/bookmarks/a', 'DELETE', 'include'],
]);

// AutoExplorer with fake timers.
function fakeTimers() {
  let id = 0;
  const timers = new Map();
  return {
    setTimer: (fn, ms) => { timers.set(++id, { fn, ms }); return id; },
    clearTimer: (t) => timers.delete(t),
    // run fires the pending timers once (each run is one "tick").
    run() { const due = [...timers]; timers.clear(); due.forEach(([, t]) => t.fn()); return due.map(([, t]) => t.ms); },
    size: () => timers.size,
  };
}
const flush = () => new Promise((resolve) => setImmediate(resolve));
const mk = (uuid) => ({ uuid, name: uuid });

{
  const timers = fakeTimers();
  const played = [];
  const ticks = [];
  let fetches = 0;
  const batches = [[mk('a'), mk('b')], [mk('b'), mk('c')]];
  const explorer = new AutoExplorer({
    fetchBatch: async (filters) => { assert.equal(filters.tag, 'jazz'); return batches[Math.min(fetches++, batches.length - 1)]; },
    play: (s) => played.push(s.uuid),
    onTick: (n) => ticks.push(n),
    dwellSeconds: 15,
    startTimeoutSeconds: 10,
    ...timers,
  });
  await explorer.start({ tag: 'jazz' });
  assert.deepEqual(played, ['a']);
  assert.equal(explorer.active, true);
  // Station "a" starts: 15 one-second ticks lead to the next station.
  explorer.playing('a');
  explorer.playing('a'); // a second "playing" (after a stall) does not restart the count
  assert.deepEqual(ticks, [15]);
  for (let i = 0; i < 14; i++) assert.deepEqual(timers.run(), [1000]);
  assert.deepEqual(played, ['a']);
  timers.run();
  await flush();
  assert.deepEqual(played, ['a', 'b']);
  assert.equal(ticks.at(-1), 1);
  // "b" never starts: the start timeout skips it; the next batch drops the
  // already heard "b".
  assert.deepEqual(timers.run(), [10000]);
  await flush();
  assert.deepEqual(played, ['a', 'b', 'c']);
  // A failure report for a station that is no longer current is ignored.
  explorer.failed('b');
  await flush();
  assert.deepEqual(played, ['a', 'b', 'c']);
  explorer.playing('c');
  explorer.next();
  await flush();
  // Everything was heard: the run starts over, avoiding the current station
  // and "b", which failed to start.
  assert.deepEqual(played, ['a', 'b', 'c', 'c']);
  // A longer dwell does not extend a running countdown.
  explorer.playing('c');
  explorer.setDwell(60);
  assert.equal(explorer.remaining, 15);
  explorer.keep();
  assert.equal(explorer.active, false);
  assert.equal(timers.size(), 0);
  explorer.playing('c');
  explorer.next();
  await flush();
  assert.deepEqual(played, ['a', 'b', 'c', 'c']);
}

{
  // Empty filters and fetch errors end the run with an error.
  const changes = [];
  const explorer = new AutoExplorer({
    fetchBatch: async () => [],
    play: () => assert.fail('nothing to play'),
    onChange: (exp, err) => changes.push([exp.active, err?.message]),
    ...fakeTimers(),
  });
  await explorer.start({});
  assert.deepEqual(changes.at(-1), [false, 'No stations match these filters']);
  const failing = new AutoExplorer({
    fetchBatch: async () => { throw new Error('upstream down'); },
    play: () => {},
    onChange: (exp, err) => changes.push([exp.active, err?.message]),
    ...fakeTimers(),
  });
  await failing.start({});
  assert.deepEqual(changes.at(-1), [false, 'upstream down']);
}

{
  // A filter whose only station is dead ends the run instead of retrying it.
  const changes = [];
  const played = [];
  const explorer = new AutoExplorer({
    fetchBatch: async () => [mk('dead')],
    play: (s) => played.push(s.uuid),
    onChange: (exp, err) => changes.push([exp.active, err?.message]),
    ...fakeTimers(),
  });
  await explorer.start({});
  explorer.failed('dead');
  await flush();
  assert.deepEqual(played, ['dead']);
  assert.deepEqual(changes.at(-1), [false, 'No station of these filters could be played']);
}

{
  // A stop during a slow fetch makes its late result harmless.
  let release;
  const played = [];
  const explorer = new AutoExplorer({
    fetchBatch: () => new Promise((resolve) => { release = resolve; }),
    play: (s) => played.push(s.uuid),
    ...fakeTimers(),
  });
  const run = explorer.start({});
  explorer.keep();
  release([mk('late')]);
  await run;
  assert.deepEqual(played, []);
}

console.log('Radio query, validation, API and auto explore tests passed.');
