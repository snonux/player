import assert from 'node:assert/strict';
import { API } from '../api.js';
import { bookmarkRows, bookmarkTime } from '../bookmarks.js';

assert.equal(bookmarkTime(0), '0:00');
assert.equal(bookmarkTime(3661.9), '1:01:01');
const row = { id: 2, media_id: 7, position_seconds: 12.5, label: '<chapter>' };
assert.deepEqual(bookmarkRows([row, { ...row, id: 1 }], 7).map(value => value.id), [1, 2]);
assert.deepEqual(bookmarkRows([], 7), []);
for (const value of [null, {}, [null], [{ ...row, media_id: 8 }], [{ ...row, id: -1 }],
  [{ ...row, position_seconds: NaN }], [{ ...row, position_seconds: -1 }], [{ ...row, label: null }]]) {
  assert.throws(() => bookmarkRows(value, 7));
}
const requests = [];
globalThis.location = { pathname: '/', href: '' };
globalThis.fetch = async (url, options) => {
  requests.push({ url, ...options });
  if (options.method === 'DELETE') return new Response(null, { status: 204 });
  return new Response(JSON.stringify(options.method === 'POST' ? row : []), {
    status: options.method === 'POST' ? 201 : 200, headers: { 'content-type': 'application/json' },
  });
};
assert.deepEqual(await API.bookmarks(7), []);
assert.deepEqual(await API.saveBookmark(7, 12.5, '<chapter>'), row);
assert.equal(await API.deleteBookmark(7, 2), null);
assert.deepEqual(requests.map(request => [request.url, request.method, request.credentials]), [
  ['/api/media/7/bookmarks', 'GET', 'include'],
  ['/api/media/7/bookmarks', 'POST', 'include'],
  ['/api/media/7/bookmarks/2', 'DELETE', 'include'],
]);
assert.deepEqual(JSON.parse(requests[1].body), { position_seconds: 12.5, label: '<chapter>' });
globalThis.fetch = async () => new Response('{"error":"unavailable"}', {
  status: 500, headers: { 'content-type': 'application/json' },
});
await assert.rejects(API.bookmarks(7), /unavailable/);
await assert.rejects(API.saveBookmark(7, 0), /unavailable/);
await assert.rejects(API.deleteBookmark(7, 2), /unavailable/);
console.log('Bookmark response validation, ordering and authenticated API tests passed.');
