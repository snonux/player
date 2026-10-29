import {test} from 'node:test';
import assert from 'node:assert/strict';
import {normalizeSetSort, readSetSort} from '../set-sort.js';
import {state} from '../state.js';

test('sort values validate fields and directions',()=>{
  for (const field of ['name','date','play_count','size','duration','date_taken']) {
    for (const direction of ['asc','desc']) assert.equal(normalizeSetSort(`${field}_${direction}`),`${field}_${direction}`);
  }
  assert.equal(normalizeSetSort('random'),'random');
  for (const bad of [null,'size;DROP TABLE media','date_sideways']) assert.equal(normalizeSetSort(bad),'name_asc');
});
test('set preferences remain scoped to user and set',()=>{
  const values = new Map([['player-set-sort:3:9','size_desc'],['player-set-sort:3:10','date_asc']]);
  globalThis.localStorage = {getItem:key=>values.get(key)};
  state.user={id:3};
  assert.equal(readSetSort(9),'size_desc');
  assert.equal(readSetSort(10),'date_asc');
  state.user={id:4};
  assert.equal(readSetSort(9),'name_asc');
  state.user=null;
  assert.equal(readSetSort(9),'name_asc');
});
