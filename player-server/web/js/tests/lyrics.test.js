import assert from 'node:assert/strict';
import { lyricsUrl, normalizeLyrics, currentLyricIndex, initLyrics, seekLyric } from '../lyrics.js';
const base = 'https://player.test/detach.html';
assert.equal(lyricsUrl('/api/media/12/stream', base), '/api/media/12/lyrics');
assert.equal(lyricsUrl('/api/v1/media/12/compat?unused=x', base), '/api/v1/media/12/lyrics');
assert.equal(lyricsUrl('/s/token/compat?view=abc.def&unused=x', base), '/s/token/lyrics?view=abc.def');
for (const url of ['/music.mp3', '/s/token', '/api/media/12/thumbnail', 'https://other.test/s/token/stream', 'http://[']) {
  assert.equal(lyricsUrl(url, base), null);
}
assert.deepEqual(normalizeLyrics(null).lines, []);
assert.deepEqual(normalizeLyrics({ lines: [{ text: ' \n' }] }).lines, []);
assert.deepEqual(normalizeLyrics({ lines: [null, {}, { text: '<b>literal</b>' }] }).lines, [{ text: '<b>literal</b>', start: undefined }]);
const lines = [{ start: 1, text: 'one' }, { start: 3, text: 'two' }, { start: 3, text: 'three' }];
assert.equal(normalizeLyrics({ synced: true, lines }).synced, true);
for (const times of [[1, 0], [1, NaN], [1, -1], [1, '3'], [1, undefined]]) {
  assert.equal(normalizeLyrics({ synced: true, lines: times.map(start => ({ start, text: 'line' })) }).synced, false);
}
for (const [time, expected] of [[0, -1], [1, 0], [2.99, 0], [3, 2], [200, 2], [NaN, -1]]) {
  assert.equal(currentLyricIndex(lines, time), expected);
}
assert.equal(currentLyricIndex([], 2), -1);
assert.equal(initLyrics({}), null);
const audio = { getAttribute: () => '/api/media/1/stream', readyState: 1, duration: 20, currentTime: 2, paused: true };
assert.equal(seekLyric(audio, 12.5), true);
assert.equal(audio.currentTime, 12.5);
assert.equal(audio.paused, true, 'seeking a lyric does not start paused playback');
assert.equal(seekLyric(audio, 0), true, 'first line can seek to zero');
for (const seconds of [NaN, Infinity, -1, 21, '4', undefined]) {
  assert.equal(seekLyric(audio, seconds), false);
  assert.equal(audio.currentTime, 0, 'invalid lyric timings never change playback');
}
for (const unavailable of [
  null, { ...audio, readyState: 0 }, { ...audio, duration: NaN },
  { ...audio, duration: Infinity }, { ...audio, duration: 0 },
  { ...audio, error: {} }, { ...audio, getAttribute: () => null },
]) assert.equal(seekLyric(unavailable, 4), false);
assert.equal(seekLyric({ ...audio, set currentTime(_) { throw Error('source unavailable'); } }, 4), false);
// Minimal DOM/event boundary for exercising the actual lyrics module without
// a browser runtime. Native keyboard activation remains a browser acceptance gap.
class Element {
  children = [];
  attributes = new Map();
  handlers = new Map();
  classList = { toggle() {} };
  scrollTop = 0;
  clientHeight = 40;
  offsetTop = 0;
  constructor(tag) { this.tagName = tag.toUpperCase(); }
  setAttribute(name, value) { this.attributes.set(name, value); }
  removeAttribute(name) { this.attributes.delete(name); }
  getAttribute(name) { return this.attributes.get(name); }
  appendChild(child) {
    if (child.tagName === 'FRAGMENT') this.children.push(...child.children);
    else this.children.push(child);
    this.children.forEach((row, i) => { row.offsetTop = i * 40; });
  }
  replaceChildren(...children) { this.children = []; children.forEach(child => this.appendChild(child)); }
  contains(node) { return this === node || this.children.some(child => child.contains(node)); }
  addEventListener(type, handler) {
    if (!this.handlers.has(type)) this.handlers.set(type, []);
    this.handlers.get(type).push(handler);
  }
  emit(type, event = {}) { this.handlers.get(type)?.forEach(handler => handler(event)); }
  focus() { document.activeElement = this; }
}
const originalDocument = globalThis.document;
const originalFetch = globalThis.fetch;
const originalLocation = globalThis.location;
try {
  globalThis.document = {
    activeElement: null,
    createElement: tag => new Element(tag),
    createDocumentFragment: () => new Element('fragment'),
  };
  globalThis.location = { href: base };
  let response = { synced: true, lines: [{ start: 0, text: 'First' }, { start: 4, text: 'Second' }, { start: 8, text: 'Third' }] };
  globalThis.fetch = async () => ({ ok: true, json: async () => response });
  const stage = new Element('div'), controls = new Element('div');
  const player = { classList: { toggle() {} }, querySelector: selector => selector === '.stage' ? stage : controls };
  const transport = new Element('audio');
  transport.currentTime = 0; // Paused local audio differs from the active receiver.
  let remoteTime = 4;
  let seeks = 0;
  const lyrics = initLyrics({ audio: transport, player, getPosition: () => remoteTime, onSeek: seconds => {
    seeks++;
    remoteTime = seconds;
    return true;
  } });
  lyrics.setMedia({ type: 'audio' }, '/api/media/1/stream');
  await new Promise(setImmediate);
  const panel = stage.children[0];
  const [first, second, third] = panel.children;
  assert.equal(second.getAttribute('aria-current'), 'true', 'initial highlight uses active remote position');
  assert.equal(second.tagName, 'BUTTON');
  assert.equal(second.getAttribute('aria-label'), 'Seek to 0:04: Second');
  second.focus();
  panel.scrollTop = 123;
  remoteTime = 8;
  lyrics.update(); // Receiver tick, with no local timeupdate event.
  assert.equal(third.getAttribute('aria-current'), 'true');
  assert.equal(second.getAttribute('aria-current'), undefined);
  assert.equal(document.activeElement, second, 'highlight changes preserve focused row');
  assert.equal(panel.scrollTop, 123, 'auto-follow cannot displace a focused reader');
  first.emit('click');
  assert.equal(seeks, 1);
  assert.equal(remoteTime, 0);
  assert.equal(transport.currentTime, 0);
  assert.equal(first.getAttribute('aria-current'), 'true', 'remote seek immediately recomputes highlight');
  assert.equal(panel.scrollTop, 0, 'explicit activation recenters its line');
  document.activeElement = null;
  panel.emit('wheel');
  panel.scrollTop = 99;
  remoteTime = 8;
  lyrics.update();
  assert.equal(panel.scrollTop, 99, 'manual scroll survives transport updates');
  panel.emit('pointerleave');
  assert.equal(panel.scrollTop, 80, 'leaving resumes remote follow');
  response = { synced: false, lines: [{ text: 'Plain' }] };
  lyrics.setMedia({ type: 'audio' }, '/api/media/2/stream');
  await new Promise(setImmediate);
  first.emit('click');
  assert.equal(seeks, 1, 'removed old line cannot seek replacement source');
  assert.equal(panel.children[0].tagName, 'P', 'untimed lyrics remain noninteractive');
  panel.scrollTop = 77;
  panel.emit('pointerleave');
  assert.equal(panel.scrollTop, 77, 'plain lyric scrolling remains intact');
} finally {
  globalThis.document = originalDocument;
  globalThis.fetch = originalFetch;
  globalThis.location = originalLocation;
}
console.log('Lyrics formatting, endpoint, transport seek, focus and reading tests passed.');
