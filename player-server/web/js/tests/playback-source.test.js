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
    prepend(child) { el.children.unshift(child); elements[child.id] = child; },
    querySelector: selector => el.children.find(child => selector === `.${child.className}`) || null,
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
  el.load = () => { el.loads += 1; el.readyState = 0; el.currentTime = 0; el.paused = true; el.error = null; };
  el.pause = () => { el.paused = true; };
  el.play = () => { el.playCalls += 1; el.paused = false; return Promise.resolve(); };
  return el;
}

const elements = {};
function resetDom() {
  for (const key of Object.keys(elements)) delete elements[key];
  for (const id of ['player', 'btn-play', 'big-play', 'cover-art', 'media-image', 'progress-track', 'progress-buffered',
    'progress-fill', 'progress-thumb', 'time-elapsed', 'time-total', 'toast', 'btn-cast', 'btn-restore-player', 'player-restore-title']) {
    elements[id] = fakeElement(id);
  }
  elements['media-video'] = fakeMedia('media-video');
  elements['media-audio'] = fakeMedia('media-audio');
  const stage = fakeElement('stage');
  elements.player.querySelector = selector => selector === '.stage' ? stage : null;
}

globalThis.CSS = { escape: String };
globalThis.HTMLMediaElement = { HAVE_METADATA: 1 };
const documentListeners = {};
globalThis.CustomEvent = class extends Event { constructor(type, options) { super(type); this.detail = options?.detail; } };
globalThis.document = {
  head: {appendChild() {}},
  fullscreenElement: null,
  getElementById: (id) => elements[id] || null,
  createElement: () => fakeElement(),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener(type, fn) { (documentListeners[type] ||= []).push(fn); },
  dispatchEvent(event) { for (const fn of documentListeners[event.type] || []) fn(event); return true; },
};
globalThis.window = { addEventListener() {}, location: { origin: 'http://test' } };
console.log = () => {}; // the player logs every media event

// Retry waits (Retry-After: 5) must not slow the tests down; real timers are
// still used so the asynchronous order stays realistic.
const realSetTimeout = globalThis.setTimeout;
globalThis.setTimeout = (fn, _ms, ...args) => realSetTimeout(fn, 0, ...args);
const settle = () => new Promise((resolve) => realSetTimeout(resolve, 20));

// --- fake server ----------------------------------------------------------

// A HEAD response of the compat endpoint: status and headers, no body.
function response(status, headers = {}) {
  return { status, headers: { get: (name) => headers[name] ?? null } };
}
const preparing = (status = 'transcoding') => response(503, { 'Retry-After': '5', 'X-Transcode-Status': status });

let requests = [];
let answer = async () => response(200);
globalThis.fetch = (url, init) => {
  requests.push({ url, method: init?.method, signal: init?.signal });
  return answer(url, init);
};

function script(...answers) {
  requests = [];
  answer = async () => (answers.length > 1 ? answers.shift() : answers[0]);
}

const osActions = {};
const os = { metadata: null, setActionHandler(action, callback) { osActions[action] = callback; }, setPositionState() {} };
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { mediaSession: os } });
globalThis.MediaMetadata = class { constructor(fields) { Object.assign(this, fields); } };

const player = await import('../playback.js');
const { API } = await import('../api.js');
const queue = await import('../queue.js');
queue.initQueue();

let failureReports = 0;
function setup(options = {}) {
  resetDom();
  failureReports = 0;
  globalThis.location = { pathname: '/', href: '/' };
  player.initPlayer({ onPlaybackFailed: () => { failureReports += 1; }, ...options });
  script(response(200));
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
  script(preparing(), preparing('busy'), response(200));
  player.selectAndPlay(aviVideo, 0, 42);
  assertEqual(video().getAttribute('src'), null, 'compat URL is not assigned before the probe answers');
  assertEqual(video().playCalls, 0, 'nothing is played while preparing');
  assertEqual(statusText(), 'Preparing clip.avi for playback…', 'preparing state is visible immediately');
  await settle();
  assertEqual(requests.length, 3, '503 answers are retried until the stream is ready');
  assert(requests.every((r) => r.url === '/api/media/2/compat' && r.method === 'HEAD'), 'the compat URL is probed with HEAD');
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
  answer = (() => {
    let first = true;
    return () => {
      if (first) { first = false; return Promise.resolve(preparing('busy')); }
      return new Promise((resolve) => { release = resolve; });
    };
  })();
  player.selectAndPlay(wmaAudio, 0);
  await settle();
  assertEqual(statusText(), 'Server is busy, song is waiting to be prepared…', 'busy 503 keeps a preparing state');
  assertEqual(audio().getAttribute('src'), null, 'audio stays without source while busy');
  release(response(200));
  await settle();
  assertEqual(audio().src, '/api/media/3/compat', 'audio compat URL is assigned once ready');
  assertEqual(audio().playCalls, 1, 'audio starts once ready');
}

async function testTerminalCompatErrorShowsToast() {
  setup();
  script(response(500));
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
  assertEqual(failureReports, 1, 'the failure is reported to onPlaybackFailed');
  assertEqual(globalThis.location.href, '/', 'a 500 does not redirect to the login page');
}

// After a failure the play button / big play / Space (all togglePlay) load
// the item again instead of silently doing nothing.
async function testTogglePlayRetriesFailedCompatStream() {
  setup();
  script(response(500), preparing(), response(200));
  player.selectAndPlay(aviVideo, 0, 42);
  await settle();
  assertEqual(video().getAttribute('src'), null, 'failed first');
  player.togglePlay();
  assertEqual(video().playCalls, 0, 'the empty element itself is never played');
  assertEqual(statusText(), 'Preparing clip.avi for playback…', 'retry shows the preparing state again');
  assertEqual(elements['btn-play'].textContent, '⏸', 'retry shows the play intent');
  await settle();
  assertEqual(requests.length, 3, 'retry probes again until ready');
  assertEqual(video().src, '/api/media/2/compat', 'retry assigns the compat URL once ready');
  assertEqual(video().playCalls, 1, 'retry plays once ready');
  video().readyState = 1;
  video().dispatch('loadedmetadata');
  assertEqual(video().currentTime, 42, 'retry keeps the resume position');
}

async function testTogglePlayRetriesAfterElementError() {
  setup();
  player.selectAndPlay(plainVideo, 0);
  video().currentTime = 17;
  video().error = { code: 2, message: 'network' };
  video().dispatch('error');
  const loads = video().loads;
  const plays = video().playCalls;
  player.togglePlay();
  assertEqual(video().loads, loads + 1, 'toggle play reloads an element in error state');
  assertEqual(video().src, '/api/media/1/stream', 'the same stream is loaded again');
  assertEqual(video().error, null, 'the reload clears the error');
  assertEqual(video().playCalls, plays + 1, 'the reloaded element is played');
  video().readyState = 1;
  video().dispatch('loadedmetadata');
  assertEqual(video().currentTime, 17, 'the reload resumes where the error happened');
}

async function testExpiredSessionRedirectsLibraryPlaybackOnly() {
  setup();
  script(response(401));
  player.selectAndPlay(aviVideo, 0);
  await settle();
  assertEqual(globalThis.location.href, '/login.html', 'a 401 on library playback goes to the login page');

  // Share page and detached popup load directly: never navigate away.
  setup();
  script(response(401));
  player.loadMediaDirect({ id: 9, file_name: 'shared.wmv', type: 'video', transcoded: true }, '/s/tok/compat', '', 0);
  await settle();
  assertEqual(globalThis.location.href, '/', 'a 401 on a share/popup URL does not redirect');
  assertEqual(toastText(), 'Cannot play shared.wmv: you are not signed in', 'a 401 on a share/popup URL shows the toast');
}

