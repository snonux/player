import assert from 'node:assert/strict';
import { normalizeNavigation, coldLanding, readNavigationPreferences } from '../navigation-preferences.js';
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
console.log('Navigation preference fallbacks and explicit/history entry boundaries passed.');
