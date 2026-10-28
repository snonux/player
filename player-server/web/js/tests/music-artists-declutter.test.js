// qs3: artist list Filters host + detail More affordance.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const catalog = readFileSync(join(root, 'views/music-catalog.js'), 'utf8');
const artwork = readFileSync(join(root, 'music-artwork.js'), 'utf8');
const top = readFileSync(join(root, 'music-top-songs.js'), 'utf8');

assert.match(catalog, /favoriteFilter\(grid,view,reload\); ratingFilters\(grid,view,reload\)/);
assert.match(catalog, /mountMusicFacets\(grid,view,current,reload\)/);
assert.ok(catalog.indexOf('mountMusicFacets(grid,view,current,reload)') < catalog.indexOf('if (!view.artist) { favoriteFilter'), 'Filters before favorites');
assert.match(catalog, /function artistDetailMore/);
assert.match(catalog, /data-artist-more/);
assert.match(catalog, /menuOnly:true/);
assert.match(catalog, /hidePlayAll:true/);
assert.match(artwork, /options=\{\}/);
assert.match(artwork, /controls\.hidden=Boolean\(options\.menuOnly\)/);
assert.match(top, /hidePlayAll/);
console.log('music-artists-declutter source checks passed');
