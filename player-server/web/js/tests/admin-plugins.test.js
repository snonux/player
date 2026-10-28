import assert from 'node:assert/strict';
import { API } from '../api.js';
import { initAdmin, showAdminSection } from '../admin.js';

// Exercise section navigation and rendered plugin action handlers. In
// particular, persisted enabled plugins are loaded lazily after a restart.
function element(extra = {}) {
  const classes = new Set();
  return {
    handlers: {},
    dataset: {},
    classList: {
      contains: (name) => classes.has(name),
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
      toggle(name, force) {
        if (arguments.length > 1) {
          if (force) classes.add(name); else classes.delete(name);
        } else if (classes.has(name)) classes.delete(name); else classes.add(name);
      },
    },
    addEventListener(type, handler) { this.handlers[type] = handler; },
    setAttribute() {},
    focus() { document.activeElement = this; },
    ...extra,
  };
}

const panels = {
  users: Object.assign(element(), { dataset: { adminPanel: 'users' } }),
  permissions: Object.assign(element(), { dataset: { adminPanel: 'permissions' } }),
  scan: Object.assign(element(), { dataset: { adminPanel: 'scan' } }),
  trash: Object.assign(element(), { dataset: { adminPanel: 'trash' } }),
  libraries: Object.assign(element(), { dataset: { adminPanel: 'libraries' } }),
  identity: Object.assign(element(), { dataset: { adminPanel: 'identity' } }),
  plugins: Object.assign(element(), { dataset: { adminPanel: 'plugins' } }),
  signin: Object.assign(element(), { dataset: { adminPanel: 'signin' } }),
};
Object.values(panels).forEach((p) => p.classList.add('hidden'));
panels.users.classList.remove('hidden');

const sectionSelect = element({ value: 'users' });
const rescanBtn = element();
rescanBtn.classList.add('hidden');
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
    'admin-section-select': sectionSelect,
    'admin-rescan': rescanBtn,
    'admin-plugins-status': status,
    'admin-plugins': list,
    'admin-users': Object.assign(element(), { querySelectorAll: () => [] }),
    'admin-permissions': element(),
    'admin-auth-help': element(),
    'admin-more-toggle': element(),
    'admin-more-menu': Object.assign(element(), {
      querySelector: () => null,
      querySelectorAll: () => [],
      contains: () => false,
    }),
  })[id] ?? null,
  querySelectorAll: (sel) => {
    if (sel === '[data-admin-panel]') return Object.values(panels);
    return [];
  },
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
API.users = async () => [];
API.permissions = async () => ({ sets: [], users: [], permissions: [] });
API.config = async () => ({});

initAdmin();

await showAdminSection('plugins');
assert.equal(sectionSelect.value, 'plugins');
assert.equal(panels.plugins.classList.contains('hidden'), false);
assert.equal(panels.users.classList.contains('hidden'), true);
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
await showAdminSection('users');
await showAdminSection('plugins');
assert.deepEqual(list.buttons.map((button) => button.dataset.pluginAction), ['enable', 'disable']);
assert.equal(probes, 1);

await showAdminSection('scan');
assert.equal(rescanBtn.classList.contains('hidden'), false, 'rescan is contextual to Library scan');
await showAdminSection('users');
assert.equal(rescanBtn.classList.contains('hidden'), true);

console.log('All admin plugin restart tests passed.');
