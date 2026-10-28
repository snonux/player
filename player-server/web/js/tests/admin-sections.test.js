import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { API } from '../api.js';
import { initAdmin, showAdminSection } from '../admin.js';

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

const panels = Object.fromEntries(
  ['users', 'permissions', 'scan', 'trash', 'libraries', 'identity', 'plugins', 'signin']
    .map((name) => [name, Object.assign(element(), { dataset: { adminPanel: name } })]),
);
Object.values(panels).forEach((p) => p.classList.add('hidden'));
panels.users.classList.remove('hidden');

const sectionSelect = element({ value: 'users' });
const rescanBtn = element();
rescanBtn.classList.add('hidden');
const cancelBtn = element({ disabled: true });
const moreToggle = element();
const moreMenu = Object.assign(element(), {
  querySelector: () => null,
  querySelectorAll: () => [],
  contains: () => false,
  showPopover() {},
  hidePopover() {},
});
const trashList = element();
Object.defineProperty(trashList, 'innerHTML', {
  get() { return this.html || ''; },
  set(html) { this.html = html; },
});
trashList.querySelectorAll = () => [];

let trashLoads = 0;
API.trash = async () => { trashLoads++; return []; };
API.users = async () => [{ id: 1, username: 'admin', is_admin: true }];
API.permissions = async () => ({ sets: [], users: [], permissions: [] });
API.config = async () => ({ auth: { proxy: false } });
API.adminLibraries = async () => [];
API.musicIdentityConflicts = async () => [];
API.plugins = async () => [];

globalThis.document = {
  body: {},
  activeElement: {},
  getElementById: (id) => ({
    'admin-toggle': element(),
    'admin-modal': element(),
    'admin-close': element(),
    'admin-rescan': rescanBtn,
    'admin-scan-cancel': cancelBtn,
    'admin-section-select': sectionSelect,
    'admin-more-toggle': moreToggle,
    'admin-more-menu': moreMenu,
    'admin-user-form': element(),
    'admin-library-form': element(),
    'admin-users': element(),
    'admin-permissions': element(),
    'admin-auth-help': element(),
    'admin-trash-list': trashList,
    'admin-libraries': element(),
    'admin-libraries-status': element(),
    'admin-identity-conflicts': element(),
    'admin-identity-status': element(),
    'admin-plugins': element(),
    'admin-plugins-status': element(),
  })[id] ?? null,
  querySelectorAll: (sel) => {
    if (sel === '[data-admin-panel]') return Object.values(panels);
    return [];
  },
};

initAdmin();

assert.ok(cancelBtn, 'Cancel scan stays in the toolbar DOM');
assert.equal(cancelBtn.disabled, true);

await showAdminSection('permissions');
assert.equal(sectionSelect.value, 'permissions');
assert.equal(panels.permissions.classList.contains('hidden'), false);
assert.equal(panels.users.classList.contains('hidden'), true);
assert.equal(rescanBtn.classList.contains('hidden'), true);

await showAdminSection('scan');
assert.equal(rescanBtn.classList.contains('hidden'), false);

await showAdminSection('trash');
assert.equal(trashLoads, 1);
assert.equal(panels.trash.classList.contains('hidden'), false);

sectionSelect.value = 'libraries';
await sectionSelect.handlers.change();
assert.equal(panels.libraries.classList.contains('hidden'), false);

const gotoBtn = Object.assign(element(), { dataset: { adminGoto: 'identity' } });
moreMenu.handlers.click({ target: { closest: () => gotoBtn } });
assert.equal(sectionSelect.value, 'identity');
assert.equal(panels.identity.classList.contains('hidden'), false);

let moreHidden = false;
moreMenu.hidePopover = () => { moreHidden = true; };
moreMenu.querySelectorAll = () => [gotoBtn];
const keyEvent = (key) => {
  const event = {
    key,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    stopped: false,
    prevented: false,
    preventDefault() { this.prevented = true; },
    stopPropagation() { this.stopped = true; },
  };
  moreMenu.handlers.keydown(event);
  return event;
};
const escape = keyEvent('Escape');
assert.equal(escape.stopped, true, 'Escape must not bubble to closeAllModals');
assert.equal(escape.prevented, true);
assert.equal(moreHidden, true);
assert.equal(document.activeElement, moreToggle, 'Escape restores the More toggle');
const j = keyEvent('j');
assert.equal(j.stopped, true, 'j must not bubble into the Admin modal shortcuts');
assert.equal(j.prevented, true);
const k = keyEvent('k');
assert.equal(k.stopped, true, 'k must not bubble into the Admin modal shortcuts');
assert.equal(k.prevented, true);

const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
assert.match(html, /id="admin-section-select"/, 'admin uses a section selector');
assert.match(html, /id="admin-more-menu"/, 'advanced admin actions live in a More menu');
assert.match(html, /id="admin-scan-cancel"/, 'Cancel scan remains in the admin chrome');
const cancelAt = html.indexOf('id="admin-scan-cancel"');
const panelsAt = html.indexOf('id="admin-panels"');
assert.ok(cancelAt > 0 && panelsAt > cancelAt, 'Cancel scan stays outside focused panels');

console.log('All admin section navigation tests passed.');
