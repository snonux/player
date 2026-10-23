import assert from 'node:assert/strict';
import { initPlaybackSpeed, normalizePlaybackSpeed, PLAYBACK_SPEEDS } from '../playback-speed.js';

function element(tagName = '') {
  const listeners = new Map(), classes = new Set();
  return { tagName, children: [], attrs: {}, value: '',
    classList: { toggle(name, force) { force ? classes.add(name) : classes.delete(name); }, contains: name => classes.has(name) },
    appendChild(child) { this.children.push(child); },
    setAttribute(name, value) { this.attrs[name] = value; },
    addEventListener(name, fn) { listeners.set(name, fn); },
    emit(name, props = {}) { const ev = { target: this, ...props }; listeners.get(name)?.(ev); return ev; },
  };
}
const doc = element(), storageEvents = new Map(), saved = new Map();
let modal = false, owner = true;
globalThis.document = { createElement: element, addEventListener: doc.addEventListener,
  querySelector: () => modal ? {} : null };
globalThis.addEventListener = (name, handler) => storageEvents.set(name, handler);
globalThis.localStorage = { getItem: key => saved.get(key), setItem: (key, value) => saved.set(key, value) };
for (const value of ['garbage', undefined, null, 0, 3, Infinity, NaN, -1]) assert.equal(normalizePlaybackSpeed(value), 1);
for (const value of PLAYBACK_SPEEDS) assert.equal(normalizePlaybackSpeed(String(value)), value);
const controls = element(), audio = element('AUDIO');
audio.currentTime = 17; audio.paused = true;
const forwarded = [];
const rate = initPlaybackSpeed({ audio, player: { querySelector: () => controls }, ownsSource: () => owner,
  remoteStep: delta => { forwarded.push(delta); return true; } });
const label = controls.children[0], select = label.children[1], status = label.children[2];
assert.equal(select.attrs['aria-label'], 'Playback speed');
assert.deepEqual(select.children.map(option => option.textContent), ['0.5×', '1× Normal', '1.25×', '1.5×', '2×']);
assert.equal(label.classList.contains('hidden'), true);
rate.setMedia({ type: 'audio' });
assert.equal(label.classList.contains('hidden'), false);
for (const speed of PLAYBACK_SPEEDS) {
  select.value = String(speed); select.emit('change');
  assert.equal(audio.playbackRate, speed);
  assert.equal(audio.defaultPlaybackRate, speed);
  assert.equal(select.value, String(speed));
  assert.equal(audio.preservesPitch, true);
  assert.equal(audio.currentTime, 17, 'rate changes never seek');
  assert.equal(audio.paused, true, 'rate changes never play');
}
rate.step(1); assert.equal(rate.get(), 2, 'upper rate clamps');
rate.set(0.5); rate.step(-1); assert.equal(rate.get(), 0.5, 'lower rate clamps');
rate.set(1.5); rate.setMedia({ type: 'audio', is_podcast: true });
assert.equal(rate.get(), 1.5, 'podcasts and music use one explicit preference');
audio.playbackRate = 1; audio.emit('loadedmetadata'); assert.equal(audio.playbackRate, 1.5, 'source reload restores rate');
rate.setCasting(true); assert.equal(select.disabled, true); assert.match(status.textContent, /unavailable while casting/);
rate.set(2); assert.equal(audio.playbackRate, 1.5, 'never applies rate to Cast local placeholder');
assert.equal(rate.step(-1), false, 'shortcut cannot pretend to change receiver');
rate.setCasting(false); assert.equal(audio.playbackRate, 2); assert.equal(select.disabled, false);
owner = false; rate.set(1.25); assert.equal(audio.playbackRate, 2, 'nonowner never mutates hidden local audio');
assert.equal(rate.step(-1), true); assert.deepEqual(forwarded, [-1], 'main window forwards to owning popup');
owner = true; rate.apply(); assert.equal(audio.playbackRate, 1.25);
rate.setMedia({ type: 'image' }); assert.equal(label.classList.contains('hidden'), true);
rate.setMedia({ type: 'video' }); assert.equal(label.classList.contains('hidden'), true);
assert.equal(rate.step(1), false);
rate.setMedia({ type: 'audio' });
function key(key, target = {}, props = {}) {
  let prevented = false;
  doc.emit('keydown', { key, target, preventDefault() { prevented = true; }, ...props });
  return prevented;
}
for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) assert.equal(key('>', { tagName }), false);
assert.equal(key('>', { isContentEditable: true }), false);
assert.equal(key('>', {}, { ctrlKey: true }), false);
assert.equal(key('>', {}, { isComposing: true }), false);
assert.equal(key('>', { closest: () => ({}) }), false);
modal = true; assert.equal(key('>'), false); modal = false;
assert.equal(rate.get(), 1.25, 'native input and dialog journeys cannot mutate rate');
assert.equal(key('>'), true); assert.equal(rate.get(), 1.5);
assert.equal(key('<'), true); assert.equal(rate.get(), 1.25);
select.value = '1'; select.emit('change'); assert.equal(audio.playbackRate, 1, 'normal choice resets');
rate.set(1.5);
const nextControls = element();
const restarted = initPlaybackSpeed({ audio: element(), player: { querySelector: () => nextControls } });
assert.equal(restarted.get(), 1.5, 'preference survives another player/window');
storageEvents.get('storage')({ key: 'player-audio-speed', newValue: '2' });
assert.equal(restarted.get(), 2, 'other windows refresh their preference');
localStorage.getItem = () => { throw Error('blocked'); };
localStorage.setItem = () => { throw Error('blocked'); };
const blocked = initPlaybackSpeed({ audio: element(), player: { querySelector: () => element() } });
assert.equal(blocked.get(), 1); blocked.set(1.25); assert.equal(blocked.get(), 1.25, 'storage denial retains in-memory speed');
console.log('Playback speed rates, native select, reset, keys, persistence, ownership, Cast and source reload passed.');

