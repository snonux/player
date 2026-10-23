/**
 * Media cards must not nest action buttons inside role=button: the a11y tree
 * flattens those children. Primary open and actions are siblings (yn3).
 */
import assert from 'node:assert/strict';

globalThis.window = { addEventListener() {}, open() {} };
globalThis.location = { pathname: '/index.html', href: '' };

const grid = {
  classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
  innerHTML: '',
  textContent: '',
  addEventListener() {},
  querySelectorAll() { return []; },
  querySelector() { return null; },
};
const emptyHint = { ...grid };
const breadcrumb = { ...grid };
const resultCount = { ...grid, textContent: '' };

globalThis.document = {
  getElementById(id) {
    if (id === 'media-grid') return grid;
    if (id === 'empty-hint') return emptyHint;
    if (id === 'breadcrumb-bar') return breadcrumb;
    if (id === 'result-count') return resultCount;
    return null;
  },
  querySelectorAll() { return []; },
  addEventListener() {},
  dispatchEvent() { return true; },
};

globalThis.fetch = async (url) => {
  if (String(url) === '/api/in-progress') {
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => ([{
        id: 3,
        file_name: 'track.flac',
        type: 'audio',
        duration: 12,
        file_size_bytes: 1024,
        music: { title: 'First Light', artist: 'Aurora' },
      }]),
    };
  }
  return { ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => ({ status: 'ok' }) };
};

const { state } = await import('../state.js');
const { initMediaGrid, loadMedia } = await import('../views/media-grid.js');

state.virtualSet = 'in-progress';
state.selectedSetId = null;
state.selectedSetIds = [];
state.folderPath = '';
state.mediaPage = 0;
initMediaGrid();
await loadMedia();

assert.match(grid.innerHTML, /role="group"/, 'card container is a group');
assert.doesNotMatch(grid.innerHTML, /class="media-card[^"]*"[^>]*role="button"/, 'card is not a button');
assert.match(grid.innerHTML, /<button type="button" class="media-open" aria-label="First Light"><\/button>/, 'primary open is a sibling button');
assert.match(grid.innerHTML, /data-action="play"[^>]*aria-label="Play"/, 'Play is labelled for AT');
assert.match(grid.innerHTML, /data-action="favorite"[^>]*aria-label="Favorite"/, 'Favorite is labelled for AT');
assert.match(grid.innerHTML, /data-action="notes"[^>]*aria-label="Notes"/, 'Notes is labelled for AT');
assert.ok(
  grid.innerHTML.indexOf('class="media-open"') < grid.innerHTML.indexOf('data-action="play"'),
  'open control and play action are siblings in card markup',
);
const playAt = grid.innerHTML.indexOf('data-action="play"');
const beforePlay = grid.innerHTML.slice(0, playAt);
assert.equal((beforePlay.match(/role="button"/g) || []).length, 0, 'no role=button ancestor before play action');

console.log('Media card a11y nesting tests passed.');
