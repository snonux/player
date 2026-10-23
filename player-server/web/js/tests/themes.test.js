import assert from 'node:assert/strict';
import fs from 'node:fs';
const attrs = {};
const events = {};
const elements = new Map();
for (const name of ['mode', 'accent', 'shape', 'fit']) elements.set(`appearance-${name}`, { value: '', addEventListener(type, fn) { this[type] = fn; } });
elements.set('appearance-status', {});
elements.set('theme-toggle', { addEventListener() {} });
const stored = new Map([['kiss-theme', 'system'], ['kiss-art-accent', 'invalid']]);
globalThis.localStorage = { getItem: (key) => stored.get(key), setItem: (key, value) => stored.set(key, value) };
globalThis.document = { documentElement: { setAttribute: (key, value) => attrs[key] = value, getAttribute: (key) => attrs[key] }, getElementById: (key) => elements.get(key) };
const media = { matches: false, addEventListener(type, fn) { this[type] = fn; } };
globalThis.window = { matchMedia: () => media, addEventListener: (type, fn) => events[type] = fn };
const themes = await import('../themes.js');
themes.initThemes();
assert.equal(attrs['data-theme'], 'light');
assert.equal(attrs['data-accent'], 'blue');
media.matches = true; media.change();
assert.equal(attrs['data-theme'], 'dark');
elements.get('appearance-accent').change({target: {value: 'teal'}});
assert.equal(attrs['data-accent'], 'teal');
assert.equal(stored.get('kiss-art-accent'), 'teal');
stored.set('kiss-art-shape', 'circle'); events.storage({key: 'kiss-art-shape'});
assert.equal(attrs['data-shape'], 'circle', 'other windows update live');
themes.toggle();
assert.equal(stored.get('kiss-theme'), 'light');
localStorage.setItem = () => { throw new Error('Storage denied'); };
themes.apply('dark');
assert.equal(attrs['data-theme'], 'dark');
assert.match(elements.get('appearance-status').textContent, /storage is unavailable/);

// Normal text on accent fills, and accent labels on every unchanged surface.
const css = fs.readFileSync(new URL('../../css/theme.css', import.meta.url), 'utf8');
function luminance(hex) {
  const rgb = hex.match(/[a-f0-9]{2}/gi).map((v) => parseInt(v,16) / 255).map((v) => v <= .04045 ? v / 12.92 : ((v+.055)/1.055)**2.4);
  return rgb[0]*.2126 + rgb[1]*.7152 + rgb[2]*.0722;
}
function contrast(a,b) { const values = [luminance(a),luminance(b)].sort((a,b)=>b-a); return (values[0]+.05)/(values[1]+.05); }
for (const [mode,colors,text,surfaces] of [
  ['dark',['5e9eff','7cb2ff','3d8bf7','55d6c2','78e3d3','c6a0ff','d6bcff'],'0b0d12',['0f1117','161920','1b1f27','1e222c']],
  ['light',['2b6cb0','1a5490','17447a','08776a','065f55','054f47','7541a8','60328d','502976'],'ffffff',['f4f5f8','ffffff','f0f2f7']],
]) for (const color of colors) {
  assert(css.includes(`#${color}`));
  assert(contrast(color,text) >= 4.5, `${mode} ${color} filled text`);
  for (const surface of surfaces) assert(contrast(color,surface) >= 4.5, `${mode} ${color} label on ${surface}`);
}
console.log('Theme persistence, system/cross-window changes, unavailable storage and alternative accent contrast passed.');
