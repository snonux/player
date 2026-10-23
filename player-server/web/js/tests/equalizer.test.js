import assert from 'node:assert/strict';
import { equalizerSettings, effectiveBands, presets } from '../equalizer.js';
assert.deepEqual(equalizerSettings(null), { enabled: false, bands: [0,0,0,0,0] });
assert.deepEqual(equalizerSettings({ enabled: true, bands: [-100,100,NaN,'3'] }), { enabled: true, bands: [-12,12,0,0,0] });
assert.deepEqual(effectiveBands({ enabled: false, bands: presets.Voice }), [0,0,0,0,0]);
assert.deepEqual(effectiveBands({ enabled: true, bands: presets['Bass boost'] }), [0,-3,-6,-8,-10]);
assert.deepEqual(effectiveBands({ enabled: true, bands: [-3,-2,-1,-2,-3] }), [-3,-2,-1,-2,-3]);
for (const bands of Object.values(presets)) assert(effectiveBands({ enabled: true, bands }).every(n => n <= 0));
console.log('Equalizer validation, bypass and clipping headroom passed.');
