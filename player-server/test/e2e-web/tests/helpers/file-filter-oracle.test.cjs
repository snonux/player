const { test } = require('node:test');
const assert = require('node:assert/strict');
const { completedFilterResult } = require('./file-filter-oracle.cjs');
const empty = {ids: [], loading: false, error: false, emptyText: 'No results.', resultCount: '0 items'};
test('only a fresh completed filtered-empty result passes', () => {
  assert.equal(completedFilterResult(empty, [], true), true);
  for (const [state, fresh] of [
    [empty, false], // Previous query's empty DOM while the new request is pending.
    [{...empty, loading: true}, true],
    [{...empty, error: true}, true],
    [{...empty, emptyText: 'Loading...'}, true],
    [{...empty, emptyText: ''}, true],
    [{...empty, resultCount: '4 items'}, true],
    [{...empty, ids: [1]}, true],
  ]) assert.equal(completedFilterResult(state, [], fresh), false);
});
test('populated identity comparisons also reject loading and errors', () => {
  assert.equal(completedFilterResult({...empty, ids: [2, 1]}, [1, 2], true), true);
  assert.equal(completedFilterResult({...empty, ids: [2, 1], loading: true}, [1, 2], true), false);
  assert.equal(completedFilterResult({...empty, ids: [1]}, [1, 2], true), false);
});
