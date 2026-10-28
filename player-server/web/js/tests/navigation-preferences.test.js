import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { normalizeNavigation, coldLanding, readNavigationPreferences, initNavigationPreferences, musicSections } from '../navigation-preferences.js';

const indexHtml = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../index.html'), 'utf8');
assert.match(indexHtml, /<details class="all-music-views">[\s\S]*data-music-open="albums"[\s\S]*data-music-open="artists"[\s\S]*data-music-open="playlists"[\s\S]*<\/details>/);
assert.match(indexHtml, /Hidden shortcuts remain available in All music views/);
assert.doesNotMatch(indexHtml, /id="all-music-views"/);
assert.equal(normalizeNavigation().landing, 'library');
assert.equal(normalizeNavigation({ landing: 'invalid' }).landing, 'library');
assert.equal(normalizeNavigation({ landing: 'albums', visible: { albums: false } }).landing, 'library');
assert.equal(normalizeNavigation({ landing: 'artists' }).landing, 'artists');
for (const boundary of [{ search: '?music=album&id=x' }, { search: '?set=3' }, { hash: '#restored' }, { historyNavigation: true }]) {
  assert.equal(coldLanding({ landing: 'albums' }, boundary), 'library');
}
assert.equal(coldLanding({ landing: 'playlists' }), 'playlists');
globalThis.localStorage = { getItem() { throw Error('unavailable'); } };
assert.deepEqual(readNavigationPreferences(), normalizeNavigation());
globalThis.localStorage = { getItem() { return '{bad'; } };
assert.deepEqual(readNavigationPreferences(), normalizeNavigation());

const elements = new Map();
const documentListeners = new Map();
function el(id, attrs = {}) {
  const nodeListeners = new Map();
  const node = {
    id,
    hidden: false,
    checked: false,
    value: '',
    textContent: '',
    classList: { add() {}, remove() {}, contains() { return false; } },
    getAttribute(name) { return attrs[name] ?? null; },
    addEventListener(type, fn) {
      const list = nodeListeners.get(type) || [];
      list.push(fn);
      nodeListeners.set(type, list);
    },
    focus() { node.focused = true; },
    click() {
      for (const fn of nodeListeners.get('click') || []) fn({ target: node, preventDefault() {} });
    },
  };
  Object.assign(node, attrs);
  if (id) elements.set(id, node);
  return node;
}
const shortcuts = Object.fromEntries(musicSections.map(name => [name, el(`music-${name}`)]));
const checks = Object.fromEntries(musicSections.map(name => [name, el(`navigation-${name}`)]));
const landing = el('navigation-landing', { value: 'library' });
const status = el('navigation-status');
const settings = el('settings-modal');
settings.classList = { open: true, add(c) { if (c === 'open') this.open = true; }, remove(c) { if (c === 'open') this.open = false; }, contains(c) { return c === 'open' && this.open; } };
const openButtons = musicSections.map(name => el(`all-music-${name}`, { 'data-music-open': name }));
const sidebarButtons = musicSections.map(name => el('', { 'data-music-open': name }));
const allOpen = [...openButtons, ...sidebarButtons];
globalThis.localStorage = {
  store: { 'player-navigation': JSON.stringify({ landing: 'library', visible: { albums: false, artists: true, playlists: false } }) },
  getItem(k) { return this.store[k] ?? null; },
  setItem(k, v) { this.store[k] = String(v); },
};
globalThis.document = {
  getElementById(id) {
    if (id === 'settings-modal') return settings;
    if (id === 'settings-toggle') return el('settings-toggle');
    return elements.get(id) || null;
  },
  querySelector() { return null; },
  querySelectorAll(sel) {
    if (sel === '[data-music-open]') return allOpen;
    return [];
  },
  addEventListener(type, fn) {
    documentListeners.set(`document:${type}`, fn);
  },
};
globalThis.window = {
  addEventListener(type, fn) { documentListeners.set(`window:${type}`, fn); },
};
const opened = [];
initNavigationPreferences(name => opened.push(name));
assert.equal(shortcuts.albums.hidden, true, 'hidden Albums shortcut stays out of the menu');
assert.equal(shortcuts.artists.hidden, false);
assert.equal(shortcuts.playlists.hidden, true);
assert.equal(checks.albums.checked, false);
assert.equal(checks.artists.checked, true);
sidebarButtons[0].click();
assert.deepEqual(opened, ['albums'], 'sidebar All music views opens Albums directly');
assert.equal(settings.classList.open, false, 'opening a music view closes Settings if it was open');
openButtons[2].click();
assert.deepEqual(opened, ['albums', 'playlists'], 'Settings All music views still opens destinations');
assert.equal(elements.has('all-music-views'), false, 'All music views is a disclosure, not a Settings shortcut button');
console.log('Navigation preference fallbacks, hidden shortcuts, and direct All music views destinations passed.');
