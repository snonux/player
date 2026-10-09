// Unit tests for how playback.js gives the media elements their source:
// plain stream vs. compat stream (probe, preparing state, retry, cancel on
// item switch) and the error toast for failed playback. The DOM, the media
// elements and fetch are minimal fakes; no browser is involved.

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

// --- fake DOM -------------------------------------------------------------

function fakeElement(id = '') {
  const classes = new Set();
  const listeners = {};
  const attrs = {};
  const el = {
    id,
    textContent: '',
    className: '',
    children: [],
    style: { setProperty() {} },
    classList: {
      add: (...names) => names.forEach((n) => classes.add(n)),
      remove: (...names) => names.forEach((n) => classes.delete(n)),
      toggle: (name, force) => { (force ?? !classes.has(name)) ? classes.add(name) : classes.delete(name); },
      contains: (name) => classes.has(name),
    },
    addEventListener(type, handler, options) {
      (listeners[type] ||= []).push({ handler, once: !!options?.once });
    },
    dispatch(type) {
      const current = listeners[type] || [];
      listeners[type] = current.filter((l) => !l.once);
      current.forEach((l) => l.handler({ type, target: el }));
    },
    setAttribute(name, value) { attrs[name] = String(value); },
    getAttribute(name) { return name in attrs ? attrs[name] : null; },
    removeAttribute(name) { delete attrs[name]; },
    appendChild(child) { el.children.push(child); elements[child.id] = child; },
    querySelector: () => null,
  };
  return el;
}

// fakeMedia mimics the parts of HTMLMediaElement the player touches. Like a
// real element it resets readyState/currentTime on every load().
function fakeMedia(id) {
  const el = fakeElement(id);
  Object.assign(el, { paused: true, readyState: 0, currentTime: 0, duration: NaN, error: null, volume: 1, muted: false, playCalls: 0, loads: 0 });
  Object.defineProperty(el, 'src', {
    get: () => el.getAttribute('src') || '',
    set: (value) => el.setAttribute('src', value),
  });
  el.load = () => { el.loads += 1; el.readyState = 0; el.currentTime = 0; el.paused = true; };
  el.pause = () => { el.paused = true; };
  el.play = () => { el.playCalls += 1; el.paused = false; return Promise.resolve(); };
  return el;
}

const elements = {};
function resetDom() {
  for (const key of Object.keys(elements)) delete elements[key];
  for (const id of ['player', 'btn-play', 'big-play', 'cover-art', 'media-image', 'progress-track', 'progress-buffered',
    'progress-fill', 'progress-thumb', 'time-elapsed', 'time-total', 'toast']) {
    elements[id] = fakeElement(id);
  }
  elements['media-video'] = fakeMedia('media-video');
  elements['media-audio'] = fakeMedia('media-audio');
  const stage = fakeElement('stage');
  elements.player.querySelector = (selector) => (selector === '.stage' ? stage : null);
}

globalThis.HTMLMediaElement = { HAVE_METADATA: 1 };
globalThis.document = {
  fullscreenElement: null,
  getElementById: (id) => elements[id] || null,
  createElement: () => fakeElement(),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};
globalThis.window = { addEventListener() {}, location: { origin: 'http://test' } };
console.log = () => {}; // the player logs every media event

// Retry waits (Retry-After: 5) must not slow the tests down; real timers are
// still used so the asynchronous order stays realistic.
const realSetTimeout = globalThis.setTimeout;
globalThis.setTimeout = (fn, _ms, ...args) => realSetTimeout(fn, 0, ...args);
const settle = () => new Promise((resolve) => realSetTimeout(resolve, 20));

// --- fake server ----------------------------------------------------------

function response(status, body = null) {
  return {
    status,
    headers: { get: (name) => (name === 'Retry-After' && status === 503 ? '5' : null) },
    json: async () => {
      if (body === null) throw new Error('no json body');
      return body;
    },
  };
}
const preparing = (status = 'transcoding') => response(503, { error: 'transcode in progress', retry_after_seconds: 5, status });

let requests = [];
let answer = async () => response(206);
globalThis.fetch = (url, init) => {
  requests.push({ url, signal: init?.signal });
  return answer(url, init);
};

function script(...answers) {
  requests = [];
  answer = async () => (answers.length > 1 ? answers.shift() : answers[0]);
}

const player = await import('../playback.js');

function setup() {
  resetDom();
  player.initPlayer();
  script(response(206));
}

