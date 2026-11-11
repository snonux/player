import { pickResumableEpisode, resumeSubtitle } from '../podcast-resume.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

console.log('Running podcast continue-listening shortcut tests...');

const ep = (key, extra = {}) => ({ id: key.length, feed_key: 'feed:a/', episode_key: `guid:${key}`, title: key, feed_title: 'Daily show', completed: false, position_seconds: 0, ...extra });

assert(pickResumableEpisode([]) === null, 'nothing started offers nothing');
assert(pickResumableEpisode(null) === null, 'a missing listing offers nothing');
assert(pickResumableEpisode([ep('fresh')]) === null, 'an episode at 0 is not offered');
assert(pickResumableEpisode([ep('done', { completed: true, position_seconds: 50 })]) === null,
  'a completed episode is not offered');
const picked = pickResumableEpisode([ep('done', { completed: true, position_seconds: 50 }), ep('latest', { position_seconds: 754 }), ep('older', { position_seconds: 3 })]);
assert(picked?.title === 'latest', 'the first unfinished started episode (most recently listened) wins');
assert(picked.position_seconds === 754, 'the saved position is kept');
assert(resumeSubtitle(picked) === 'Daily show · Resume at 12:34', `subtitle names podcast and position: ${resumeSubtitle(picked)}`);
assert(resumeSubtitle({ position_seconds: 3725 }) === 'Resume at 1:02:05', 'hours are shown and an unnamed podcast is left out');

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('podcast continue-listening shortcut tests passed');
process.exit(0);
