import { selectedLibraryIds, libraryQueryParams, clearMusicLibraryCache } from '../music-libraries.js';

const store = new Map();
globalThis.sessionStorage = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => { store.set(k, String(v)); },
  removeItem: (k) => { store.delete(k); },
};

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

clearMusicLibraryCache();
sessionStorage.removeItem('player.music.library_ids');
assert(selectedLibraryIds().length === 0, 'default selection empty');
assert(Object.keys(libraryQueryParams()).length === 0, 'no library_ids when empty');

sessionStorage.setItem('player.music.library_ids', JSON.stringify([2, 5]));
assert(selectedLibraryIds().join(',') === '2,5', 'reads stored ids');
assert(libraryQueryParams().library_ids === '2,5', 'query params');

sessionStorage.setItem('player.music.library_ids', 'not-json');
assert(selectedLibraryIds().length === 0, 'bad json yields empty');

console.log('music-libraries tests passed');