async function testSwitchWhilePreparingCancels() {
  setup();
  let release;
  answer = (url) => (url.endsWith('/compat') ? new Promise((resolve) => { release = resolve; }) : Promise.resolve(response(200)));
  player.selectAndPlay(aviVideo, 0);
  const signal = requests[0].signal;
  player.selectAndPlay(plainVideo, 1);
  assert(signal.aborted, 'switching items aborts the pending probe');
  assertEqual(statusText(), '', 'preparing state is removed on switch');
  assertEqual(video().src, '/api/media/1/stream', 'the new item is loaded');
  const playsBefore = video().playCalls;
  release(response(200));
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
  script(preparing(), response(200));
  // Share page / detached popup: explicit URL, no autoplay until requested.
  player.loadMediaDirect({ id: 9, file_name: 'shared.wmv', type: 'video', transcoded: true }, '/s/tok/compat', '', 7);
  assertEqual(video().getAttribute('src'), null, 'share compat URL is not assigned before it is ready');
  assertEqual(elements['btn-play'].textContent, '▶', 'a direct load waits for a play press');
  assert(!elements['big-play'].classList.contains('hidden'), 'a direct load shows the big play prompt');
  assertEqual(player.hasPendingPlay(), false, 'no play intent before the press');
  player.togglePlay();
  assertEqual(player.hasPendingPlay(), true, 'the press is remembered while preparing');
  assertEqual(elements['btn-play'].textContent, '⏸', 'the remembered press is shown');
  await settle();
  assert(requests.every((r) => r.url === '/s/tok/compat' && r.method === 'HEAD'), 'the share playback URL is probed with HEAD only');
  assertEqual(player.hasPendingPlay(), false, 'no pending play once the stream is attached');
  assertEqual(video().src, '/s/tok/compat', 'share compat URL is assigned once ready');
  assertEqual(video().playCalls, 1, 'a play request made while preparing is honoured once ready');

  setup();
  player.loadMediaDirect({ id: 9, file_name: 'shared.mp4', type: 'video', transcoded: false }, '/s/tok/stream', '', 0);
  assertEqual(video().src, '/s/tok/stream', 'plain share stream is assigned directly');
  assertEqual(video().playCalls, 0, 'direct load does not autoplay');
  assertEqual(requests.length, 0, 'plain share stream is not probed');
}

async function testLocalFlushDoesNotOverwriteRemoteOwners() {
  const progress = API.progress, ticket = API.castTicket;
  let writes = 0;
  const positions = [];
  try {
    API.progress = async (id, position) => { writes++; positions.push([id, position]); };
    setup();
    player.selectAndPlay({ id: 6, type: 'audio', file_name: 'song.mp3' }, 0);
    audio().currentTime = 5;
    const popup = { closed: false, postMessage() {}, close() { this.closed = true; }, __playerDetachCurrentState: () => ({media:{id:6},currentTime:37}) };
    window.open = () => popup;
    player.toggleDetach();
    player.selectAndPlay(plainVideo, 0);
    await settle();
    assertEqual(writes, 0, 'the detached popup owns progress, not the main stale element');
    player.stopAndClose(); await settle();
    assert(popup.closed, 'closing active queue source closes detached output without reattaching');
    assertEqual(positions.at(-1)?.[0], 6, 'detached stop flush belongs to actual popup media');
    assertEqual(positions.at(-1)?.[1], 37, 'detached stop uses authoritative popup position');
    writes = 0;
    popup.closed = true;
    delete window.open;

    setup();
    const cast = await import('../cast.js');
    const listeners = {};
    let session = { loadMedia: async () => {} };
    const context = {
      setOptions() {}, getCastState: () => 'none', getCurrentSession: () => session,
      addEventListener: (type, listener) => { listeners[type] = listener; },
      endCurrentSession: () => { session = null; listeners.session({ sessionState: 'ended' }); },
    };
    function Value() {}
    window.chrome = { cast: { AutoJoinPolicy: {}, media: {
      DEFAULT_MEDIA_RECEIVER_APP_ID: 'default', StreamType: { BUFFERED: 'buffered' },
      PlayerState: { IDLE: 'idle' }, MediaInfo: Value, LoadRequest: Value,
      GenericMediaMetadata: Value, MusicTrackMediaMetadata: Value,
    } } };
    window.isSecureContext = true;
    window.cast = { framework: {
      CastContext: { getInstance: () => context },
      CastContextEventType: { CAST_STATE_CHANGED: 'cast', SESSION_STATE_CHANGED: 'session' },
      CastState: { NO_DEVICES_AVAILABLE: 'none' },
      SessionState: { SESSION_STARTED: 'started', SESSION_ENDED: 'ended' },
      RemotePlayer: function () { return { isPaused: true }; },
      RemotePlayerController: function () { return { addEventListener() {} }; },
      RemotePlayerEventType: {},
    } };
    cast.initCast({ button: fakeElement('cast') }, window,
      { createElement: () => ({}), head: { appendChild() {} } });
    window.__onGCastApiAvailable(true);
    API.castTicket = async () => ({ ticket: 'local-test', stream_content_type: 'audio/mpeg' });
    player.selectAndPlay({ id: 6, type: 'audio', file_name: 'song.mp3' }, 0);
    audio().currentTime = 5;
    await cast.castItem({ id: 6, type: 'audio', file_name: 'song.mp3' }, 60);
    assert(cast.isCasting(), 'test receiver owns the playback session');
    player.selectAndPlay(plainVideo, 0);
    await settle();
    assertEqual(writes, 0, 'the receiver owns progress, stale local 5s cannot overwrite remote 60s');
    player.stopAndClose(); await settle();
    assert(!cast.isCasting(), 'closing active queue source ends cast receiver session');
    assertEqual(writes, 0, 'cast stop does not write stale local progress');
  } finally {
    API.progress = progress; API.castTicket = ticket;
    delete window.chrome; delete window.cast; delete window.isSecureContext;
  }
}

async function testInactiveOrStaleEndedEventCannotFinishCurrentAudio() {
  let writes = 0, advanced = 0;
  setup({ onNext: () => { advanced++; } });
  const progress = API.progress, status = API.progressStatus;
  try {
    API.progress = async () => { writes++; };
    API.progressStatus = async () => { writes++; };
    player.selectAndPlay({ id: 6, type: 'audio', file_name: 'song.mp3' }, 0);
    video().currentTime = 12; video().ended = true;
    video().dispatch('ended');
    audio().currentTime = 3; audio().ended = false;
    audio().dispatch('ended');
    await settle();
    assertEqual(writes, 0, 'idle old video and stale active events cannot finish current audio');
    assertEqual(advanced, 0, 'inactive or stale ended events cannot advance');
    assertEqual(player.isPlaybackActive(), true, 'stale events cannot stop active playback state');
  } finally { API.progress = progress; API.progressStatus = status; }
}

async function testCompletionPrecedesAdvanceWithoutReopeningFinished() {
  const calls = [];
  let release;
  setup({ onNext: () => { calls.push('next'); player.selectAndPlay(plainVideo, 0); } });
  const progress = API.progress, status = API.progressStatus;
  try {
    API.progress = (id, position) => {
      calls.push(`position:${id}:${position}`);
      return new Promise(resolve => { release = resolve; });
    };
    API.progressStatus = async (id, state) => { calls.push(`${state}:${id}`); };
    player.selectAndPlay({ id: 6, type: 'audio', file_name: 'song.mp3' }, 0);
    audio().currentTime = 12;
    audio().ended = true;
    audio().dispatch('ended');
    await settle();
    assertEqual(calls.join(','), 'position:6:12', 'advance waits for the final save');
    release();
    await settle();
    assertEqual(calls.join(','), 'position:6:12,finished:6,next', 'finish is before next and replacement does not reopen the old track');
  } finally { API.progress = progress; API.progressStatus = status; }
}

async function testDelayedCompletionCannotAdvanceAReplacement() {
  let release, advanced = 0;
  setup({ onNext: () => { advanced++; } });
  const progress = API.progress, status = API.progressStatus;
  try {
    API.progress = () => new Promise(resolve => { release = resolve; });
    API.progressStatus = async () => {};
    player.selectAndPlay({ id: 6, type: 'audio', file_name: 'song.mp3' }, 0);
    audio().currentTime = 12; audio().ended = true;
    audio().dispatch('ended');
    await settle();
    player.selectAndPlay(plainVideo, 0);
    release(); await settle();
    assertEqual(advanced, 0, 'a delayed old completion cannot navigate away from the new source');
  } finally { API.progress = progress; API.progressStatus = status; }
}

console.log = () => {};
async function testAlbumCollectionSurvivesBrowseButNotRestoredSource() {
  setup();
  const nav = await import('../views/playback-nav.js');
  const { state } = await import('../state.js');
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.music = { album: 'al_test' };
  const songs = [101, 102, 103].map(id => ({ id, type: 'audio', file_name: `${id}.mp3`, duration: 12 }));
  await nav.playAlbumCollection(songs, 0, 'Album');
  // Ordinary browsing changes the visible list but leaves the source alone.
  state.music = null;
  state.media = [{ id: 999, type: 'audio', file_name: 'restored.mp3', duration: 12 }];
  await nav.navigatePlayable(1, { forcePlay: true });
  assertEqual(player.currentMediaId(), 102, 'browsing another list retains the album snapshot');
  // This is the actual replacement used by navigation.restore for a different
  // history media ID, not merely a navigation event or visible-list mutation.
  player.selectMedia(state.media[0], 0, 0);
  await nav.navigatePlayable(1, { forcePlay: true });
  assertEqual(player.currentMediaId(), 999, 'history source replacement discards stale album entries');
}

