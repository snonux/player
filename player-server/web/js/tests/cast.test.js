// Unit tests for cast.js: when the Cast SDK is used, the URLs and metadata a
// receiver is given, and a whole cast session against a fake Cast SDK (the
// real one only exists in Chrome): ticket request, LOAD from the local
// position, remote controls, progress saving and the hand-back.

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

// --- fakes ---------------------------------------------------------------

const requests = [];
globalThis.fetch = async (url, init = {}) => {
  requests.push({ url, method: init.method || 'GET', body: init.body });
  const json = url.endsWith('/cast')
    ? { ticket: 'T/1', stream_content_type: 'audio/flac', compat_content_type: 'audio/mp4' }
    : {};
  return { ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => json };
};

function fakeButton() {
  const classes = new Set(['hidden']);
  const attrs = {};
  const listeners = {};
  return {
    classList: {
      toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)),
      contains: (c) => classes.has(c),
    },
    setAttribute: (k, v) => { attrs[k] = v; },
    getAttribute: (k) => attrs[k],
    addEventListener: (type, fn) => { listeners[type] = fn; },
    click: () => listeners.click?.(),
  };
}

// A minimal stand-in for cast.framework and chrome.cast.
function fakeSdk() {
  const contextListeners = {};
  const playerListeners = {};
  const loads = [];
  let session = null;
  const remotePlayer = { currentTime: 0, duration: 0, isPaused: false, isMediaLoaded: true, playerState: 'PLAYING' };
  const controller = { toggles: 0, seeks: [] };
  const sdk = {
    loads,
    remotePlayer,
    controller,
    ended: 0,
    latePauses: 0,
    deferredLoad: null,
    startSession() {
      session = {
        getCastDevice: () => ({ friendlyName: 'Den TV' }),
        loadMedia: async (request) => { loads.push(request); await sdk.deferredLoad; },
        getMediaSession: () => ({ pause: (_request, done) => { sdk.latePauses++; done(); } }),
      };
      contextListeners.SESSION_STATE_CHANGED?.({ sessionState: 'SESSION_STARTED' });
    },
    endSession() {
      session = null;
      contextListeners.SESSION_STATE_CHANGED?.({ sessionState: 'SESSION_ENDED' });
    },
    castState(state) {
      contextListeners.CAST_STATE_CHANGED?.({ castState: state });
    },
    emit(type) { playerListeners[type]?.(); },
  };
  const context = {
    setOptions: (o) => { sdk.options = o; },
    getCastState: () => 'NO_DEVICES_AVAILABLE',
    getCurrentSession: () => session,
    requestSession: async () => { sdk.requested = true; },
    endCurrentSession: () => sdk.endSession(),
    addEventListener: (type, fn) => { contextListeners[type] = fn; },
  };
  const framework = {
    CastContext: { getInstance: () => context },
    CastContextEventType: { CAST_STATE_CHANGED: 'CAST_STATE_CHANGED', SESSION_STATE_CHANGED: 'SESSION_STATE_CHANGED' },
    CastState: { NO_DEVICES_AVAILABLE: 'NO_DEVICES_AVAILABLE', NOT_CONNECTED: 'NOT_CONNECTED', CONNECTED: 'CONNECTED' },
    SessionState: { SESSION_STARTED: 'SESSION_STARTED', SESSION_ENDED: 'SESSION_ENDED' },
    RemotePlayer: function RemotePlayer() { return remotePlayer; },
    RemotePlayerController: function RemotePlayerController() {
      return {
        addEventListener: (type, fn) => { playerListeners[type] = fn; },
        playOrPause: () => { controller.toggles++; },
        seek: () => { controller.seeks.push(remotePlayer.currentTime); },
      };
    },
    RemotePlayerEventType: {
      CURRENT_TIME_CHANGED: 'CURRENT_TIME_CHANGED',
      IS_PAUSED_CHANGED: 'IS_PAUSED_CHANGED',
      PLAYER_STATE_CHANGED: 'PLAYER_STATE_CHANGED',
    },
  };
  function MediaInfo(url, contentType) { this.contentId = url; this.contentType = contentType; }
  function LoadRequest(media) { this.media = media; }
  function Metadata(kind) { return function () { this.kind = kind; }; }
  const chromeCast = {
    AutoJoinPolicy: { ORIGIN_SCOPED: 'origin_scoped' },
    Image: function Image(url) { this.url = url; },
    media: {
      DEFAULT_MEDIA_RECEIVER_APP_ID: 'CC1AD845',
      MediaInfo,
      LoadRequest,
      PauseRequest: function PauseRequest() {},
      StreamType: { BUFFERED: 'BUFFERED' },
      GenericMediaMetadata: Metadata('generic'),
      MusicTrackMediaMetadata: Metadata('music'),
      PlayerState: { IDLE: 'IDLE' },
    },
  };
  sdk.framework = framework;
  sdk.chromeCast = chromeCast;
  return sdk;
}

