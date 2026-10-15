import assert from 'node:assert/strict';
import { isAudiobook, VISUAL_MODES, initAudioVisuals } from '../audioVisuals.js';

assert.equal(VISUAL_MODES.length, 6);
assert.equal(new Set(VISUAL_MODES).size, 6);
for (const media of [
  { file_name: 'Book.M4B' }, { rel_path: 'Audiobooks/Author/01.mp3' },
  { file_name: 'An audio-book chapter.mp3' },
]) assert.equal(isAudiobook(media), true);
assert.equal(isAudiobook({ file_name: '01.mp3' }, 'Audio Books'), true);
for (const media of [null, {}, { file_name: 'long mix.mp3', duration: 60000 },
  { file_name: 'song.m4a' }, { rel_path: 'notanaudiobook/song.mp3' },
]) assert.equal(isAudiobook(media), false);
assert.equal(initAudioVisuals({}), null, 'missing player markup preserves native playback');
console.log('Audio visual classification tests passed.');
