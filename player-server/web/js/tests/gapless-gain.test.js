import assert from 'node:assert/strict';
import { initAudioVisuals } from '../audioVisuals.js';
import { initGapless } from '../gapless.js';

// Run the real graph, settings controls, controller and scheduler against a
// deterministic audio clock. No browser audio output is claimed by this test.
class Element extends EventTarget {
  constructor(tag = 'div') {
    super(); this.tagName = tag.toUpperCase(); this.children = []; this.queries = new Map();
    this.classList = { toggle() {}, contains: () => false }; this.value = '';
  }
  appendChild(child) { this.children.push(child); }
  append(...children) { this.children.push(...children); }
  setAttribute() {}
  querySelector(selector) {
    if (!this.queries.has(selector)) this.queries.set(selector, new Element(selector));
    return this.queries.get(selector);
  }
  querySelectorAll(selector) {
    const key = `all:${selector}`;
    if (!this.queries.has(key)) this.queries.set(key,
      Array.from({ length: ['input[type="range"]', 'output'].includes(selector) ? 5 : 2 }, () => new Element(selector)));
    return this.queries.get(key);
  }
  getContext() { return {}; }
}
const document = new EventTarget();
document.createElement = tag => new Element(tag);
document.querySelector = () => null;
globalThis.document = document;
globalThis.location = { href: 'http://fixture/', origin: 'http://fixture' };
globalThis.localStorage = { getItem: key => ({ 'player-gapless': 'true', 'player-audio-visual': 'off',
  'player-loudness-v1': JSON.stringify({ mode: 'track', preamp: 0 }) })[key] ?? null, setItem() {} };
globalThis.MutationObserver = class { observe() {} };
globalThis.cancelAnimationFrame = () => {};

class AudioContext extends EventTarget {
  constructor() { super(); this.currentTime = 10; this.state = 'running'; this.gains = []; this.decoded = 0; }
  async resume() {}
  createGain() {
    const gain = { events: [], setValueAtTime: (value, when) => gain.events.push({ value, when }),
      cancelScheduledValues: when => { gain.events = gain.events.filter(event => event.when < when); } };
    this.gains.push(gain); return { gain, connect() {}, disconnect() {} };
  }
  createMediaElementSource() { return { connect() {} }; }
  createBiquadFilter() { return { gain: { setValueAtTime() {} }, frequency: {}, Q: {}, connect() {} }; }
  createBufferSource() { return { connect() {}, disconnect() {}, start() {}, stop() {} }; }
  async decodeAudioData() {
    const duration = this.decoded++ === 0 ? 8 : 7;
    return { duration, length: duration * 48000, numberOfChannels: 2 };
  }
}
globalThis.AudioContext = AudioContext;
globalThis.fetch = async () => new Response(new Uint8Array([1, 2]));
const audio = new Element('audio');
Object.assign(audio, { src: '/first', currentSrc: '/first', paused: true, playbackRate: 1, decoded: null });
const player = new Element(), controls = player.querySelector('.controls');
const graph = initAudioVisuals({ audio, player });
const song = (id, duration, gain) => ({ id, type: 'audio', duration,
  music: { replaygain_track_gain_db: gain, replaygain_track_peak: 1 } });
const current = song(1, 8, -6), next = song(2, 7, -12);
graph.setMedia(current);
const controller = initGapless({ audio, player, graph: () => graph,
  next: () => ({ media: next, token: 'next-occurrence' }), available: () => true,
  boundary: () => true, fallback: reason => { throw Error(reason); } });
audio.decoded = await controller.prepare(current, '/first', 0);
assert(audio.decoded);
const engine = audio.decoded, context = engine.context, gain = context.gains[0];
await controller.refresh(); await engine.play();
assert.equal(engine.next.when, 18);
assert.equal(gain.events.at(-1).value, 10 ** (-12 / 20));
context.currentTime = 11; engine.pause();
assert.equal(engine.next.node, null);
assert.equal(gain.events.at(-1).value, 10 ** (-6 / 20), 'pause restores current song multiplier');

// Let the cancelled deadline pass, then drive actual loudness and EQ controls.
context.currentTime = 30;
const loudness = controls.children.find(element => element.className === 'loudness-control hidden');
loudness.querySelector('select').value = 'track';
loudness.querySelectorAll('input')[0].value = '3';
loudness.dispatchEvent(new Event('change'));
const currentGain = 10 ** (-3 / 20), nextGain = 10 ** (-9 / 20);
assert.equal(gain.events.at(-1).value, currentGain, 'settings while paused keep current track gain');
assert(!gain.events.some(event => event.when === 18 && event.value === nextGain), 'cancelled deadline must not receive new gain automation');
const equalizer = controls.children.find(element => element.className === 'equalizer-control loudness-control hidden');
const slider = equalizer.querySelectorAll('input[type="range"]')[0];
slider.value = '6'; slider.dispatchEvent(new Event('input'));
assert.equal(gain.events.at(-1).value, currentGain, 'EQ change while paused cannot install successor gain');
await engine.play();
assert.equal(engine.next.when, 37);
assert.equal(gain.events.at(-1).when, 37);
assert.equal(gain.events.at(-1).value, nextGain, 'resume schedules updated successor gain only at new boundary');
assert.equal(gain.events.filter(event => event.when <= 30).at(-1).value, currentGain);

controller.reset(); engine.dispose();
console.log('Production graph/controller pause, loudness/EQ and resumed boundary gain regression passed.');