const video = () => elements['media-video'];
const audio = () => elements['media-audio'];
const statusText = () => elements['player-status']?.textContent ?? '';
const toastText = () => elements.toast.textContent;

const plainVideo = { id: 1, file_name: 'clip.mp4', type: 'video', duration: 12 };
const aviVideo = { id: 2, file_name: 'clip.avi', type: 'video', duration: 12, transcoded: true };
const wmaAudio = { id: 3, file_name: 'song.wma', type: 'audio', duration: 12, transcoded: true };

// --- tests ----------------------------------------------------------------

async function testPlainStreamIsAssignedDirectly() {
  setup();
  player.selectAndPlay(plainVideo, 0, 30);
  assertEqual(video().src, '/api/media/1/stream', 'plain media gets the stream URL at once');
  assertEqual(video().playCalls, 1, 'plain media starts playing at once');
  assertEqual(requests.length, 0, 'plain media is not probed');
  assertEqual(statusText(), '', 'no preparing state for plain media');
  video().dispatch('loadedmetadata');
  assertEqual(video().currentTime, 30, 'plain media resumes at the saved position');
}

async function testCompatStreamWaitsUntilReady() {
  setup();
  script(preparing(), preparing('busy'), response(206));
  player.selectAndPlay(aviVideo, 0, 42);
  assertEqual(video().getAttribute('src'), null, 'compat URL is not assigned before the probe answers');
  assertEqual(video().playCalls, 0, 'nothing is played while preparing');
  assertEqual(statusText(), 'Preparing clip.avi for playback…', 'preparing state is visible immediately');
  await settle();
  assertEqual(requests.length, 3, '503 answers are retried until the stream is ready');
  assert(requests.every((r) => r.url === '/api/media/2/compat'), 'the compat URL is probed');
  assertEqual(video().src, '/api/media/2/compat', 'compat URL is assigned once ready');
  assertEqual(video().playCalls, 1, 'the requested playback starts once ready');
  assertEqual(statusText(), '', 'preparing state is removed once ready');
  assertEqual(toastText(), 'Preparing clip.avi for playback…', 'first 503 raises a preparing toast');
  assert(elements.toast.className.includes('info'), 'preparing toast is informational');
  video().readyState = 1;
  video().dispatch('loadedmetadata');
  assertEqual(video().currentTime, 42, 'resume position is applied to the compat stream');
}

async function testBusyStatusIsShown() {
  setup();
  let release;
  script(preparing('busy'));
  answer = (() => {
    let first = true;
    return () => {
      if (first) { first = false; return Promise.resolve(preparing('busy')); }
      return new Promise((resolve) => { release = resolve; });
    };
  })();
  player.selectAndPlay(wmaAudio, 0);
  await settle();
  assertEqual(statusText(), 'Server is busy, song.wma is waiting to be prepared…', 'busy 503 keeps a preparing state');
  assertEqual(audio().getAttribute('src'), null, 'audio stays without source while busy');
  release(response(200));
  await settle();
  assertEqual(audio().src, '/api/media/3/compat', 'audio compat URL is assigned once ready');
  assertEqual(audio().playCalls, 1, 'audio starts once ready');
}

async function testTerminalCompatErrorShowsToast() {
  setup();
  script(response(500, { error: 'transcode failed' }));
  player.selectAndPlay(aviVideo, 0);
  await settle();
  assertEqual(toastText(), 'Cannot play clip.avi: the server could not convert this file', '500 shows the error toast');
  assert(elements.toast.className.includes('error'), 'failure toast uses the error style');
  assertEqual(video().getAttribute('src'), null, 'a failed compat stream is never assigned');
  assertEqual(video().playCalls, 0, 'a failed compat stream is never played');
  assertEqual(elements['btn-play'].textContent, '▶', 'play button is reset after the failure');
  assert(!elements['big-play'].classList.contains('hidden'), 'big play prompt is shown again');
  assertEqual(player.isPlaybackActive(), false, 'playback state is reset after the failure');
  assertEqual(statusText(), '', 'preparing state is removed after the failure');
  assertEqual(requests.length, 1, 'a terminal status is not retried');

  // Pressing play now must not "play" the empty element.
  player.togglePlay();
  assertEqual(video().playCalls, 0, 'toggle play ignores an element without source');
}

