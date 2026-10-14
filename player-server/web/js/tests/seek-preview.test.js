import assert from 'node:assert/strict';
import { initSeekPreview } from '../seekPreview.js';

// Deterministic media events/timers let us exercise pending seeks and stale
// callbacks without downloading a video or depending on browser timing.
let timers = new Map();
let nextTimer = 0;
globalThis.setTimeout = (fn, ms) => { timers.set(++nextTimer, { fn, ms }); return nextTimer; };
globalThis.clearTimeout = (id) => timers.delete(id);
function tick(ms) {
  for (const [id, timer] of [...timers]) {
    if (timer.ms <= ms && timers.has(id)) { timers.delete(id); timer.fn(); }
  }
}
let decoded = [];
let paints = [];
function element(tag) {
  const events = new Map();
  const attributes = new Map();
  const styles = new Map();
  return {
    tag, hidden: false, children: [], style: { setProperty: (k, v) => styles.set(k, v) }, styles,
    addEventListener: (name, fn) => events.set(name, [...(events.get(name) || []), fn]),
    emit(name, ev = {}) { for (const fn of events.get(name) || []) fn(ev); },
    append(...children) { this.children.push(...children); },
    appendChild(child) { this.children.push(child); },
    setAttribute: (k, v) => attributes.set(k, v),
    getAttribute: (k) => attributes.get(k) || null,
    removeAttribute: (k) => attributes.delete(k),
    getContext: () => ({ clearRect() {}, drawImage: (media) => paints.push(media.currentTime) }),
  };
}
globalThis.document = {
  head: element('head'),
  createElement(tag) {
    const el = element(tag);
    if (tag === 'video') {
      let position = 0;
      Object.assign(el, { readyState: 0, duration: 120, videoWidth: 640, videoHeight: 480, seeking: false, load() {} });
      Object.defineProperty(el, 'currentTime', {
        get: () => position,
        set: (value) => { position = value; el.seeking = true; },
      });
      decoded.push(el);
    }
    return el;
  },
};
function setup() {
  timers.clear(); decoded = []; paints = [];
  const track = element('track');
  const player = element('player');
  const video = element('video');
  video.duration = 120;
  video.currentTime = 7;
  video.paused = false;
  video.setAttribute('src', '/s/token/compat?view=credential');
  track.getBoundingClientRect = () => ({ left: 20, top: 180, width: 200 });
  player.getBoundingClientRect = () => ({ left: 0, bottom: 220, width: 240 });
  let active = video;
  const reset = initSeekPreview({ track, player, video, getVideo: () => active, formatTime: (seconds) => `${Math.floor(seconds)}s` });
  const move = (x, pointerType = 'mouse') => track.emit('pointermove', { clientX: x, pointerType });
  return { track, player, video, reset, move, setActive: (value) => { active = value; } };
}
function metadata(media) { media.readyState = 1; media.emit('loadedmetadata'); }
function seeked(media) { media.readyState = 2; media.seeking = false; media.emit('seeked'); }

{
  const s = setup();
  s.move(120);
  const [canvas, time, status] = s.player.children[0].children;
  assert.equal(decoded.length, 0, 'hover does not immediately download media');
  assert.equal(time.textContent, '60s');
  assert.equal(status.textContent, 'Loading preview…');
  tick(100);
  const media = decoded[0];
  assert.equal(media.src, '/s/token/compat?view=credential', 'reuse actual compatibility/share source');
  assert.equal(media.muted, true);
  metadata(media);
  assert.equal(media.currentTime, 60);
  s.move(180);
  tick(100);
  assert.equal(media.currentTime, 60, 'only one seek may run at once');
  seeked(media);
  assert.deepEqual(paints, [], 'outdated frame is discarded');
  assert.equal(media.currentTime, 96, 'seek latest hover after pending seek finishes');
  seeked(media);
  assert.deepEqual(paints, [96]);
  assert.equal(canvas.hidden, false);
  assert.equal(time.textContent, '96s');
  assert.equal(status.textContent, '');
  assert.equal(s.video.currentTime, 7, 'hover never seeks playback');
  assert.equal(s.video.paused, false, 'hover never pauses playback');
  s.move(-20); tick(100); seeked(media);
  assert.equal(media.currentTime, 0, 'left edge is clamped');
  s.move(999); tick(100); seeked(media);
  assert.equal(media.currentTime, 119.95, 'EOF preview selects a decodable frame');
  assert.equal(time.textContent, '120s');
  assert.equal(document.head.children.at(-1).textContent.includes('--preview-left: 72px'), true, 'right edge stays in player');
  s.track.emit('pointerleave');
  assert.equal(s.player.children[0].hidden, true);
  const before = paints.length;
  media.emit('seeked');
  assert.equal(paints.length, before, 'late event after leave cannot paint');
  assert.equal(timers.size, 0);
}
{
  const s = setup();
  s.move(120, 'touch'); tick(100);
  assert.equal(s.player.children.length, 0, 'touch does not start hover decoder');
  s.setActive(element('audio')); s.move(120); tick(100);
  assert.equal(decoded.length, 0, 'audio does not get a video preview');
  s.setActive(s.video); s.video.duration = Infinity; s.move(120); tick(100);
  assert.equal(decoded.length, 0, 'unknown/live duration does not request preview');
}
{
  const s = setup(); s.move(120); tick(100);
  const old = decoded[0]; metadata(old);
  s.reset();
  s.video.setAttribute('src', '/api/media/2/stream');
  s.move(60); tick(100);
  const current = decoded[1]; metadata(current);
  old.emit('error'); seeked(old);
  assert.equal(s.player.children[0].hidden, false, 'old source events do not hide new hover');
  assert.deepEqual(paints, [], 'old source cannot paint new preview');
  seeked(current);
  assert.deepEqual(paints, [24]);
  s.video.emit('emptied');
  assert.equal(s.player.children[0].hidden, true, 'source clear resets preview');
}
for (const failure of ['error', 'timeout']) {
  const s = setup(); s.move(120); tick(100);
  if (failure === 'error') decoded[0].emit('error');
  else tick(5000);
  const [canvas, time, status] = s.player.children[0].children;
  assert.equal(canvas.hidden, true);
  assert.equal(time.textContent, '60s');
  assert.equal(status.textContent, 'Preview unavailable');
  s.move(180); tick(100);
  assert.equal(decoded.length, 1, 'failed hover does not repeatedly download source');
  assert.equal(time.textContent, '96s', 'time remains useful if frames are unsupported');
  s.track.emit('pointerleave'); s.move(180); tick(100);
  assert.equal(decoded.length, 2, 'new hover can retry transient failure');
  s.reset();
}
console.log('Seek preview tests passed.');