async function testRapidNextDuringLongAlbumResumeKeepsCollection() {
  setup();
  const nav = await import('../views/playback-nav.js');
  const { state } = await import('../state.js');
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.music = null;
  state.media = [];
  let resolveProgress;
  const progress = new Promise(resolve => { resolveProgress = resolve; });
  const originalDetail = API.mediaDetail;
  API.mediaDetail = id => id === 202 ? progress : Promise.resolve({ progress: null });
  try {
    const songs = [201, 202, 203].map(id => ({ id, type: 'audio', file_name: `${id}.mp3`, duration: id === 202 ? 1300 : 12 }));
    await nav.playAlbumCollection(songs, 0, 'Long album');
    const pending = nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 201, 'old source remains active during long-track resume read');
    await nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 203, 'rapid Next retains collection while the intermediate target is pending');
    resolveProgress({ progress: { position_seconds: 300 } });
    await pending;
    assertEqual(player.currentMediaId(), 203, 'stale progress read cannot replace the newer target');
  } finally { API.mediaDetail = originalDetail; }
}

async function testEditableQueueIdentityOwnershipAndProgress() {
  setup();
  const songs = [301, 302, 301].map(id => ({id, type:'audio', file_name:`${id}.mp3`, duration:12}));
  const oldProgress = API.progress, writes = [];
  API.progress = async (id, position) => writes.push([id,position]);
  try {
    await queue.playAlbumCollection(songs,0,'Editable');
    const first = queue.queueState().entries[0].id;
    audio().currentTime = 4;
    const loads = audio().loads;
    queue.moveQueue(0,1);
    assertEqual(queue.queueState().entries[1].id, first, 'move preserves occurrence identity');
    assertEqual(queue.queueState().index, 1, 'current index follows moved occurrence');
    assertEqual(audio().loads, loads, 'move does not reload current source');
    queue.removeQueue(2);
    assertEqual(audio().loads, loads, 'removing duplicate occurrence does not remove current');
    queue.undoQueue();
    assertEqual(queue.queueState().entries.length, 3, 'Undo restores removed occurrence');
    queue.removeQueue(1);
    await settle();
    assertEqual(player.currentMediaId(), undefined, 'removing current stops audio');
    assert(writes.some(([id,p]) => id === 301 && p === 4), 'current removal flushes old media progress');
    queue.undoQueue();
    assertEqual(player.currentMediaId(), undefined, 'Undo never restarts removed source');
    assertEqual(queue.queueState().entries[1].id, first, 'Undo restores entry ID');
    await queue.jumpQueue(1);
    player.selectAndPlay({id:999,type:'audio',file_name:'Sets.mp3',duration:12},0);
    assertEqual(queue.queueState().entries.length, 3, 'Sets takeover retains queue contents');
    assertEqual(queue.queueState().active, false, 'Sets takeover deactivates queue');
    audio().currentTime = 6;
    const before = writes.length;
    queue.removeQueue(1); queue.undoQueue(); queue.clearQueue(); queue.undoQueue();
    await settle();
    assertEqual(player.currentMediaId(),999,'inactive edits leave unrelated source untouched');
    assertEqual(audio().currentTime,6,'inactive edits cannot seek unrelated source');
    assertEqual(writes.length,before,'inactive edits cannot flush unrelated source progress');
    await queue.jumpQueue(0);
    assertEqual(player.currentMediaId(),302,'explicit jump resumes retained edited order');
    queue.clearQueue(); await settle();
    assertEqual(queue.queueState().entries.length,0,'Clear empties queue');
    queue.undoQueue();
    assertEqual(queue.queueState().entries.length,3,'Clear Undo restores contents');
    assertEqual(player.currentMediaId(),undefined,'Clear Undo stays stopped');
  } finally { API.progress = oldProgress; }
}
async function testInitialLongAlbumPlayAndPendingReplacement() {
  setup();
  player.selectAndPlay({id:998,type:'audio',file_name:'Sets.mp3'},0);
  const original = API.mediaDetail;
  let resolve;
  API.mediaDetail = () => new Promise(done => { resolve = done; });
  try {
    const pending = queue.playAlbumCollection([{id:401,type:'audio',duration:1300},{id:402,type:'audio',duration:12}],0);
    await queue.advanceQueue(1);
    assertEqual(player.currentMediaId(),402,'initial long resume Next belongs to intended queue');
    resolve({progress:{position_seconds:30}}); await pending;
    assertEqual(player.currentMediaId(),402,'late initial read cannot replace skipped target');
    const blocked = queue.jumpQueue(0);
    player.selectMedia({id:997,type:'video',file_name:'video.mp4'},0);
    resolve({progress:{position_seconds:50}}); await blocked;
    assertEqual(player.currentMediaId(),997,'history/source takeover cancels pending queue resume');
    assertEqual(queue.queueState().entries.length,2,'pending takeover keeps inactive contents');
  } finally { API.mediaDetail = original; }
}

async function testLatestQueueIntentCancelsPendingSetsResume() {
  const nav = await import('../views/playback-nav.js');
  const { state } = await import('../state.js');
  const original = API.mediaDetail;
  for (const retained of [false,true]) {
    setup();
    const songs=[702,703].map(id=>({id,type:'audio',file_name:`${id}.mp3`,duration:12}));
    if (retained) await queue.playAlbumCollection(songs,0,'Retained');
    state.media=[{id:701,type:'audio',file_name:'Sets.mp3',duration:1300}];
    let resolve;
    API.mediaDetail=()=>new Promise(done=>{resolve=done;});
    const pending=nav.playMediaById(701);
    if (retained) await queue.jumpQueue(1); else await queue.playAlbumCollection(songs,0,'New album');
    const expected=retained ? 703 : 702;
    assertEqual(player.currentMediaId(),expected,'new queue intent owns source before old Sets read');
    resolve({progress:{position_seconds:27}}); await pending;
    assertEqual(player.currentMediaId(),expected,`${retained ? 'retained queue jump' : 'new album play'} cancels older Sets resume`);
  }
  setup();
  await queue.playAlbumCollection([{id:704,type:'audio',duration:12}],0,'Retained');
  state.media=[{id:705,type:'audio',file_name:'pendingSets.mp3',duration:1300}];
  let finish;
  API.mediaDetail=()=>new Promise(done=>{finish=done;});
  const sets=nav.playMediaById(705);
  queue.clearQueue(); queue.undoQueue();
  finish({progress:{position_seconds:13}}); await sets;
  assertEqual(player.currentMediaId(),705,'inactive queue clear/Undo cannot cancel pending unrelated Sets intent');
  API.mediaDetail=original;
}

async function testUndoCannotResurrectInvalidatedFIFO() {
  const song=id=>({id,type:'audio',file_name:`${id}.mp3`,duration:12});
  for (const mode of ['jump','backward','natural']) {
    setup(); await queue.playAlbumCollection([song(901),song(902)],0);
    await queue.insertQueue([song(903),song(904)],'next');
    if (mode === 'backward') await queue.advanceQueue(1);
    await queue.insertQueue([song(905)],'append');
    if (mode === 'jump') await queue.jumpQueue(0);
    if (mode === 'backward') await queue.advanceQueue(-1);
    if (mode === 'natural') await queue.advanceQueue(1);
    queue.undoQueue();
    await queue.insertQueue([song(906)],'next');
    assertEqual(queue.queueState().entries.map(entry=>entry.media.id).join(), mode==='natural' ? '901,903,904,906,902' : '901,906,903,904,902', `${mode}: Undo content cannot resurrect an explicitly invalidated FIFO, natural advance retains remaining FIFO`);
    assertEqual(queue.queueState().entries.filter(entry=>entry.upNext).map(entry=>entry.media.id).join(),mode==='natural' ? '904,906' : '906',`${mode}: only valid future occurrences carry FIFO markers`);
  }
}

async function testSupersededPlayNowDoesNotConfirmOrCaptureNewUndo() {
  setup();
  const song = (id,duration=12) => ({id,duration,type:'audio',file_name:`${id}.mp3`});
  await queue.playAlbumCollection([song(850)],0);
  const original=API.mediaDetail; let resolve;
  API.mediaDetail=()=>new Promise(done=>resolve=done);
  try {
    const pending=queue.insertQueue([song(851,1300)],'now');
    await queue.playAlbumCollection([song(852)],0,'New album');
    await queue.insertQueue([song(853)],'append');
    const token=queue.queueUndoToken();
    resolve({progress:{position_seconds:60}});
    assertEqual(await pending,false,'superseded insertion reports canceled instead of confirmation');
    assertEqual(queue.queueUndoToken(),token,'stale insertion cannot replace new Undo token');
    assertEqual(player.currentMediaId(),852,'stale insertion cannot replace newer playback');
  } finally {API.mediaDetail=original;}
}

