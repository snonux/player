import assert from 'node:assert/strict';
import { initHelp, toggleHelp, focusHelpSearch } from '../views/help.js';

let onModalChange;
globalThis.MutationObserver = class { constructor(callback) { onModalChange = callback; } observe() {} };
const listeners = {};
const rows = ['↑ ↓ Navigate media list', 'f Toggle FULLSCREEN', '/ Search library; filter shortcuts in help', 'Shift+↑ Nudge crop position'].map(textContent => ({ textContent, hidden: false }));
const input = { value: '', addEventListener: (type, fn) => { listeners[type] = fn; }, focus() { this.focused = true; }, select() { this.selected = true; } };
const empty = { hidden: true };
let open = false;
const nextSibling = {};
const parent = { insertBefore(node, next) { assert.equal(next, nextSibling); node.parentNode = this; } };
const modal = {
  parentNode: parent, nextSibling, contains: () => false,
  classList: { contains: () => open, toggle: () => { open = !open; } },
  addEventListener() {},
  querySelectorAll: () => rows,
};
globalThis.document = { addEventListener(type, callback) { listeners[type] = callback; }, getElementById: id => ({ 'help-modal': modal, 'help-search': input, 'help-no-results': empty }[id] || null) };
initHelp();
assert(rows.every(row => !row.hidden));
input.value = '  FuLlScReEn  ';
listeners.input();
assert.deepEqual(rows.map(row => row.hidden), [true, false, true, true]);
assert.equal(empty.hidden, true);
input.value = 'Shift+↑';
listeners.input();
assert.deepEqual(rows.map(row => row.hidden), [true, true, true, false]);
input.value = 'not a shortcut';
listeners.input();
assert(rows.every(row => row.hidden));
assert.equal(empty.hidden, false);
input.value = ' ';
listeners.input();
assert(rows.every(row => !row.hidden));
assert.equal(empty.hidden, true);
input.value = 'old query';
toggleHelp();
assert.equal(input.value, '');
assert.equal(open, true);
assert(rows.every(row => !row.hidden));
focusHelpSearch();
assert(input.focused && input.selected);
toggleHelp(); // close before reopening fullscreen
const fullscreen = { contains: () => false, appendChild(node) { node.parentNode = this; } };
document.fullscreenElement = fullscreen;
toggleHelp();
assert.equal(modal.parentNode, fullscreen, 'Fullscreen help must render inside its target');
document.fullscreenElement = null;
listeners.fullscreenchange();
assert.equal(open, true, 'Browser fullscreen exit must keep help open');
assert.equal(modal.parentNode, parent, 'Browser fullscreen exit restores help into the page');
toggleHelp();
onModalChange();
assert.equal(modal.parentNode, parent, 'Any help close restores its original DOM position');
console.log('Help search tests passed.');
