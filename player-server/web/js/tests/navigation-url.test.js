import assert from 'node:assert/strict';
import { readLibraryURL, libraryURL, safeReturnTo, loginDestination } from '../navigation-url.js';
const state = {
  selectedSetIds: [6], folderPath: 'R&B/Beyoncé/Space + # ? % & /日本語',
  virtualSet: '', mediaPage: 3,
  filters: { search: 'name & tags', favorites: true, tags: 'r&b', type: 'audio', sort: 'date', minDuration: '3.5' },
};
const url = libraryURL(state, 1973, true);
const route = readLibraryURL(url.slice(1));
assert.deepEqual(route.sets, [6]);
assert.equal(route.folder, state.folderPath);
assert.equal(route.media, 1973);
assert.equal(route.filters.search, state.filters.search);
assert.equal(route.filters.favorites, true);
assert.equal(route.page, 3);
assert.equal(route.shuffle, true);
for (const bad of ['0', '-1', '1e2', '1.2', '9007199254740992', '1/stream']) assert.equal(readLibraryURL('?media=' + bad).media, null);
for (const folder of ['../secret', 'a/../secret', '/etc', 'a\\b', 'a\0b']) assert.equal(readLibraryURL('?set=1&folder=' + encodeURIComponent(folder)).folder, '');
assert.deepEqual(readLibraryURL('?set=1,2,2,invalid').sets, [1, 2]);
assert.equal(readLibraryURL('?set=1,2&folder=a').folder, '');
assert.equal(readLibraryURL('?page=90071992547409999').page, undefined);
for (const bad of ['https://evil.example', '//evil.example', '/\\evil.example', '/login.html', '/api/media/1/stream', '/%2f%2fevil', '/index.html/../login.html']) assert.equal(safeReturnTo(bad), '/');
assert.equal(safeReturnTo(url), url);
assert.equal(new URLSearchParams(loginDestination('/login.html', url).split('?')[1]).get('return_to'), url);
console.log('Deep link serialization and return destination tests passed.');