async function testExplicitInsertionFIFOAndUndo() {
  setup();
  const song = id => ({id,type:'audio',file_name:`${id}.mp3`,duration:12});
  const ids = () => queue.queueState().entries.map(entry=>entry.media.id).join();
  await queue.playAlbumCollection([song(801),song(802)],0,'Insertion');
  audio().currentTime=4;
  const loads=audio().loads, original=queue.queueState().entries[0].id;
  await queue.insertQueue([song(803),song(804)],'next');
  await queue.insertQueue([song(803)],'next');
  await queue.insertQueue([song(805)],'append');
  assertEqual(ids(),'801,803,804,803,802,805','successive Next blocks are FIFO and Append is tail');
  assertEqual(audio().loads,loads,'Next/Append never reload source');
  assertEqual(audio().currentTime,4,'Next/Append preserve position');
  assertEqual(queue.queueState().entries[0].id,original,'active occurrence survives insertion');
  assertEqual(new Set(queue.queueState().entries.map(entry=>entry.id)).size,6,'duplicates have independent entry IDs');
  await queue.advanceQueue(1);
  await queue.insertQueue([song(806)],'next');
  assertEqual(ids(),'801,803,804,803,806,802,805','advance consumes one pending occurrence and preserves remaining FIFO');
  await queue.advanceQueue(-1);
  await queue.insertQueue([song(807)],'next');
  assertEqual(ids(),'801,807,803,804,803,806,802,805','backward transport starts a new FIFO block');
  const before=ids();
  await queue.insertQueue([song(808)],'now');
  assertEqual(player.currentMediaId(),808,'Play now starts inserted song');
  assert(ids().includes('802,805'),'Play now preserves original remainder');
  queue.undoQueue(); await settle();
  assertEqual(ids(),before,'Undo Play now restores previous contents');
  assertEqual(player.currentMediaId(),undefined,'Undo inserted current stays paused');
  player.selectAndPlay(song(899),0); audio().currentTime=6;
  await queue.insertQueue([song(810)],'append');
  assertEqual(player.currentMediaId(),899,'inactive append never takes Sets source');
  assertEqual(audio().currentTime,6,'inactive append cannot seek Sets');
  const token=queue.queueUndoToken();
  queue.moveQueue(0,1); const moved=ids(); queue.undoQueue(token);
  assertEqual(ids(),moved,'stale toast Undo cannot undo a later queue edit');
  queue.clearQueue();
  await queue.insertQueue([song(811)],'next');
  assertEqual(player.currentMediaId(),899,'empty Next does not start playback');
  assertEqual(ids(),'811','empty Next stores song');
  await queue.insertQueue([song(812)],'next');
  assertEqual(ids(),'811,812','empty queue FIFO keeps first added song first');
}

async function testRecoveredResumeRetainsFutureFIFO() {
 setup(); globalThis.location.origin='http://test';
 const snapshot={version:1,entries:[{id:9001,mediaId:61},{id:9002,mediaId:62},{id:9003,mediaId:63},{id:9004,mediaId:64}],index:0,position:4,label:'FIFO',upNext:[9002,9003]};
 const values=new Map([['player.queue.http://test.15',JSON.stringify(snapshot)]]);
 globalThis.localStorage={getItem:k=>values.get(k)??null,setItem:(k,v)=>values.set(k,v),removeItem:k=>values.delete(k)};
 const original=API.mediaDetail; API.mediaDetail=async id=>({media:{id,type:'audio',duration:12}});
 try {
  await queue.restoreQueue(15); await queue.resumeQueue();
  assertEqual(queue.queueState().entries.filter(e=>e.upNext).map(e=>e.media.id).join(','),'62,63','Resume retains future FIFO block');
  await queue.insertQueue([{id:65,type:'audio',duration:12}],'next');
  assertEqual(queue.queueState().entries.map(e=>e.media.id).join(','),'61,62,63,65,64','Play next after recovered Resume appends to retained FIFO');
  await queue.jumpQueue(1);
  assertEqual(queue.queueState().entries.filter(e=>e.upNext).length,0,'deliberate row jump retains normal invalidation');
 } finally { API.mediaDetail=original; }
}

async function testPersistedQueueRecovery() {
  setup(); queue.initQueue();
  globalThis.location.origin = 'http://test';
  const values = new Map();
  globalThis.localStorage = { getItem: key => values.get(key) ?? null, setItem: (key,value) => values.set(key,value), removeItem: key => values.delete(key) };
  const key = 'player.queue.http://test.1', original = API.mediaDetail;
  const snapshot = {version:1, entries:[{id:701,mediaId:51},{id:702,mediaId:52},{id:703,mediaId:51}],index:2,position:7.25,label:'Repeated album',upNext:[702],repeat:'one',shuffle:true,canonical:[702,701,703]};
  API.mediaDetail = async id => ({media:{id,type:'audio',duration:12,file_name:`${id}.mp3`}});
  try {
    values.set(key, JSON.stringify(snapshot));
    const calls = audio().playCalls;
    await queue.restoreQueue(1);
    assertEqual(queue.queueState().entries.map(e=>e.id).join(','),'701,702,703','recovery preserves repeated occurrences');
    assertEqual(queue.queueState().index,2,'recovery restores later repeated occurrence');
    assertEqual(queue.queueState().repeat,'one','recovery restores scoped mode');
    assert(queue.queueState().shuffle,'recovery restores shuffle');
    assertEqual(JSON.parse(values.get(key)).canonical.join(),'702,701,703','recovery retains canonical order');
    assertEqual(queue.queueState().position,7.25,'short-song exact queue offset survives');
    assertEqual(audio().playCalls,calls,'recovery never calls play');
    player.selectMedia({id:51,type:'audio',duration:12,file_name:'51.mp3'},-1,queue.recoveredQueuePosition(51));
    assert(queue.queueState().entries[1].upNext,'paused route restoration preserves FIFO markers');
    assertEqual(queue.queueState().position,7.25,'paused route restoration keeps offset');
    queue.moveQueue(2,-1);
    assertEqual(queue.queueState().index,1,'restored reorder follows current occurrence');
    assertEqual(queue.queueState().position,7.25,'restored reorder retains offset');
    await queue.resumeQueue();
    audio().readyState=1; audio().dispatch('loadedmetadata');
    assertEqual(audio().currentTime,7.25,'explicit resume seeks short song');
    assertEqual(player.currentMediaId(),51,'explicit resume chooses current repeat');
    audio().currentTime=8.5; audio().dispatch('pause');
    assertEqual(JSON.parse(values.get(key)).position,8.5,'pause immediately persists exact queue offset');
    queue.clearQueue();
    assertEqual(JSON.parse(values.get(key)).entries.length,0,'clear empties durable queue');
    queue.undoQueue();
    assertEqual(JSON.parse(values.get(key)).entries.length,3,'Undo restores durable occurrence list');
    // Denied media gets dropped; duplicate selected identity still follows its occurrence.
    values.set(key,JSON.stringify(snapshot));
    API.mediaDetail = async id => { if(id===52) throw Object.assign(new Error('denied'),{status:403}); return {media:{id,type:'audio',duration:12}}; };
    await queue.restoreQueue(1);
    assertEqual(queue.queueState().index,1,'dropping earlier unavailable row retains selected repeat');
    assertEqual(JSON.parse(values.get(key)).canonical.join(),'701,703','unavailable canonical IDs removed too');
    assertEqual(queue.queueState().position,7.25,'unavailable earlier row keeps offset');
    assert(queue.queueState().status.includes('1 song is no longer available'),'quiet unavailable notice');
    // Network failure must never rewrite the snapshot as empty or deleted.
    values.set(key,JSON.stringify(snapshot));
    API.mediaDetail = async()=>{throw new Error('offline');};
    await queue.restoreQueue(1);
    assert(queue.queueState().restoreError,'offline has Retry');
    assertEqual(values.get(key),JSON.stringify(snapshot),'offline retains snapshot bytes');
    let finish;
    API.mediaDetail = () => new Promise(resolve=>{ finish=resolve; });
    const pending = queue.restoreQueue(2);
    await Promise.resolve();
    assertEqual(queue.queueState().entries.length,0,'scope switch hides old account rows immediately');
    assertEqual(queue.queueState().label,'','scope switch hides old source label');
    assert(!queue.queueState().shuffle,'new account clears shuffle');
    // No snapshot for account two; restore must not import account one.
    await pending;
    values.set('player.queue.http://test.2','bad JSON');
    await queue.restoreQueue(2);
    assert(queue.queueState().status.includes('unreadable'),'corrupt data has readable status');
    assertEqual(values.get('player.queue.http://test.2') && JSON.parse(values.get('player.queue.http://test.2')).entries.length,0,'corrupt snapshot replaced with empty valid state');
    values.set(key,JSON.stringify(snapshot));
    API.mediaDetail = () => new Promise(resolve=>{ finish=resolve; });
    const late=queue.restoreQueue(1); await Promise.resolve();
    await queue.playAlbumCollection([{id:99,type:'audio',duration:12}],0,'Replacement');
    finish({media:{id:51,type:'audio',duration:12}}); await late;
    assertEqual(queue.queueState().label,'Replacement','late recovery cannot replace user action');
    const realSet=localStorage.setItem;
    localStorage.setItem=()=>{throw new Error('quota');};
    await queue.insertQueue([{id:100,type:'audio'}],'append');
    assertEqual(queue.queueState().entries.length,2,'quota failure leaves queue usable');
    assert(queue.queueState().status.includes('could not be saved'),'quota failure visible');
    localStorage.setItem=realSet;
  } finally { API.mediaDetail=original; }
}

