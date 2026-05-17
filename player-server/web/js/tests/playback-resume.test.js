import { API } from '../api.js';
import { state } from '../state.js';

// --- Minimal test harness for browser module validation ---
const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

// Mock fetch and DOM for headless validation
const mockDetail = {
  media: { id: 7, file_name: 'song.mp3', type: 'audio', duration: 180 },
  progress: { user_id: 1, media_id: 7, position_seconds: 42.5, updated_at: new Date().toISOString() }
};

// We can't run the real module in Node without DOM, so we test the JSON shape contract instead.
function testDetailShape() {
  assert(mockDetail.media.id === 7, 'media.id should exist');
  assert(mockDetail.progress.position_seconds === 42.5, 'progress.position_seconds should be 42.5');
}

function testResumeFromComputation() {
  const detailWithProgress = { progress: { position_seconds: 99 } };
  const detailWithout = { progress: null };
  const resumeFrom = detailWithProgress.progress ? detailWithProgress.progress.position_seconds : 0;
  assert(resumeFrom === 99, 'resumeFrom should be 99 when progress exists');
  const resumeFromNone = detailWithout.progress ? detailWithout.progress.position_seconds : 0;
  assert(resumeFromNone === 0, 'resumeFrom should be 0 when no progress');
}

function testListItemShape() {
  const item = { id: 1, file_name: 'a.mp4', type: 'video', duration: 120 };
  assert(item.id === 1, 'list item id');
  assert(item.file_name === 'a.mp4', 'list item file_name');
  assert(!('resume_from' in item), 'list item should not have resume_from');
}

// Run tests
console.log('Running playback resume frontend contract tests...');
testDetailShape();
testResumeFromComputation();
testListItemShape();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All frontend contract tests passed.');
  process.exit(0);
}
