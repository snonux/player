import assert from 'node:assert/strict';
import { initAudioVisuals } from '../audioVisuals.js';

class Element {
  constructor(tagName = 'DIV') { this.tagName = tagName; this.children = []; this.events = {}; this.value = ''; this.hidden = false; this.classList = { toggle() {}, contains: name => name === 'open' }; }
  addEventListener(name, fn) { this.events[name] = fn; }
  appendChild(node) { this.children.push(node); }
  append(...nodes) { this.children.push(...nodes); }
  setAttribute() {}
  set innerHTML(value) {
    this.parts = { select: new Element('SELECT'), details: new Element('DETAILS'), status: new Element('SPAN'), checkbox: new Element('INPUT') };
    this.inputs = [new Element('INPUT'), new Element('INPUT')];
    this.sliders = Array.from({length: 5}, () => new Element('INPUT'));
    this.outputs = Array.from({length: 5}, () => new Element('OUTPUT'));
    this.buttons = [new Element('BUTTON'), new Element('BUTTON')];
  }
  querySelector(selector) { return selector === 'select' ? this.parts.select : selector === 'details' ? this.parts.details : selector.includes('checkbox') ? this.parts.checkbox : this.parts.status; }
  querySelectorAll(selector) { return selector.includes('range') ? this.sliders : selector === 'output' ? this.outputs : selector === 'button' ? this.buttons : this.inputs; }
  getContext() { return {}; }
}
const controls = new Element(), stage = new Element(), audio = new Element('AUDIO');
audio.src = 'http://local/audio.mp3'; audio.paused = true;
const player = new Element(); player.querySelector = selector => selector === '.stage' ? stage : controls;
globalThis.document = { createElement: tag => new Element(tag.toUpperCase()), addEventListener() {}, dispatchEvent() {}, hidden: true };
globalThis.MutationObserver = class { observe() {} };
globalThis.cancelAnimationFrame = () => {};
globalThis.location = { origin: 'http://local', href: 'http://local/' };
globalThis.localStorage = { getItem: key => key === 'player-audio-visual' ? 'off' : null, setItem() {} };
const nodes = [];
function node(kind) {
  const result = { kind, links: [], connect(to) { this.links.push(to); }, gain: { value: 0, setValueAtTime(n) { this.value = n; } }, frequency: {}, Q: {} };
  nodes.push(result); return result;
}
let contexts = 0, captures = 0;
globalThis.AudioContext = class {
  constructor() { contexts++; this.state = 'running'; this.currentTime = 1; this.destination = node('destination'); }
  addEventListener() {}
  createGain() { return node('replaygain'); }
  createBiquadFilter() { return node('filter'); }
  createMediaElementSource() { captures++; return node('source'); }
};
const view = initAudioVisuals({ audio, player, coverArt: new Element() });
view.setMedia({ type: 'audio', music: { replaygain_track_gain_db: -6 } });
view.prepare();
assert.equal(contexts, 0, 'bypass plus visuals Off does not capture native playback');
view.setLoudnessState({ settings: { mode: 'track' }, equalizer: { enabled: true, bands: [6,3,0,-2,-4] } });
assert.equal(contexts, 1); assert.equal(captures, 1);
const filters = nodes.filter(n => n.kind === 'filter'), gain = nodes.find(n => n.kind === 'replaygain');
assert.deepEqual(filters.map(n => n.gain.value), [0,-3,-6,-8,-10]);
assert(Math.abs(gain.gain.value - 10 ** (-6/20)) < 0.000001, 'ReplayGain stays independent');
assert.deepEqual(nodes.find(n => n.kind === 'source').links, [filters[0]]);
filters.forEach((filter, i) => assert.deepEqual(filter.links, [filters[i + 1] || gain]));
assert.deepEqual(gain.links, [nodes[0]], 'only one audible destination path');
view.setLoudnessState({ equalizer: { enabled: false, bands: [6,3,0,-2,-4] } });
assert.deepEqual(filters.map(n => n.gain.value), [0,0,0,0,0]);
view.setMedia({type:'audio'}); view.prepare();
assert.equal(contexts, 1); assert.equal(captures, 1, 'track replacement reuses source/context');
assert.equal(view.loudnessState().equalizer.enabled, false);
view.setCasting(true);
const eqPanel = controls.children.find(child => child.className?.includes('equalizer-control'));
assert.match(eqPanel.parts.status.textContent, /Cast uses receiver effects/);
view.setCasting(false);
assert.match(eqPanel.parts.status.textContent, /bypassed/);
console.log('Production equalizer graph preserves one context/source, bypass and ReplayGain chain.');