async function testCollectionReplaceUndoAndOccurrenceOrder() {
  setup();
  const songs = [501,502,501].map(id => ({id,type:'audio',duration:12,file_name:`${id}.mp3`}));
  await queue.playAlbumCollection([{id:500,type:'audio',duration:12}],0,'Original');
  const previous = queue.queueState().entries.map(entry=>entry.id);
  await queue.insertQueue(songs,'now',{replace:true,source:'Selected playlists'});
  assertEqual(queue.queueState().entries.map(entry=>entry.media.id).join(','),'501,502,501','replacement keeps occurrences and drops old remainder');
  assertEqual(queue.queueState().label,'Selected playlists','replacement label persists');
  assertEqual(queue.queueState().index,0,'replacement starts first occurrence');
  assertEqual(audio().src,'/api/media/501/stream','replacement assigns first original source');
  queue.undoQueue(queue.queueUndoToken());
  assertEqual(queue.queueState().entries.map(entry=>entry.id).join(','),previous.join(','),'one Undo restores original occurrence IDs');
  assertEqual(queue.queueState().label,'Original','Undo restores source label');
  assert(!queue.queueState().active,'replacement Undo stays paused');
}

async function testCollectionDuplicateSubmitAndSupersession() {
  setup();
  const {bindCollectionActions,queueCollectionSelected} = await import('../collection-actions.js');
  const {state} = await import('../state.js');
  const {beginPlaybackIntent} = await import('../playback-intent.js');
  const original=API.album;
  let complete, calls=0;
  API.album=()=>{calls++;return new Promise(resolve=>complete=resolve);};
  const play={}, menu={}, status={};
  const root={isConnected:true,querySelector:selector=>selector==='[data-collection-play]'?play:selector==='[data-collection-menu]'?menu:status,querySelectorAll:()=>[play,menu]};
  const owner=bindCollectionActions(root,()=>[{kind:'album',key:'a'}],()=>true,'Test album');
  owner.catalog=true;owner.kind='album';owner.selecting=false;
  // The shortcut path selects one catalog target while sharing the toolbar's busy owner.
  const originalQuery=document.querySelectorAll;
  const card={dataset:{collectionKey:'a'},classList:{contains:()=>true}};
  document.querySelectorAll=selector=>selector.includes('.media-card')?[card]:[];
  try {
    state.user={id:7};
    queueCollectionSelected('next'); queueCollectionSelected('next');
    assertEqual(calls,1,'repeated e while resolving sends only one collection request');
    complete({songs:[{id:503,type:'audio',duration:12}]});await settle();
    assertEqual(queue.queueState().entries.filter(entry=>entry.media.id===503).length,1,'deferred repeated e inserts once');
    play.onclick();
    beginPlaybackIntent();
    complete({songs:[{id:504,type:'audio',duration:12}]});await settle();
    assert(!queue.queueState().entries.some(entry=>entry.media.id===504),'new source intent rejects deferred collection Play');
    play.onclick(); state.user={id:8};
    complete({songs:[{id:505,type:'audio',duration:12}]});await settle();
    assert(!queue.queueState().entries.some(entry=>entry.media.id===505),'new account rejects deferred collection Play');
  } finally {API.album=original;document.querySelectorAll=originalQuery;}
}

async function testQueueRepeatModesAndEdits() {
  const song = id => ({id, type:'audio', file_name:`${id}.mp3`, duration:12});
  const mode = value => { for (let i=0; queue.queueState().repeat !== value && i<3; i++) queue.cycleQueueRepeat(); };
  setup(); mode('off');
  await queue.playAlbumCollection([song(601),song(602),song(601)],2);
  const occurrence = queue.queueState().entries[2].id;
  await queue.advanceQueue(1,{completed:true});
  assertEqual(queue.queueState().entries[queue.queueState().index].id,occurrence,'Off completion stops at last occurrence');
  mode('all'); await queue.advanceQueue(1,{completed:true});
  assertEqual(queue.queueState().index,0,'All completion wraps to first');
  await queue.advanceQueue(-1);
  assertEqual(queue.queueState().index,2,'All explicit Previous wraps');
  mode('one'); await queue.advanceQueue(1,{completed:true});
  assertEqual(queue.queueState().entries[queue.queueState().index].id,occurrence,'One completion retains later duplicate occurrence');
  await queue.advanceQueue(1);
  assertEqual(queue.queueState().index,2,'One explicit Next boundary does not wrap');
  await queue.advanceQueue(-1);
  assertEqual(queue.queueState().index,1,'One explicit Previous follows adjacency');
  await queue.insertQueue([song(603),song(604)],'next');
  await queue.advanceQueue(1,{completed:true});
  await queue.insertQueue([song(605)],'next');
  assertEqual(queue.queueState().entries.map(e=>e.media.id).join(), '601,602,603,604,605,601','One does not consume future next FIFO');
  queue.moveQueue(1,1);
  const moved = queue.queueState().entries[queue.queueState().index].id;
  await queue.advanceQueue(1,{completed:true});
  assertEqual(queue.queueState().entries[queue.queueState().index].id,moved,'One follows occurrence after reorder');
  queue.clearQueue(); queue.undoQueue();
  assertEqual(queue.queueState().repeat,'one','Clear and Undo never roll preference back');
  player.selectAndPlay(song(699),0); await queue.advanceQueue(1,{completed:true});
  assertEqual(player.currentMediaId(),699,'repeat cannot take over replacement Sets source');
  mode('off');
}

async function testRepeatCompletionEpochAndGenuineReplay() {
  let advances=0, release;
  setup({onNext:()=>advances++});
  const original=API.progress, status=API.progressStatus;
  API.progressStatus=async()=>{};
  try {
    player.selectAndPlay({id:611,type:'audio',duration:12},0);
    API.progress=()=>new Promise(done=>release=done);
    audio().currentTime=12; audio().ended=true;
    audio().dispatch('ended'); audio().dispatch('ended'); await settle();
    audio().dispatch('seeking'); audio().currentTime=2; audio().ended=false;
    release(); await settle();
    assertEqual(advances,0,'seek while final save waits cancels old completion');
    API.progress=async()=>{};
    audio().dispatch('play'); audio().currentTime=12; audio().ended=true; audio().dispatch('ended'); await settle();
    assertEqual(advances,1,'genuine replay same source can complete');
    audio().dispatch('play'); audio().dispatch('ended'); audio().dispatch('ended'); await settle();
    assertEqual(advances,2,'later replay completes once despite duplicate event');
    API.progress=()=>new Promise(done=>release=done);
    audio().dispatch('play'); audio().dispatch('ended'); await settle();
    player.selectAndPlay({id:611,type:'audio',duration:12},0); // same media, new occurrence/source
    audio().currentTime=12; audio().ended=true;
    release(); await settle();
    assertEqual(advances,2,'same-media replacement rejects stale deferred completion');
  } finally { API.progress=original; API.progressStatus=status; }
}

