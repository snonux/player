import assert from 'node:assert/strict';
import { createSleepTimer } from '../sleep-timer.js';
let now = 1000, expired = 0;
const timer = createSleepTimer({ now: () => now, expire: () => expired++ });
assert.equal(timer.remaining(), null);
assert.equal(timer.start(NaN), false);
timer.start(5000); now += 1000;
assert.equal(timer.remaining(), 4000);
timer.start(2000); now += 1999;
assert.equal(timer.tick(), false);
now++; assert.equal(timer.tick(), true); assert.equal(expired, 1);
timer.tick(); assert.equal(expired, 1, 'exactly one pause');
timer.start(2000); timer.cancel(); now += 5000; timer.tick(); assert.equal(expired, 1);
timer.restore(now - 1); timer.tick(); assert.equal(expired, 2, 'expired transferred deadline pauses');
timer.start(5000); now += 1000;
const popup = createSleepTimer({ now: () => now, expire: () => expired++ });
popup.restore(timer.deadline()); assert.equal(popup.remaining(), 4000);
now += 5000; popup.tick(); assert.equal(expired, 3, 'background delay expires on next run');
const restarted = createSleepTimer({ now: () => now, expire: () => expired++ });
assert.equal(restarted.remaining(), null, 'process/reload starts off');
console.log('Sleep countdown start/restart/cancel/expiry/transfer/reload passed.');

// The actual secondary controls preserve native selects and expose cancellation.
const { initSleepTimer } = await import('../sleep-timer.js');
const handlers = new Map();
function element(tag = '') {
  const listeners = new Map();
  return { tagName: tag, children: [], attributes: {}, hidden: false,
    classList: { toggle() {} },
    setAttribute(k, v) { this.attributes[k] = v; },
    appendChild(child) { this.children.push(child); },
    addEventListener(k, v) { listeners.set(k, v); },
    click() { listeners.get('click')?.(); }, focus() { this.focused = true; },
  };
}
const controls = element();
globalThis.document = { createElement: element, querySelector: () => null,
  addEventListener: (name, listener) => handlers.set(name, listener) };
const oldInterval = globalThis.setInterval;
globalThis.setInterval = () => 1;
const ui = initSleepTimer({ player: { querySelector: () => controls }, expire() {} });
globalThis.setInterval = oldInterval;
ui.setMedia({ type: 'audio' });
const [select, start, status, cancel] = controls.children[0].children;
assert.equal(select.attributes['aria-label'], 'Sleep duration');
assert.equal(cancel.hidden, true);
start.click(); assert(ui.remaining() > 0); assert.equal(cancel.hidden, false);
assert.match(status.textContent, /remaining/);
cancel.click(); assert.equal(ui.remaining(), null); assert.equal(start.focused, true);
let prevented = false;
handlers.get('keydown')({ key: 'Z', target: { tagName: 'SELECT' }, preventDefault() { prevented = true; } });
assert.equal(prevented, false, 'duration select keeps native typing');
handlers.get('keydown')({ key: 'Z', target: {}, preventDefault() { prevented = true; } });
assert.equal(prevented, true); assert.equal(select.focused, true);
console.log('Sleep picker, visible remaining, cancel focus and typing-safe Z passed.');