// Exercise the real main-window handoff protocol, including a popup rate edit
// that hasn't reached the opener's asynchronous detach-state cache yet.
const { initDetach, toggleDetach, sendDetachedLoad } = await import('../detach.js');
let receive, popupState = null;
const messages = [], restored = [];
const track = { id: 23, type: 'audio' }, local = { media: track, playbackRate: 1.5, currentTime: 17, playing: false };
const popup = { closed: false, postMessage: message => messages.push(message),
  close() { this.closed = true; }, __playerDetachCurrentState: () => popupState };
globalThis.window = { location: { origin: 'http://local' }, open: () => popup, addEventListener: (_, fn) => receive = fn };
globalThis.document.getElementById = () => null;
initDetach({ els: () => ({ player: { classList: { add() {}, remove() {} } } }),
  localPlaybackState: () => local, getCurrentMedia: () => track,
  currentMediaElement: () => ({ pause() {} }), loadMedia: () => restored.push('loaded'),
  setPlaybackSpeed: speed => restored.push(speed), requestPlay: () => restored.push('played') });
toggleDetach();
receive({ origin: 'http://local', source: popup, data: { type: 'detach-ready' } });
assert.equal(messages.at(-1).playbackRate, 1.5, 'detach transfers initial selected speed');
popupState = { ...local, playbackRate: 2 };
sendDetachedLoad({ ...track, id: 24 }, 0, false);
assert.equal(messages.at(-1).playbackRate, 2, 'next track uses owning popup rate before cached state arrives');
popupState = { ...local, playbackRate: 1.25, playing: true };
toggleDetach();
assert.deepEqual(restored, ['loaded', 1.25, 'played'], 'return applies popup speed before resuming locally');
console.log('Detached rate transfer, fresh popup state and restore-before-play passed.');