const sdk = fakeSdk();
const appended = [];
globalThis.window = {
  chrome: { cast: sdk.chromeCast },
  cast: { framework: sdk.framework },
  isSecureContext: true,
  location: { origin: 'https://player.test', pathname: '/index.html', href: '' },
};
globalThis.location = window.location;
const doc = { createElement: () => ({}), head: { appendChild: (el) => appended.push(el) } };

const cast = await import('../cast.js');

const tick = () => new Promise((r) => setTimeout(r, 0));

// --- tests ---------------------------------------------------------------

function testSupport() {
  assert(cast.castSupported({ chrome: {}, isSecureContext: true }), 'Chrome on HTTPS');
  assert(!cast.castSupported({ chrome: {}, isSecureContext: false }), 'not on plain HTTP');
  assert(!cast.castSupported({ isSecureContext: true }), 'not outside Chromium');
  assertEqual(cast.initCast({ button: fakeButton() }, { isSecureContext: true }, doc), false, 'no SDK load elsewhere');
}

function testLoadFields() {
  const ticket = { ticket: 'a/b', stream_content_type: 'video/x-matroska', compat_content_type: 'video/mp4' };
  const original = cast.castLoadFields({ id: 1, type: 'video', file_name: 'film.mkv', thumbnail_path: 't' }, ticket, 'https://h');
  assertEqual(original.url, 'https://h/cast/a%2Fb/stream', 'original stream URL, ticket escaped');
  assertEqual(original.contentType, 'video/x-matroska', 'original type');
  assertEqual(original.imageUrl, 'https://h/cast/a%2Fb/thumbnail', 'thumbnail URL');
  assert(original.isVideo, 'video flag');
  const compat = cast.castLoadFields({ id: 1, type: 'audio', transcoded: true }, ticket, 'https://h');
  assertEqual(compat.url, 'https://h/cast/a%2Fb/compat', 'compat URL for transcoded items');
  assertEqual(compat.contentType, 'video/mp4', 'compat type');
  assertEqual(compat.imageUrl, '', 'no image without a thumbnail');
  const tagged = cast.castLoadFields({id:7,type:'audio',file_name:'song.flac',music:{title:'東京 Tagged Title'}},ticket,'https://h');
  assertEqual(tagged.title,'東京 Tagged Title','embedded music title');
  assertEqual(tagged.url,'https://h/cast/a%2Fb/stream','title leaves source URL unchanged');
}

