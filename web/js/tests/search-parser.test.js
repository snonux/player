import { parseQuery } from '../search.js';

const failures = [];

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

function testSetFilter() {
  const parsed = parseQuery('set:yoga');
  assert(parsed.set === 'yoga', 'set token should parse set name');
  assert(parsed.search === '', 'set token should not become text search');
}

function testQuotedSetFilter() {
  const parsed = parseQuery('set:"Yoga Flow" type:video morning');
  assert(parsed.set === 'Yoga Flow', 'quoted set token should preserve spaces');
  assert(parsed.type === 'video', 'other filters should still parse');
  assert(parsed.search === 'morning', 'free text should still parse');
}

console.log('Running search parser tests...');
testSetFilter();
testQuotedSetFilter();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All search parser tests passed.');
  process.exit(0);
}
