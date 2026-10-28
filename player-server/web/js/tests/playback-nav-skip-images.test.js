// B3 / nm3: Sets Next from audio must skip folder cover.jpg and never hand
// playback to the image viewer (which swallows Shift+N/Shift+P).

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}
function assertEqual(actual, expected, msg) {
  assert(actual === expected, `${msg}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

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
    appendChild(child) { if(child.isFragment)el.children.push(...child.children);else el.children.push(child); elements[child.id] = child; },
    replaceChildren(...children) {el.children=[];el.textContent='';for(const child of children)el.appendChild(child);},
    contains(child) {return el===child || el.children.some(node=>node.contains?.(child));},
    prepend(child) { el.children.unshift(child); elements[child.id] = child; },
    querySelector: selector => el.children.find(child => selector === `.${child.className}`) || null,
    scrollIntoView() {},
    dataset: {},
  };
  return el;
}

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
  head: { appendChild() {} },
  fullscreenElement: null,
  getElementById: (id) => elements[id] || null,
  createElement: () => fakeElement(),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener(type, fn) { (documentListeners[type] ||= []).push(fn); },
  dispatchEvent(event) { for (const fn of documentListeners[event.type] || []) fn(event); return true; },
};
globalThis.window = { addEventListener() {}, location: { origin: 'http://test' } };
globalThis.location = { pathname: '/', href: '/' };
console.log = () => {};

const realSetTimeout = globalThis.setTimeout;
globalThis.setTimeout = (fn, _ms, ...args) => realSetTimeout(fn, 0, ...args);
const settle = () => new Promise((resolve) => realSetTimeout(resolve, 20));

function response(status, headers = {}) {
  return { status, headers: { get: (name) => headers[name] ?? null } };
}
let answer = async () => response(200);
globalThis.fetch = (url, init) => answer(url, init);

const osActions = {};
const os = { metadata: null, setActionHandler(action, callback) { osActions[action] = callback; }, setPositionState() {} };
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { mediaSession: os } });
globalThis.MediaMetadata = class { constructor(fields) { Object.assign(this, fields); } };

const player = await import('../playback.js');
const { API } = await import('../api.js');
const queue = await import('../queue.js');
queue.initQueue();
const nav = await import('../views/playback-nav.js');
const { state } = await import('../state.js');
const selection = await import('../selection.js');

function card(id, index) {
  const el = fakeElement();
  el.classList.add('media-card');
  el.dataset = { id: String(id), index: String(index) };
  el.scrollIntoView = () => {};
  return el;
}

function setupAlbumGrid(items) {
  resetDom();
  player.initPlayer({ onPlaybackFailed() {} });
  queue.deactivateQueue();
  state.music = null;
  state.media = items.map((item) => ({ ...item }));
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.folderPath = 'Album';
  const cards = items.map((item, index) => card(item.id, index));
  document.querySelectorAll = (selector) => {
    if (String(selector).includes('#media-grid') || String(selector).includes('.media-card') || String(selector).includes('.media-row')) {
      return cards;
    }
    return [];
  };
  document.querySelector = (selector) => {
    const match = String(selector).match(/data-id="(\d+)"/);
    if (match) return cards.find((c) => c.dataset.id === match[1]) || null;
    return null;
  };
  return cards;
}

async function testNextAfterLastAudioSkipsCoverAndWrapsAmongAudio() {
  const song1 = { id: 1, type: 'audio', file_name: '01.mp3', duration: 12, browse_path: 'Album' };
  const song2 = { id: 2, type: 'audio', file_name: '02.mp3', duration: 12, browse_path: 'Album' };
  const cover = { id: 3, type: 'image', file_name: 'cover.jpg', browse_path: 'Album' };
  const cards = setupAlbumGrid([song1, song2, cover]);
  const originalDetail = API.mediaDetail;
  API.mediaDetail = async () => ({ progress: null });
  try {
    selection.selectByElement(cards[1]);
    await nav.playMediaById(2);
    assertEqual(player.currentMediaId(), 2, 'start on last audio track');
    assertEqual(player.currentMediaInfo()?.type, 'audio', 'playing audio');
    await nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 1, 'Next after last audio wraps to first audio');
    assertEqual(player.currentMediaInfo()?.type, 'audio', 'Next never opens cover.jpg');
    await nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 2, 'Next advances to second audio, still skipping cover');
    assertEqual(player.currentMediaInfo()?.type, 'audio', 'cover stays out of the audio sequence');
    await nav.navigatePlayable(-1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 1, 'Previous stays on audio');
    await nav.navigatePlayable(-1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 2, 'Previous from first wraps to last audio, not cover');
  } finally {
    API.mediaDetail = originalDetail;
  }
}

async function testCrossFolderNextSkipsCoverAndStopsWithoutImage() {
  const song1 = { id: 11, type: 'audio', file_name: '01.mp3', duration: 12, browse_path: 'Album' };
  const song2 = { id: 12, type: 'audio', file_name: '02.mp3', duration: 12, browse_path: 'Album' };
  const cover = { id: 13, type: 'image', file_name: 'cover.jpg', browse_path: 'Album' };
  const cards = setupAlbumGrid([song1, song2, cover]);
  state.selectedSetId = 7;
  state.selectedSetIds = [7];
  state.folderPath = 'Album';
  const originalDetail = API.mediaDetail;
  const originalBrowse = API.browse;
  const album = { current_path: 'Album', folders: [], media: [song1, song2, cover] };
  const root = { current_path: '', folders: [{ name: 'Album' }], media: [] };
  API.mediaDetail = async () => ({ progress: null });
  API.browse = async (_setId, path) => (path === 'Album' || path === 'Album/' ? album : root);
  try {
    selection.selectByElement(cards[1]);
    await nav.playMediaById(12);
    assertEqual(player.currentMediaId(), 12, 'start on last audio');
    await nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaInfo()?.type, 'audio', 'folder-edge Next never selects cover.jpg');
    assertEqual(player.currentMediaId(), 11, 'set wrap lands on first audio of the album');
  } finally {
    API.mediaDetail = originalDetail;
    API.browse = originalBrowse;
  }
}

async function testVideoSequenceStillReachesImages() {
  const clip = { id: 21, type: 'video', file_name: 'clip.mp4', duration: 12, browse_path: 'Album' };
  const cover = { id: 22, type: 'image', file_name: 'cover.jpg', browse_path: 'Album' };
  const cards = setupAlbumGrid([clip, cover]);
  const originalDetail = API.mediaDetail;
  API.mediaDetail = async () => ({ progress: null });
  try {
    selection.selectByElement(cards[0]);
    await nav.playMediaById(21);
    assertEqual(player.currentMediaInfo()?.type, 'video', 'start on video');
    await nav.navigatePlayable(1, { forcePlay: true });
    assertEqual(player.currentMediaId(), 22, 'video-started sequence still advances to images');
    assertEqual(player.currentMediaInfo()?.type, 'image', 'image remains reachable from video');
  } finally {
    API.mediaDetail = originalDetail;
  }
}

async function testSignedInSavedProductionLyricsCaller(){
 resetDom();
 const stage=fakeElement('stage'),controls=fakeElement('controls');
 elements.player.querySelector=selector=>selector==='.stage'?stage:selector==='.controls'?controls:null;
 document.createDocumentFragment=()=>({...fakeElement('fragment'),isFragment:true});
 const oldLocation=globalThis.location,oldStorage=globalThis.localStorage,oldCaches=globalThis.caches;
 globalThis.location={origin:'http://test',pathname:'/',href:'http://test/',search:''};
 const url='http://test/api/media/99/download',lyricsKey=url+'?player_lyrics=1';
 const song={id:99,type:'audio',file_name:'saved.mp3',duration:12,offlinePinURL:url};
 globalThis.localStorage={getItem:key=>key==='player-offline-v1-7:index'?JSON.stringify({songs:{[url]:{media:song,url,lyricsKey,etag:'saved-a'}},collections:{}}):null};
 globalThis.caches={open:async()=>({match:async key=>key===url?new Response(new Uint8Array([1,2,3]),{headers:{etag:'saved-a'}}):key===lyricsKey?new Response(JSON.stringify({synced:true,lines:[{text:'Cold saved line',start:0}]}),{headers:{'x-player-audio-etag':'saved-a'}}):null})};
 const oldUser=state.user;state.user={id:7};
 const {setPlaybackCacheAccount}=await import("../playback-cache.js");setPlaybackCacheAccount(7);
 const oldAnswer=answer;answer=async()=>{throw new TypeError("Network unavailable");};
 try {
  player.initPlayer();
  player.selectMedia(song,0);await settle();await settle();
  const panel=stage.children.find(child=>child.id==='audio-lyrics');
  const text=node=>(node?.textContent||'')+(node?.children||[]).map(text).join('');
  assertEqual(text(panel?.children[0]),'Cold saved line','production showMedia retains identity for signed-in saved lyrics');
 }finally {player.stopAndClose();setPlaybackCacheAccount(null);state.user=oldUser;answer=oldAnswer;globalThis.location=oldLocation;globalThis.localStorage=oldStorage;globalThis.caches=oldCaches;}
}

for (const test of [
  testNextAfterLastAudioSkipsCoverAndWrapsAmongAudio,
  testCrossFolderNextSkipsCoverAndStopsWithoutImage,
  testVideoSequenceStillReachesImages,
  testSignedInSavedProductionLyricsCaller,
]) {
  try {
    await test();
    await settle();
  } catch (err) {
    failures.push(`${test.name} threw: ${err?.stack || err}`);
  }
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.info('All playback-nav skip-images tests passed.');
process.exit(0);