async function testSession() {
  const button = fakeButton();
  const events = [];
  let paused = false;
  const media = { id: 7, type: 'audio', file_name: 'song.flac' };
  assert(cast.initCast({
    button,
    getCurrentMedia: () => media,
    localPosition: () => 33,
    pauseLocal: () => { paused = true; },
    onRemoteTime: (t, d) => events.push(['time', t, d]),
    onRemotePlaying: (p) => events.push(['playing', p]),
    onRemoteEnded: () => events.push(['ended']),
    onCastStarted: (name) => events.push(['started', name]),
    onCastEnded: (id, t) => events.push(['handback', id, t]),
    onError: (m) => events.push(['error', m]),
  }, window, doc), 'initCast loads the SDK');
  assertEqual(appended[0]?.src, cast.SDK_URL, 'SDK script added');
  window.__onGCastApiAvailable(true);
  assertEqual(sdk.options.receiverApplicationId, 'CC1AD845', 'Default Media Receiver');

  assert(button.classList.contains('hidden'), 'hidden without devices');
  sdk.castState('NOT_CONNECTED');
  assert(!button.classList.contains('hidden'), 'shown once a device is available');
  button.click();
  assert(sdk.requested, 'button opens the device picker');

  sdk.startSession();
  await tick(); await tick();
  assert(paused, 'local playback paused');
  const ticketRequest = requests.find((r) => r.url === '/api/media/7/cast');
  assertEqual(ticketRequest?.method, 'POST', 'ticket requested');
  const load = sdk.loads[0];
  assertEqual(load?.media.contentId, 'https://player.test/cast/T%2F1/stream', 'receiver URL');
  assertEqual(load?.media.contentType, 'audio/flac', 'receiver content type');
  assertEqual(load?.media.metadata.title, 'song', 'filename-stem fallback');
  assertEqual(load?.currentTime, 33, 'starts at the local position');
  assert(load?.autoplay, 'autoplay');
  assert(cast.isCasting(), 'casting');
  assertEqual(JSON.stringify(events[0]), JSON.stringify(['started', 'Den TV']), 'started event');

  sdk.remotePlayer.currentTime = 50;
  sdk.remotePlayer.duration = 200;
  sdk.emit('CURRENT_TIME_CHANGED');
  assertEqual(JSON.stringify(events.at(-1)), JSON.stringify(['time', 50, 200]), 'remote time shown');

  cast.castTogglePlay();
  assertEqual(sdk.controller.toggles, 1, 'play/pause goes to the receiver');
  cast.castSeek(500);
  assertEqual(sdk.controller.seeks.at(-1), 200, 'seek clamped to the duration');
  cast.castSeek(-3);
  assertEqual(sdk.controller.seeks.at(-1), 0, 'seek clamped to zero');

  sdk.remotePlayer.currentTime = 60;
  sdk.emit('CURRENT_TIME_CHANGED');
  // Expiry pauses an in-flight LOAD, unless a newer explicit Play supersedes it.
  for (const manualPlay of [false, true]) {
    let resolveLoad;
    sdk.deferredLoad = new Promise(resolve => { resolveLoad = resolve; });
    const loading = cast.castItem(media, 60);
    await tick(); await tick();
    const before = sdk.latePauses;
    cast.pauseCastPlayback();
    if (manualPlay) { sdk.remotePlayer.isPaused = true; cast.castTogglePlay(); }
    resolveLoad();
    await loading;
    assertEqual(sdk.latePauses - before, manualPlay ? 0 : 1,
      manualPlay ? 'manual Play supersedes expiry on deferred LOAD' : 'expiry still pauses deferred LOAD without manual Play');
    sdk.deferredLoad = null;
    sdk.remotePlayer.isPaused = false;
  }
  cast.stopCasting();
  assertEqual(JSON.stringify(events.at(-1)), JSON.stringify(['handback', 7, 60]), 'hand-back at the remote position');
  assert(!cast.isCasting(), 'no longer casting');
  await tick();
  assert(requests.some((r) => r.url === '/api/progress' && JSON.parse(r.body).position_seconds === 60), 'progress saved at the end');

  // A second session that plays the item to its end.
  sdk.startSession();
  await tick(); await tick();
  assertEqual(sdk.loads.length, 4, 'second session loads again');
  sdk.remotePlayer.isMediaLoaded = false;
  sdk.remotePlayer.playerState = 'IDLE';
  sdk.emit('PLAYER_STATE_CHANGED');
  assertEqual(JSON.stringify(events.at(-1)), JSON.stringify(['ended']), 'end on the receiver is reported');
  await tick();
  assert(requests.some((r) => r.url === '/api/progress/status' && r.body.includes('finished')), 'finished saved');
  const saved = requests.length;
  cast.stopCasting();
  assertEqual(JSON.stringify(events.at(-1)), JSON.stringify(['handback', 7, 0]), 'finished item hands back at the start');
  await tick();
  assertEqual(requests.length, saved, 'no progress written over "finished"');
}

console.log('Running cast tests...');
testSupport();
testLoadFields();
await testSession();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All cast tests passed.');
  process.exit(0);
}
