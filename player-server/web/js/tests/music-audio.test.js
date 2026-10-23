import assert from 'node:assert/strict';
import {musicRows, musicDetailGroups, musicAudioRows} from '../music.js';
const music = {bpm:123.5,sample_rate:48000,bit_depth:24,channels:2,replaygain_track_gain_db:0,replaygain_album_gain_db:-6.25,replaygain_track_peak:0.89,replaygain_album_peak:1};
assert.equal(musicRows({type:'audio',music}).find(([label]) => label === 'Tempo')[1], '123.5 BPM (beats/min)');
assert.deepEqual(musicAudioRows(music), [
 ['Sample rate','48 kHz (48000 Hz)'], ['Bit depth','24 bits'], ['Channels','2 (stereo)'],
 ['Track ReplayGain','0 dB'], ['Album ReplayGain','-6.25 dB'], ['Track peak','0.89 (1 = full scale)'], ['Album peak','1 (1 = full scale)']
]);
assert.deepEqual(musicAudioRows({bpm:NaN,sample_rate:-1,bit_depth:0,channels:1.5,replaygain_track_gain_db:Infinity,replaygain_track_peak:-1}),[]);
assert.deepEqual(musicDetailGroups({type:'audio',music:{}}),[]);
assert.deepEqual(musicDetailGroups({type:'video',music}),[]);
assert.equal(musicAudioRows({sample_rate:44100,channels:1})[0][1],'44.1 kHz (44100 Hz)');
console.log('Music audio information mapping passed');
