import assert from 'node:assert/strict';
import { API } from '../api.js';
import { savedResumePosition } from '../resume-position.js';
import { setSyncAccount, syncAccount } from '../sync-account.js';

// A library item without an explicit position resumes from saved progress;
// a finished item starts over, and a slow or failing lookup keeps the start.
const oldDetail = API.mediaDetail;
try {
  API.mediaDetail = async () => ({ progress: { position_seconds: 42 } });
  assert.equal(await savedResumePosition({ id: 1 }, 0), 42);
  assert.equal(await savedResumePosition({ id: 1 }, 7), 7, 'an explicit position wins');
  assert.equal(await savedResumePosition({ id: -1 }, 0), 0, 'items without a server id are not looked up');
  API.mediaDetail = async () => ({ progress: { position_seconds: 42, finished: true } });
  assert.equal(await savedResumePosition({ id: 1 }, 0), 0, 'finished items start over');
  API.mediaDetail = async () => { throw new Error('offline'); };
  assert.equal(await savedResumePosition({ id: 1 }, 0), 0);
  API.mediaDetail = () => new Promise(() => {});
  assert.equal(await savedResumePosition({ id: 1 }, 0, 10), 0, 'a hanging lookup is bounded');
} finally { API.mediaDetail = oldDetail; }

// The sync account accepts only positive ids and announces each change once.
const events = [];
globalThis.document = { dispatchEvent: event => events.push(event.type) };
setSyncAccount(5); setSyncAccount(5); setSyncAccount(-1);
assert.equal(syncAccount(), null);
assert.deepEqual(events, ['sync:account-changed', 'sync:account-changed']);
console.log('Saved resume position and sync account checks passed.');
