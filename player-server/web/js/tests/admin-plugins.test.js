import assert from 'node:assert/strict';
import { API } from '../api.js';
import { initAdmin } from '../admin.js';

// Exercise the public section toggle and rendered action handlers. In
// particular, persisted enabled plugins are loaded lazily after a restart.
function element() {
  const classes = new Set();
  return {
    handlers: {},
    classList: {
      contains: (name) => classes.has(name),
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
      toggle(name, force) {
        if (force) classes.add(name); else classes.delete(name);
      },
    },
    addEventListener(type, handler) { this.handlers[type] = handler; },
    setAttribute() {},
    focus() { document.activeElement = this; },
  };
}

const toggle = element();
const section = element();
section.classList.add('hidden');
const status = element();
const list = element();
Object.defineProperty(list, 'innerHTML', {
  get() { return this.html; },
  set(html) {
    this.html = html;
    this.buttons = [...html.matchAll(/<button\b[^>]*data-plugin-action="([^"]+)"[^>]*data-id="([^"]+)"[^>]*>/g)]
      .map((match) => Object.assign(element(), {
        dataset: { pluginAction: match[1], id: match[2] },
      }));
  },
});
list.querySelectorAll = () => list.buttons;
globalThis.document = {
  body: {},
  activeElement: {},
  getElementById: (id) => ({
    'admin-plugins-toggle': toggle,
    'admin-plugins-section': section,
    'admin-plugins-status': status,
    'admin-plugins': list,
  })[id] ?? null,
};
let installed = [{
  id: 'restored', name: '<Restored & ready>', version: '1.0.0',
  enabled: true, status: 'disabled', capabilities: ['probe'],
}];
let probes = 0;
API.plugins = async () => installed;
API.invokePlugin = async (id, capability) => {
  assert.equal(id, 'restored');
  assert.equal(capability, 'probe');
  probes++;
  installed = installed.map((plugin) => ({ ...plugin, status: 'active' }));
  return { result: 42 };
};
initAdmin();

await toggle.handlers.click();
assert.equal(status.textContent, '1 plugin on this server.');
assert.match(list.html, /enabled \(ready for probe\)/);
assert.match(list.html, /&lt;Restored &amp; ready&gt;/);
assert.deepEqual(list.buttons.map((button) => button.dataset.pluginAction), ['disable', 'probe']);
await list.buttons.find((button) => button.dataset.pluginAction === 'probe').handlers.click();
assert.equal(probes, 1, 'restored enabled plugin can invoke its declared capability');
assert.match(list.html, /active/);

// Disabled or undeclared capabilities must never offer invocation.
installed = [
  { id: 'off', name: 'Off', enabled: false, status: 'disabled', capabilities: ['probe'] },
  { id: 'invalid', name: 'Invalid', enabled: true, status: 'error', capabilities: [] },
];
await toggle.handlers.click();
await toggle.handlers.click();
assert.deepEqual(list.buttons.map((button) => button.dataset.pluginAction), ['enable', 'disable']);
assert.equal(probes, 1);
console.log('All admin plugin restart tests passed.');
