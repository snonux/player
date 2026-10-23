import assert from 'node:assert/strict';
import fs from 'node:fs';

function classList(initial = []) {
  const values = new Set(initial);
  return {
    add: (c) => values.add(c),
    remove: (c) => values.delete(c),
    contains: (c) => values.has(c),
    toggle(c, on) {
      if (on === undefined) {
        if (values.has(c)) values.delete(c); else values.add(c);
        return values.has(c);
      }
      if (on) values.add(c); else values.delete(c);
      return values.has(c);
    },
    has: (c) => values.has(c),
  };
}

let matchNarrow = true;
const pageClasses = classList(['page', 'has-sidebar']);
const sidebarClasses = classList(['sidebar', 'open']);
const setRow = { inSidebar: true, blur() { this.blurred = true; } };
const folderOpen = { focus() { this.focused = true; } };
const listeners = {};

const sidebar = {
  id: 'sidebar',
  classList: sidebarClasses,
  contains(el) { return Boolean(el?.inSidebar); },
};
const grid = {
  querySelector(sel) {
    return String(sel).includes('folder-open') ? folderOpen : null;
  },
};

globalThis.matchMedia = (query) => ({
  matches: matchNarrow && String(query).includes('860px'),
  addEventListener() {},
  removeEventListener() {},
});
globalThis.document = {
  activeElement: setRow,
  getElementById(id) {
    if (id === 'sidebar') return sidebar;
    if (id === 'media-grid') return grid;
    return null;
  },
  querySelector(sel) {
    if (sel === '.page') return { classList: pageClasses };
    return null;
  },
  querySelectorAll: () => [],
  addEventListener(type, fn, opts) { listeners[type] = { fn, opts }; },
  removeEventListener(type, fn) {
    if (listeners[type]?.fn === fn) delete listeners[type];
  },
};

const {
  NARROW_SIDEBAR_MQ,
  isNarrowSidebar,
  closeSidebar,
  dismissSidebarAfterNav,
  toggleSidebar,
} = await import('../views/help.js');

assert.match(NARROW_SIDEBAR_MQ, /860px/, 'breakpoint matches layout.css overlay');
assert.equal(isNarrowSidebar(), true);

assert.equal(dismissSidebarAfterNav(), true, 'narrow open sidebar dismisses after nav');
assert.equal(sidebarClasses.contains('open'), false);
assert.equal(pageClasses.contains('has-sidebar'), false);
assert.equal(setRow.blurred, true, 'focus leaves the overlay');
assert.equal(listeners['library:navigated']?.opts?.once, true, 'focus waits for content paint');
listeners['library:navigated'].fn();
assert.equal(folderOpen.focused, true, 'focus moves into content after navigate');

matchNarrow = false;
sidebarClasses.add('open');
pageClasses.add('has-sidebar');
delete listeners['library:navigated'];
assert.equal(isNarrowSidebar(), false);
assert.equal(dismissSidebarAfterNav(), false, 'desktop does not auto-close');
assert.equal(sidebarClasses.contains('open'), true);
assert.equal(pageClasses.contains('has-sidebar'), true);

assert.equal(closeSidebar(), true);
assert.equal(sidebarClasses.contains('open'), false);
assert.equal(closeSidebar(), false, 'idempotent when already closed');

toggleSidebar();
assert.equal(sidebarClasses.contains('open'), true);
toggleSidebar();
assert.equal(sidebarClasses.contains('open'), false);

matchNarrow = true;
sidebarClasses.add('open');
pageClasses.add('has-sidebar');
const { state } = await import('../state.js');
state.sets = [{ id: 1, name: 'A' }, { id: 2, name: 'B' }];
state.selectedSetId = 1;
state.selectedSetIds = [1];

const setsMod = await import('../views/sets.js');
let loaded = 0;
setsMod.initSets({ onLoadMedia: () => { loaded++; } });

assert.equal(setsMod.selectSetByHotkey('2'), true);
assert.equal(state.selectedSetId, 2);
assert.equal(sidebarClasses.contains('open'), false, 'hotkey set choice closes narrow sidebar');
assert.equal(loaded, 1);

sidebarClasses.add('open');
pageClasses.add('has-sidebar');
setsMod.setSetByDelta(1);
assert.equal(sidebarClasses.contains('open'), true, 'Tab/delta set cycle keeps sidebar open');

const mediaGridSrc = fs.readFileSync(new URL('../views/media-grid.js', import.meta.url), 'utf8');
assert.match(mediaGridSrc, /export function enterFolder\([\s\S]*?dismissSidebarAfterNav\(\)/);
const setsSrc = fs.readFileSync(new URL('../views/sets.js', import.meta.url), 'utf8');
assert.match(setsSrc, /if \(selectSet\(parseInt\(row\.dataset\.id, 10\)\)\) dismissSidebarAfterNav\(\)/);
assert.match(setsSrc, /export function selectSetByHotkey[\s\S]*?dismissSidebarAfterNav\(\)/);
const deltaBody = setsSrc.split('export function setSetByDelta')[1] || '';
assert.ok(!deltaBody.includes('dismissSidebarAfterNav'), 'Tab/delta must not dismiss sidebar');
const musicSrc = fs.readFileSync(new URL('../views/music-catalog.js', import.meta.url), 'utf8');
assert.match(musicSrc, /dismissSidebarAfterNav\(\)/);
const appSrc = fs.readFileSync(new URL('../app.js', import.meta.url), 'utf8');
assert.match(appSrc, /function openSet\(id\) \{[\s\S]*?dismissSidebarAfterNav\(\)/);
const layout = fs.readFileSync(new URL('../../css/layout.css', import.meta.url), 'utf8');
assert.match(layout, /@media \(max-width: 860px\)[\s\S]*?\.sidebar\.open/);

console.log('Narrow sidebar nav tests passed.');