async function testSenderNaturalCompletionRetainsRepeatOneOccurrence() {
  const cast=await import('../cast.js'), ticket=API.castTicket;
  const remoteListeners={}, contextListeners={};
  const remote={isPaused:false,isMediaLoaded:true,currentTime:0,playerState:'playing'};
  let session={loadMedia:async()=>{}};
  const context={setOptions(){},getCastState:()=> 'none',getCurrentSession:()=>session,
    addEventListener:(type,fn)=>contextListeners[type]=fn,
    endCurrentSession:()=>{session=null;contextListeners.session({sessionState:'ended'});}};
  function Value() {}
  window.chrome={cast:{AutoJoinPolicy:{},media:{DEFAULT_MEDIA_RECEIVER_APP_ID:'default',StreamType:{BUFFERED:'buffered'},PlayerState:{IDLE:'idle'},MediaInfo:Value,LoadRequest:Value,GenericMediaMetadata:Value,MusicTrackMediaMetadata:Value}}};
  window.isSecureContext=true;
  window.cast={framework:{CastContext:{getInstance:()=>context},CastContextEventType:{CAST_STATE_CHANGED:'cast',SESSION_STATE_CHANGED:'session'},CastState:{NO_DEVICES_AVAILABLE:'none'},SessionState:{SESSION_STARTED:'started',SESSION_ENDED:'ended'},RemotePlayer:function(){return remote;},RemotePlayerController:function(){return {addEventListener:(type,fn)=>remoteListeners[type]=fn};},RemotePlayerEventType:{CURRENT_TIME_CHANGED:'time',IS_PAUSED_CHANGED:'pause',PLAYER_STATE_CHANGED:'state'}}};
  try {
    setup({cast:true,onNext:options=>queue.advanceQueue(1,options)});
    window.__onGCastApiAvailable(true);
    API.castTicket=async()=>({ticket:'local-test',stream_content_type:'audio/mpeg'});
    await queue.playAlbumCollection([{id:621,type:'audio',duration:12},{id:621,type:'audio',duration:12},{id:622,type:'audio',duration:12}],1);
    while(queue.queueState().repeat!=='one') queue.cycleQueueRepeat();
    const occurrence=queue.queueState().entries[1].id;
    await cast.castItem({id:621,type:'audio',duration:12},0);
    remote.currentTime=12;remoteListeners.time();remote.isMediaLoaded=false;remote.playerState='idle';remoteListeners.state();await settle();
    assertEqual(queue.queueState().index,1,'production sender natural callback retains One index');
    assertEqual(queue.queueState().entries[queue.queueState().index].id,occurrence,'production sender retains exact later occurrence');
    player.stopAndClose();await settle();
  } finally {API.castTicket=ticket;delete window.chrome;delete window.cast;delete window.isSecureContext;}
}

async function testPausedReattachHintPreservesIntentAndPreparation() {
  const callbacks={},oldListener=window.addEventListener,oldOpen=window.open;
  const popup={closed:false,postMessage(){},close(){this.closed=true;}};
  window.addEventListener=(type,fn)=>{(callbacks[type] ||= []).push(fn);};
  window.open=()=>popup;
  const close=(media,position=7)=>{
    for(const fn of callbacks.message||[])fn({origin:'http://test',source:popup,data:{type:'detach-closing',state:{media,currentTime:position,playing:false,index:0}}});
  };
  try{
    setup();player.selectAndPlay({id:720,type:'audio',file_name:'a.mp3'},0);player.toggleDetach();
    const b={id:721,type:'audio',file_name:'b.mp3'};close(b);
    assertEqual(audio().paused,true,'paused popup return never autoplays');
    audio().readyState=1;audio().dispatch('loadedmetadata');
    assertEqual(audio().currentTime,7,'paused return retains current source position');
    assertEqual(statusText(),'Playback returned here. Press Play to resume.','paused return explains visible Play precondition');
    assertEqual(elements['btn-play'].textContent,'▶','existing Play is the primary action');
    player.requestPlay();audio().dispatch('playing');
    assertEqual(statusText(),'','actual playback clears return hint');
    player.toggleDetach();close(b);player.selectMedia({id:722,type:'audio',file_name:'c.mp3'},0);
    assertEqual(statusText(),'','new source clears old return hint');
    player.toggleDetach();let release;answer=()=>new Promise(done=>release=done);close(wmaAudio);
    assert(statusText().startsWith('Preparing '),'return hint never replaces compat preparation');
    release(response(200));await settle();
    assertEqual(audio().paused,true,'prepared paused return remains paused');
    assertEqual(statusText(),'Playback returned here. Press Play to resume.','hint appears only after source ready');
    audio().error={code:2};audio().dispatch('error');
    assertEqual(statusText(),'','runtime error clears return hint instead of masking Retry feedback');
    player.stopAndClose();
  }finally{window.addEventListener=oldListener;window.open=oldOpen;}
}

async function testRegisteredSystemPauseCancelsPreparingPlayIntent() {
  setup();let release;
  answer=()=>new Promise(done=>release=done);
  player.selectMedia(wmaAudio,0,4);
  osActions.play({});osActions.pause({});
  assertEqual(player.hasPendingPlay(),false,'OS Pause cancels preparing autoplay intent');
  release(response(200));await settle();
  assertEqual(audio().playCalls,0,'resolved preparation cannot play after OS Pause');
  player.stopAndClose();
}
async function testRegisteredSystemPlayRetriesErrorsIdempotently() {
  setup();script(response(500),response(200));
  player.selectAndPlay(wmaAudio,0,4);await settle();
  assertEqual(audio().getAttribute('src'),null,'OS retry fixture first fails preparation');
  osActions.play({});await settle();
  assertEqual(requests.length,2,'OS Play reprobes failed preparation');
  assertEqual(audio().src,'/api/media/3/compat','OS Play restores prepared source');
  assertEqual(audio().playCalls,1,'OS Play starts exactly once after successful retry');
  const plays=audio().playCalls;osActions.play({});
  assertEqual(audio().playCalls,plays,'OS Play already-playing is idempotent');
  audio().currentTime=7;audio().error={code:2};audio().dispatch('error');
  const loads=audio().loads;
  osActions.play({});await settle();
  assertEqual(audio().loads,loads+2,'OS Play clears failed compat source then attaches successfully probed source');
  assertEqual(audio().error,null,'OS Play clears runtime failure by reloading source');
  player.stopAndClose();
}
async function testRegisteredSystemMetadataReturnsAfterCastAndFailedStart() {
  const cast=await import('../cast.js'), listeners={};let failed=false;
  let session={loadMedia:async()=>{if(failed)throw Error('receiver load failed');},getCastDevice:()=>({friendlyName:'Owned fake receiver'})};
  const context={setOptions(){},getCastState:()=> 'none',getCurrentSession:()=>session,
    addEventListener:(type,fn)=>listeners[type]=fn,endCurrentSession:()=>{session=null;listeners.session({sessionState:'ended'});}};
  function Value(){}
  window.chrome={cast:{AutoJoinPolicy:{},Image:Value,media:{DEFAULT_MEDIA_RECEIVER_APP_ID:'default',StreamType:{BUFFERED:'buffered'},PlayerState:{IDLE:'idle'},MediaInfo:Value,LoadRequest:Value,GenericMediaMetadata:Value,MusicTrackMediaMetadata:Value}}};
  window.isSecureContext=true;
  window.cast={framework:{CastContext:{getInstance:()=>context},CastContextEventType:{CAST_STATE_CHANGED:'cast',SESSION_STATE_CHANGED:'session'},CastState:{NO_DEVICES_AVAILABLE:'none'},SessionState:{SESSION_STARTED:'started',SESSION_ENDED:'ended'},RemotePlayer:function(){return {isPaused:true};},RemotePlayerController:function(){return {addEventListener(){}};},RemotePlayerEventType:{}}};
  const ticket=API.castTicket;
  try {
    setup({cast:true});window.__onGCastApiAvailable(true);
    API.castTicket=async()=>({ticket:'owned',stream_content_type:'audio/mpeg'});
    answer=async()=>({ok:true,blob:async()=>new Blob(['art'],{type:'image/png'})});
    const media={id:711,type:'audio',file_name:'file.mp3',thumbnail_path:'art',music:{title:'Actual song',artist:'Actual artist',album:'Actual album'}};
    player.selectAndPlay(media,0);await settle();
    assertEqual(os.metadata?.title,'Actual song','local music initially published');
    listeners.session({sessionState:'started'});await settle();
    assertEqual(os.metadata,null,'Cast takeover clears local OS session');
    cast.stopCasting();await settle();
    assertEqual(os.metadata?.title,'Actual song','Cast handback republishes local title');
    assertEqual(os.metadata?.artist,'Actual artist','Cast handback republishes actual artist');
    assertEqual(os.metadata?.artwork?.length,1,'Cast handback republishes owned artwork');
    assert(typeof osActions.play==='function'&&typeof osActions.nexttrack==='function','Cast handback restores OS actions');
    osActions.play?.({});assertEqual(audio().paused,false,'returned OS Play controls actual local element');
    session={loadMedia:async()=>{throw Error('failed start');}};failed=true;
    listeners.session({sessionState:'started'});await settle();
    assertEqual(os.metadata?.title,'Actual song','failed Cast start rolls back to local OS metadata');
    assert(typeof osActions.play==='function','failed Cast start restores system Play');
    player.stopAndClose();
  } finally {API.castTicket=ticket;delete window.chrome;delete window.cast;delete window.isSecureContext;}
}

