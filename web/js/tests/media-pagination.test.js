import { paginateItems, setMediaPageSize } from '../views/media-grid.js';
import { state } from '../state.js';

const failures = [];

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

function items(count) {
  return Array.from({ length: count }, (_, i) => i + 1);
}

function testDefaultPageSize() {
  setMediaPageSize(undefined);
  const first = paginateItems(items(101), 0);
  assert(first.items.length === 100, 'default first page should contain 100 items');
  assert(first.hasNext, 'default first page should have next');
  assert(!first.hasPrev, 'default first page should not have previous');

  const second = paginateItems(items(101), 1);
  assert(second.items.length === 1, 'default second page should contain remaining item');
  assert(second.hasPrev, 'default second page should have previous');
  assert(!second.hasNext, 'default second page should not have next');
}

function testConfiguredPageSize() {
  state.mediaPage = 2;
  setMediaPageSize(25);
  const page = paginateItems(items(60), 1);
  assert(state.mediaPage === 0, 'changing page size should reset current media page');
  assert(page.items.length === 25, 'configured page should contain configured number of items');
  assert(page.start === 25, 'configured second page should start at item offset 25');
  assert(page.end === 50, 'configured second page should end at item offset 50');
}

function testClampsOutOfRangePage() {
  setMediaPageSize(25);
  const page = paginateItems(items(60), 99);
  assert(page.page === 2, 'page should clamp to the last page');
  assert(page.items.length === 10, 'last page should contain remaining items');
}

console.log('Running media pagination tests...');
testDefaultPageSize();
testConfiguredPageSize();
testClampsOutOfRangePage();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All media pagination tests passed.');
  process.exit(0);
}
