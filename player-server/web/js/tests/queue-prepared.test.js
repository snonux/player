import assert from 'node:assert/strict';
import { preparedQueuePlan } from '../queue-prepared.js';
const entries = [1, 2, 3].map(id => ({ id, media: { id: id === 3 ? 2 : id } }));
const state = { entries, index: 0, scope: 'origin/account-1', revision: 1, repeat: 'off' };
const first = preparedQueuePlan(state);
assert.equal(first.media.id, 2); assert.equal(first.target, 1);
for (const edited of [
  { ...state, entries: [entries[0], entries[2], entries[1]] },
  { ...state, scope: 'origin/account-2' }, { ...state, revision: 2 },
  { ...state, repeat: 'one' }, { ...state, index: 1 },
]) assert.notEqual(preparedQueuePlan(edited)?.token, first.token, 'stale successor/owner/repeat/source cannot commit');
assert.equal(preparedQueuePlan({ ...state, entries: entries.slice(0, 2) }).token, first.token, 'unrelated tail edit preserves prepared occurrence');
assert.equal(preparedQueuePlan({ ...state, index: 2 }), null);
assert.equal(preparedQueuePlan({ ...state, index: 2, repeat: 'all' }).target, 0);
assert.equal(preparedQueuePlan({ ...state, repeat: 'one' }).target, 0);
console.log('Prepared queue occurrence, account, intent, repeat and edit checks passed.');
