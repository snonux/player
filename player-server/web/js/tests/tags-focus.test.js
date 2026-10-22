import assert from 'node:assert/strict';
import { API } from '../api.js';
import { state } from '../state.js';
import { clearSelection, selectByElement } from '../selection.js';
import { initTags, openTagsForElement, closeTagsModal } from '../views/tags.js';

function classes() {
  const values = new Set();
  return {add: value => values.add(value), remove: value => values.delete(value),
    contains: value => values.has(value)};
}
function element(id) {
  return {dataset: {id}, classList: classes(), listeners: {}, innerHTML: '', value: '',
    addEventListener(type, fn) { this.listeners[type] = fn; },
    querySelectorAll() { return []; }, scrollIntoView() {}, matches() { return false; },
    focus() { document.activeElement = this; }};
}
function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return {promise, resolve};
}
function setup(reload) {
  const elements = Object.fromEntries(['tags-modal','tags-close','tags-new','tags-add','tags-list','media-grid']
    .map(id => [id, element(id)]));
  let cards = [element('1')];
  elements['media-grid'].querySelectorAll = () => cards;
  globalThis.document = {activeElement: null, addEventListener() {},
    getElementById: id => elements[id] || null, querySelectorAll: () => cards};
  Object.assign(state, {music: null, virtualSet: '', selectedSetIds: [10], folderPath: '', mediaPage: 0});
  state.filters.tags = 'keep'; state.filters.search = '';
  API.mediaDetail = async id => ({media: {id}, tags: [{name: 'keep'}]});
  API.addTag = async () => {};
  initTags({onFilterChange: () => reload({elements,
    replace(next) { cards = next; clearSelection(); }})});
  return {elements, get cards() { return cards; }};
}
async function start(env) {
  await openTagsForElement(env.cards[0]);
  env.elements['tags-new'].value = 'fresh';
  return {mutation: env.elements['tags-add'].listeners.click()};
}

// Completed reload restores the replacement target without stealing input focus.
{
  const gate = deferred(), replacement = element('1');
  const env = setup(async ({replace}) => { replace([replacement]); await gate.promise; });
  const {mutation} = await start(env);
  assert.equal(replacement.classList.contains('selected'), false);
  assert.equal(document.activeElement, env.elements['tags-new']);
  gate.resolve(); await mutation;
  assert.equal(replacement.classList.contains('selected'), true);
  assert.equal(document.activeElement, env.elements['tags-new']);
  closeTagsModal(); await Promise.resolve();
  assert.equal(document.activeElement, replacement);
}
// Filtered-out target falls back to grid on close, never to hidden dialog input.
{
  const env = setup(async ({replace}) => replace([]));
  const {mutation} = await start(env); await mutation;
  closeTagsModal(); await Promise.resolve();
  assert.equal(document.activeElement, env.elements['media-grid']);
}
// Late completion cannot override a new selection, navigation or reopened dialog.
for (const mode of ['close', 'navigate', 'reopen']) {
  const gate = deferred(), oldTarget = element('1'), other = element('2');
  const env = setup(async ({replace}) => { replace([oldTarget, other]); await gate.promise; });
  const {mutation} = await start(env);
  const before = env.elements['tags-list'].innerHTML;
  if (mode === 'close') { closeTagsModal(); await Promise.resolve(); }
  selectByElement(other);
  if (mode === 'reopen') {
    await openTagsForElement(other);
    env.elements['tags-new'].value = 'new dialog draft';
  }
  else if (mode === 'navigate') state.folderPath = 'another-folder';
  else other.focus();
  gate.resolve(); await mutation;
  assert.equal(oldTarget.classList.contains('selected'), false, mode);
  assert.equal(other.classList.contains('selected'), true, mode);
  assert.equal(document.activeElement, mode === 'close' ? other : env.elements['tags-new'], mode);
  if (mode === 'reopen') assert.equal(env.elements['tags-new'].value, 'new dialog draft');
  if (mode === 'close') {
    assert.equal(env.elements['tags-modal'].classList.contains('open'), false);
    assert.equal(env.elements['tags-list'].innerHTML, before);
  }
  closeTagsModal(); await Promise.resolve();
}
console.log('Tag filtered-reload focus tests passed (5 scenarios).');