async function testRichIdentityClearsArtworkAcrossMediaKinds() {
  setup();
  elements['player-music-identity'] = fakeElement('player-music-identity');
  player.selectMedia({id:701,type:'audio',file_name:'01.mp3',thumbnail_path:'cover',music:{title:'Tagged title',artist:'Real artist',album:'Real album'}},0);
  assertEqual(elements['player-music-identity'].hidden,false,'full music identity visible');
  assertEqual(elements['player-restore-title'].textContent,'Restore: Tagged title · Real artist · Real album','mini title includes actual tags');
  const art=elements['btn-restore-player'].querySelector('.player-mini-art');
  assertEqual(art.hidden,false,'mini uses artwork');
  player.toggleMinimize();
  assertEqual(elements['player-restore-title'].textContent,'Restore: Tagged title · Real artist · Real album','minimize retains metadata');
  player.selectMedia({id:702,type:'video',file_name:'film.mp4'},0);
  assertEqual(art.hidden,true,'video clears old music mini art');
  assertEqual(elements['player-music-identity'].hidden,true,'video hides music text');
  player.selectMedia({id:703,type:'image',file_name:'cover.jpg'},0);
  assertEqual(art.hidden,true,'image clears old mini art');
  player.stopAndClose();assertEqual(art.hidden,true,'stop leaves no artwork');
}

async function testActualPopupFinishesBeforeAdvanceAndRejectsStaleCycle() {
  setup();
  const listeners={}, messages=[], writes=[];
  const oldListener=window.addEventListener, original=API.progress, status=API.progressStatus;
  window.addEventListener=(type,fn)=>{(listeners[type] ||= []).push(fn);};
  window.location.origin='http://test';
  window.opener={closed:false,postMessage:message=>messages.push(message)};
  let release;
  API.progress=async(id,position)=>{writes.push(`position:${id}:${position}`);};
  API.progressStatus=async(id,value)=>{writes.push(`${value}:${id}`);};
  try {
    await import('../detachPopup.js');
    const load=id=>{for(const fn of listeners.message||[]) fn({origin:'http://test',data:{type:'detach-load',media:{id,type:'audio',duration:1301},index:1,streamUrl:`/api/media/${id}/stream`,play:false}});audio().readyState=1;};
    load(631);audio().currentTime=1301;audio().ended=true;
    audio().dispatch('ended');audio().dispatch('ended');await settle();
    assertEqual(writes.join(','),'position:631:1301,finished:631','actual popup finalizes long-source progress in order exactly once');
    assertEqual(messages.filter(m=>m.type==='detach-next'&&m.completed).length,1,'actual popup advances once after final save');
    writes.length=0;messages.length=0;
    load(631);audio().currentTime=1301;audio().ended=true;
    API.progress=async(id,position)=>{writes.push(`position:${id}:${position}`);await new Promise(done=>release=done);};
    audio().dispatch('ended');await settle();load(631); // same media, newer occurrence
    release?.();await settle();
    assertEqual(messages.filter(m=>m.type==='detach-next').length,0,'new same-ID popup load rejects deferred old completion');
  } finally {API.progress=original;API.progressStatus=status;window.addEventListener=oldListener;delete window.opener;}
}

console.info('Running playback source tests...');
async function testCanceledCollectionSelectionReleasesOnlyItsOwnBusyState() {
  setup();
  const {bindCollectionCatalog,cancelCollectionSelection}=await import('../collection-actions.js');
  const {state}=await import('../state.js');state.user={id:44};
  const oldCreate=document.createElement,oldAlbum=API.album;
  const controls=new Map();
  for(const key of ['data-select-collections','data-collection-play','data-collection-shuffle','data-collection-menu','data-collection-status','data-batch','data-batch-count']) controls.set(`[${key}]`,{focus(){},isConnected:true});
  const toolbar={isConnected:true,querySelector:s=>controls.get(s),querySelectorAll:()=>['data-collection-play','data-collection-shuffle','data-collection-menu'].map(k=>controls.get(`[${k}]`)),append(){}};
  let check,created=0;
  const card=fakeElement();card.dataset={};card.prepend=value=>{check=value;};card.querySelector=s=>s==='input'?check:{append(){}};
  const grid={querySelector:()=>({append(){}}),querySelectorAll:()=>[card]};
  document.createElement=()=>{if(created++===0)return toolbar;const el=fakeElement();el.dataset={};return el;};
  const completions=[];API.album=()=>new Promise(resolve=>completions.push(resolve));
  try{
    const owner=bindCollectionCatalog(grid,[{album_key:'a',title:'Album'}],'album','album_key','title',()=>true);
    const toggle=controls.get('[data-select-collections]'),play=controls.get('[data-collection-play]');
    toggle.onclick();check.checked=true;check.onchange();
    const canceled=play.onclick();assertEqual(completions.length,1,'selection starts actual resolver');
    assert(cancelCollectionSelection(),'actual selection Cancel handled');
    toggle.onclick();check.checked=true;check.onchange();
    const newer=play.onclick();assertEqual(completions.length,2,'new explicit selection action works immediately after Cancel');
    completions[0]({songs:[{id:1181,type:'audio',duration:12}]});await canceled;
    if(completions[1]) {
      assert(owner.busy && play.disabled,'late canceled selection cannot unlock newer operation');
      completions[1]({songs:[{id:1182,type:'audio',duration:12}]});await newer;
      assertEqual(queue.queueState().entries[0].media.id,1182,'only new selection mutates queue');
    }
  } finally { document.createElement=oldCreate;API.album=oldAlbum; }
}

async function testShuffleOptionsEscapeRestoresOpenerAndCancelsPending() {
  setup();
  const {bindCollectionActions} = await import('../collection-actions.js');
  const {initKeyboard} = await import('../keyboard.js');
  const {state} = await import('../state.js');
  const oldCreate=document.createElement, oldBody=document.body, oldForm=globalThis.FormData;
  const oldAlbum=API.album, oldKeys=documentListeners.keydown;
  const focusable = () => ({isConnected:true,focus(){if (!this.disabled) document.activeElement=this;}});
  const opener=focusable(), card=focusable(), input={...focusable(),tagName:'INPUT',blur(){document.activeElement=null;}};
  const play={}, shuffle={}, status={}, submit={}, cancel={};
  const modal=fakeElement('shuffle-options-modal'), form={querySelector:()=>submit};
  modal.firstElementChild={};
  modal.querySelector=selector=>selector==='form'?form:status;
  modal.querySelectorAll=()=>[cancel];
  elements['shuffle-options-modal']=modal;
  let popover;
  document.body={append(value){popover=value;}};
  document.createElement=()=>{
    const el=fakeElement();
    const option={dataset:{action:'options'},focus(){}};
    el.querySelectorAll=()=>[option];el.querySelector=()=>option;
    el.showPopover=()=>{};el.hidePopover=()=>{};
    return el;
  };
  globalThis.FormData=class { *[Symbol.iterator](){yield ['genre',''];yield ['maximum',''];} };
  const root={isConnected:true,querySelector:selector=>selector==='[data-collection-play]'?play:selector==='[data-collection-shuffle]'?shuffle:selector==='[data-collection-menu]'?opener:status,querySelectorAll:()=>[play,shuffle,opener]};
  try {
    state.user={id:41};
    await queue.playAlbumCollection([{id:1199,type:'audio',duration:12}],0);
    const before=queue.queueState().entries.map(e=>e.id).join();
    let complete;
    API.album=()=>new Promise(resolve=>complete=resolve);
    bindCollectionActions(root,()=>[{kind:'album',key:'test'}],()=>true,'Escape');
    opener.focus();opener.onclick({currentTarget:opener});popover.querySelector().onclick();
    const pending=form.onsubmit({preventDefault(){}});
    await Promise.resolve();
    assertEqual(status.textContent,'Loading songs…','actual options form starts delayed collection resolution');
    input.focus();
    initKeyboard({escape:()=>{modal.classList.remove('open');card.focus();}});
    const event={key:'Escape',target:input,preventDefault(){this.prevented=true;},stopPropagation(){this.stopped=true;},type:'keydown'};
    modal.onkeydown?.(event);
    if(!event.stopped) document.dispatchEvent(event);
    assert(!modal.classList.contains('open'),'Escape closes shuffle options');
    assert(document.activeElement===opener,'Escape must focus original Queue actions opener rather than selected song');
    assert(!opener.disabled,'pending Escape re-enables actual opener');
    const canceledComplete=complete;
    const newer=shuffle.onclick();
    assert(shuffle.disabled,'newer collection transaction owns busy controls');
    canceledComplete({songs:[{id:1198,type:'audio',duration:12}]});await pending;
    assertEqual(queue.queueState().entries.map(e=>e.id).join(),before,'Escape cancels delayed option resolution before queue replacement');
    assert(shuffle.disabled,'late canceled finally cannot unlock newer controls');
    assertEqual(status.textContent,'Loading songs…','late canceled finally cannot erase newer status');
    complete({songs:[{id:1197,type:'audio',duration:12}]});await newer;
    assertEqual(queue.queueState().entries[0].media.id,1197,'new explicit action remains usable after pending cancel');

  } finally {
    document.createElement=oldCreate;document.body=oldBody;globalThis.FormData=oldForm;
    API.album=oldAlbum;documentListeners.keydown=oldKeys||[];
  }
}

