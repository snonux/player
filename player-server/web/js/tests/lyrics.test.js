import assert from 'node:assert/strict';
import { lyricsUrl, normalizeLyrics, currentLyricIndex, initLyrics } from '../lyrics.js';
const base = 'https://player.test/detach.html';
assert.equal(lyricsUrl('/api/media/12/stream', base), '/api/media/12/lyrics');
assert.equal(lyricsUrl('/api/v1/media/12/compat?unused=x', base), '/api/v1/media/12/lyrics');
assert.equal(lyricsUrl('/s/token/compat?view=abc.def&unused=x', base), '/s/token/lyrics?view=abc.def');
for (const url of ['/music.mp3', '/s/token', '/api/media/12/thumbnail', 'https://other.test/s/token/stream', 'http://[']) {
  assert.equal(lyricsUrl(url, base), null);
}
assert.deepEqual(normalizeLyrics(null).lines, []);
assert.deepEqual(normalizeLyrics({ lines: [{ text: ' \n' }] }).lines, []);
assert.deepEqual(normalizeLyrics({ lines: [null, {}, { text: '<b>literal</b>' }] }).lines, [{ text: '<b>literal</b>', start: undefined }]);
const lines = [{ start: 1, text: 'one' }, { start: 3, text: 'two' }, { start: 3, text: 'three' }];
assert.equal(normalizeLyrics({ synced: true, lines }).synced, true);
for (const times of [[1, 0], [1, NaN], [1, -1], [1, '3'], [1, undefined]]) {
  assert.equal(normalizeLyrics({ synced: true, lines: times.map(start => ({ start, text: 'line' })) }).synced, false);
}
for (const [time, expected] of [[0, -1], [1, 0], [2.99, 0], [3, 2], [200, 2], [NaN, -1]]) {
  assert.equal(currentLyricIndex(lines, time), expected);
}
assert.equal(currentLyricIndex([], 2), -1);
assert.equal(initLyrics({}), null);
console.log('Lyrics formatting, endpoint and current line tests passed.');
