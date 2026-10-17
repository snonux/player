import assert from 'node:assert/strict';
import { assignShortcutKeys } from '../keyboard-access.js';
assert.deepEqual(assignShortcutKeys(['Save position', 'Bookmarks', 'Notes', 'Favorite'], ['s', 'b', 'n', 'f']), ['s', 'b', 'n', 'f']);
for (const count of [0, 5, 21, 22, 100, 1000]) {
  const keys = assignShortcutKeys(Array(count).fill('Repeated menu label'));
  assert.equal(new Set(keys).size, count);
  assert.ok(keys.every(key => /^[a-z]+$/.test(key)));
  assert.ok(keys.every(key => !keys.some(other => other !== key && other.startsWith(key))), 'sequences must be prefix-free');
  assert.ok(keys.every(key => !/[hjkl]/.test(key)));
}
console.log('Keyboard shortcut allocation tests passed.');