async function testSwitchWhilePreparingCancels() {
  setup();
  let release;
  answer = (url) => (url.endsWith('/compat') ? new Promise((resolve) => { release = resolve; }) : Promise.resolve(response(206)));
  player.selectAndPlay(aviVideo, 0);
  const signal = requests[0].signal;
  player.selectAndPlay(plainVideo, 1);
  assert(signal.aborted, 'switching items aborts the pending probe');
  assertEqual(statusText(), '', 'preparing state is removed on switch');
  assertEqual(video().src, '/api/media/1/stream', 'the new item is loaded');
  const playsBefore = video().playCalls;
  release(response(206));
  await settle();
  assertEqual(video().src, '/api/media/1/stream', 'a late ready answer does not replace the new item');
  assertEqual(video().playCalls, playsBefore, 'a late ready answer does not start playback');
  assertEqual(toastText(), '', 'a cancelled probe shows no toast');
}

async function testSwitchDuringRetryWaitStopsRetries() {
  setup();
  script(preparing());
  player.selectAndPlay(aviVideo, 0);
  await new Promise((resolve) => realSetTimeout(resolve, 5));
  const seen = requests.length;
  assert(seen >= 1, 'probing started');
  player.stopAndClose();
  await settle();
  assert(requests.length <= seen + 1, `closing the player stops the retries (${seen} -> ${requests.length})`);
  const after = requests.length;
  await settle();
  assertEqual(requests.length, after, 'no further probes after close');
  assertEqual(video().getAttribute('src'), null, 'closing while preparing leaves no source behind');
}

async function testElementErrorShowsToast() {
  setup();
  player.selectAndPlay(plainVideo, 0);
  video().dispatch('play');
  assertEqual(elements['btn-play'].textContent, '⏸', 'playing state before the error');
  video().error = { code: 3, message: 'PIPELINE_ERROR_DECODE' };
  video().dispatch('error');
  assertEqual(toastText(), 'Cannot play clip.mp4: the file is damaged or cannot be decoded', 'decode error shows the toast');
  assert(elements.toast.className.includes('error'), 'element error toast uses the error style');
  assertEqual(elements['btn-play'].textContent, '▶', 'play button is reset after an element error');
  assertEqual(player.isPlaybackActive(), false, 'playback state is reset after an element error');
  assert(video().paused, 'the failed element is paused');
}

async function testIdleElementErrorIsIgnored() {
  setup();
  player.selectAndPlay(plainVideo, 0);
  // Clearing the idle element's source makes browsers fire "empty src".
  assertEqual(audio().getAttribute('src'), '', 'idle element source is cleared');
  audio().error = { code: 4, message: 'MEDIA_ELEMENT_ERROR: Empty src attribute' };
  audio().dispatch('error');
  assertEqual(toastText(), '', 'an error of the idle element shows no toast');
  assertEqual(player.isPlaybackActive(), true, 'an error of the idle element keeps the playback state');
}

async function testDirectLoadProbesShareCompatUrl() {
  setup();
  script(preparing(), response(206));
  // Share page / detached popup: explicit URL, no autoplay until requested.
  player.loadMediaDirect({ id: 9, file_name: 'shared.wmv', type: 'video', transcoded: true }, '/s/tok/compat', '', 7);
  assertEqual(video().getAttribute('src'), null, 'share compat URL is not assigned before it is ready');
  player.togglePlay();
  await settle();
  assert(requests.every((r) => r.url === '/s/tok/compat'), 'the share playback URL is probed');
  assertEqual(video().src, '/s/tok/compat', 'share compat URL is assigned once ready');
  assertEqual(video().playCalls, 1, 'a play request made while preparing is honoured once ready');

  setup();
  player.loadMediaDirect({ id: 9, file_name: 'shared.mp4', type: 'video', transcoded: false }, '/s/tok/stream', '', 0);
  assertEqual(video().src, '/s/tok/stream', 'plain share stream is assigned directly');
  assertEqual(video().playCalls, 0, 'direct load does not autoplay');
  assertEqual(requests.length, 0, 'plain share stream is not probed');
}

console.log = () => {};
console.info('Running playback source tests...');
for (const test of [
  testPlainStreamIsAssignedDirectly,
  testCompatStreamWaitsUntilReady,
  testBusyStatusIsShown,
  testTerminalCompatErrorShowsToast,
  testSwitchWhilePreparingCancels,
  testSwitchDuringRetryWaitStopsRetries,
  testElementErrorShowsToast,
  testIdleElementErrorIsIgnored,
  testDirectLoadProbesShareCompatUrl,
]) {
  try {
    await test();
  } catch (err) {
    failures.push(`${test.name} threw: ${err?.stack || err}`);
  }
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.info('All playback source tests passed.');
  process.exit(0);
}