async function testPlaybackShufflePreservesOccurrenceAndCanonicalIntent() {
  setup();
  const queue = await import('../queue.js');
  const song = id => ({id,type:'audio',duration:12});
  await queue.playAlbumCollection([song(1101),song(1102),song(1101),song(1103),song(1104)],1,'Shuffle');
  const original = queue.queueState(), current = original.entries[1].id;
  audio().currentTime=4;
  const loads=audio().loads;
  queue.shuffleQueue();
  let q=queue.queueState();
  assert(q.shuffle,'toggle selects playback shuffle');
  assertEqual(q.entries[q.index].id,current,'shuffle preserves selected occurrence');
  assertEqual(audio().currentTime,4,'shuffle preserves actual source position');
  assertEqual(audio().loads,loads,'shuffle never reloads source');
  assertEqual(q.entries.map(e=>e.id).sort((a,b)=>a-b).join(),original.entries.map(e=>e.id).join(),'no occurrence missing or duplicated');
  queue.shuffleQueue({reshuffle:true}); queue.undoQueue();
  assertEqual(queue.queueState().entries.map(e=>e.id).join(),q.entries.map(e=>e.id).join(),'Undo reshuffle restores exact concrete permutation');
  queue.shuffleQueue();
  assertEqual(queue.queueState().entries.map(e=>e.id).join(),original.entries.map(e=>e.id).join(),'Off restores canonical remainder');
  queue.shuffleQueue();
  await queue.insertQueue([song(1105),song(1106)],'next');
  const fifo=queue.queueState().entries.slice(2,4).map(e=>e.id).join();
  queue.shuffleQueue({reshuffle:true});
  assertEqual(queue.queueState().entries.slice(2,4).map(e=>e.id).join(),fifo,'pinned Play next FIFO is not shuffled');
  await queue.insertQueue([song(1107)],'append');
  queue.shuffleQueue();
  assertEqual(queue.queueState().entries.at(-1).media.id,1107,'append stays at canonical tail');
  assertEqual(queue.queueState().entries.slice(2,4).map(e=>e.id).join(),fifo,'Off retains pinned FIFO');
  await queue.advanceQueue(1); // Consume one explicit next occurrence.
  const before=queue.queueState();
  queue.shuffleQueue(); queue.shuffleQueue();
  assertEqual(queue.queueState().entries.slice(0,before.index+1).map(e=>e.id).join(),before.entries.slice(0,before.index+1).map(e=>e.id).join(),'Off after advance never moves consumed prefix');
  queue.removeQueue(queue.queueState().entries.length-1);
  queue.undoQueue();
  assertEqual(queue.queueState().entries.length,before.entries.length,'remove Undo retains occurrences/canonical');
  await queue.insertQueue([song(1110),song(1110),song(1111)],'now',{replace:true,shuffled:true});
  q=queue.queueState();
  assert(q.shuffle,'collection replacement enables shuffle');
  assertEqual(q.index,0,'collection starts first shuffled occurrence');
  assertEqual(q.entries.map(e=>e.media.id).sort().join(),'1110,1110,1111','collection shuffle preserves repeated media');
  queue.shuffleQueue();
  const selected=q.entries[0].id;
  assertEqual(queue.queueState().entries[0].id,selected,'Off never changes current shuffled collection item');
  queue.undoQueue(); assert(queue.queueState().shuffle,'Undo toggle restores selected shuffle state');
  queue.moveQueue(1,1);const moved=queue.queueState().entries.map(e=>e.id).join();
  queue.shuffleQueue();assertEqual(queue.queueState().entries.map(e=>e.id).join(),moved,'manual move establishes canonical order');
  await queue.playAlbumCollection([song(1112)],0);
  assert(!queue.queueState().shuffle,'normal Play restores ordered mode');
  queue.shuffleQueue(); assert(!queue.queueState().shuffle,'less than two upcoming cannot enable');
  queue.clearQueue();
}

for (const test of [
  testCanceledCollectionSelectionReleasesOnlyItsOwnBusyState,
  testShuffleOptionsEscapeRestoresOpenerAndCancelsPending,
  testPlaybackShufflePreservesOccurrenceAndCanonicalIntent,
  testSenderNaturalCompletionRetainsRepeatOneOccurrence,
  testQueueRepeatModesAndEdits,
  testRepeatCompletionEpochAndGenuineReplay,
  testCollectionReplaceUndoAndOccurrenceOrder,
  testCollectionDuplicateSubmitAndSupersession,
  testRecoveredResumeRetainsFutureFIFO,
  testPersistedQueueRecovery,
  testUndoCannotResurrectInvalidatedFIFO,
  testSupersededPlayNowDoesNotConfirmOrCaptureNewUndo,
  testExplicitInsertionFIFOAndUndo,
  testLatestQueueIntentCancelsPendingSetsResume,
  testEditableQueueIdentityOwnershipAndProgress,
  testInitialLongAlbumPlayAndPendingReplacement,
  testRapidNextDuringLongAlbumResumeKeepsCollection,
  testAlbumCollectionSurvivesBrowseButNotRestoredSource,
  testLocalFlushDoesNotOverwriteRemoteOwners,
  testInactiveOrStaleEndedEventCannotFinishCurrentAudio,
  testCompletionPrecedesAdvanceWithoutReopeningFinished,
  testDelayedCompletionCannotAdvanceAReplacement,
  testPlainStreamIsAssignedDirectly,
  testCompatStreamWaitsUntilReady,
  testBusyStatusIsShown,
  testTerminalCompatErrorShowsToast,
  testTogglePlayRetriesFailedCompatStream,
  testTogglePlayRetriesAfterElementError,
  testExpiredSessionRedirectsLibraryPlaybackOnly,
  testSwitchWhilePreparingCancels,
  testSwitchDuringRetryWaitStopsRetries,
  testElementErrorShowsToast,
  testIdleElementErrorIsIgnored,
  testDirectLoadProbesShareCompatUrl,
  testPausedReattachHintPreservesIntentAndPreparation,
  testRegisteredSystemPauseCancelsPreparingPlayIntent,
  testRegisteredSystemPlayRetriesErrorsIdempotently,
  testRegisteredSystemMetadataReturnsAfterCastAndFailedStart,
  testRichIdentityClearsArtworkAcrossMediaKinds,
  testActualPopupFinishesBeforeAdvanceAndRejectsStaleCycle,
]) {
  try {
    await test();
  } catch (err) {
    failures.push(`${test.name} threw: ${err?.stack || err}`);
  }
}

// A pending Sets search must not replace an explicit Music navigation.
try {
  const { initSearch } = await import('../search.js');
  const { openAlbums, openAlbum } = await import('../views/music-catalog.js');
  const { state } = await import('../state.js');
  const input = fakeElement('search-input');
  let searches = 0;
  initSearch({input, onChange: () => { searches++; state.music = null; }});
  input.value = 'pending'; input.dispatch('input');
  openAlbums();
  await settle();
  assert(state.music?.album === '', 'pending search must not replace Albums');
  input.dispatch('input'); openAlbum('al_test');
  await settle();
  assertEqual(state.music?.album, 'al_test', 'pending search must not replace album detail');
  input.value = 'intentional'; input.dispatch('input');
  await settle();
  assertEqual(searches, 1, 'new intentional search still runs');
  assertEqual(state.music, null, 'intentional search returns to Sets results');
} catch (err) { failures.push(`Music search navigation threw: ${err?.stack || err}`); }

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.info('All playback source tests passed.');
  process.exit(0);
}
