import assert from 'node:assert/strict';
import { formatAudioQuality, initAudioFormat } from '../audioFormat.js';
import { mediaResourceUrl } from '../lyrics.js';

const base = 'https://player.test/index.html';
assert.equal(mediaResourceUrl('/api/media/7/stream', 'format', base), '/api/media/7/format');
assert.equal(mediaResourceUrl('/s/tok/compat?view=a.b&x=1', 'format', base), '/s/tok/format?view=a.b');
assert.equal(mediaResourceUrl('https://other.test/api/media/7/stream', 'format', base), null);

assert.equal(formatAudioQuality(null), '');
assert.equal(formatAudioQuality({ lossless: false, transcoded: false }), '');
assert.equal(formatAudioQuality({
  codec_label: 'FLAC', bit_depth: 24, sample_rate: 96000, bitrate: 2304000, channels: 2, lossless: true,
}), 'FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo');
assert.equal(formatAudioQuality({ codec_label: 'MP3', sample_rate: 44100, bitrate: 320000, channels: 1 }),
  'MP3 · 44.1 kHz · 320 kbps · Mono');
assert.equal(formatAudioQuality({ codec_label: 'AC-3', sample_rate: 48000, channels: 6, channel_layout: '5.1(side)' }),
  'AC-3 · 48 kHz · 5.1');
assert.equal(formatAudioQuality({ codec_label: 'WMA', channels: 4, transcoded: true }), 'WMA · 4 ch (plays as AAC)');
assert.equal(formatAudioQuality({ codec_label: 'X', bitrate: -1, sample_rate: 'bad', bit_depth: NaN }), 'X');
assert.equal(initAudioFormat({}), null);
console.log('Audio format tests passed.');
