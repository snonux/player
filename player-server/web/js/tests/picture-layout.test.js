import assert from 'node:assert/strict';
import { pictureRatio, pictureRows } from '../views/picture-layout.js';
import { select, currentIndex, navDown, navUp, navLeft, navRight } from '../selection.js';

assert.equal(pictureRatio(800, 400), 2);
assert.equal(pictureRatio(400, 800), 0.5);
for (const invalid of [0, -1, undefined, null, NaN, Infinity, '800']) {
  assert.equal(pictureRatio(invalid, 400), 1);
  assert.equal(pictureRatio(400, invalid), 1);
}
assert.equal(pictureRatio(1, 10000), 0.5);
assert.equal(pictureRatio(10000, 1), 3);

for (const width of [288, 640, 1100]) {
  const ratios = [2, 0.5, 1, 3, 1, 0.5, 2, 1];
  const rows = pictureRows(ratios, width);
  assert.equal(rows[0].start, 0);
  assert.equal(rows.at(-1).end, ratios.length);
  rows.forEach((row, i) => {
    if (i) assert.equal(row.start, rows[i - 1].end, 'rows preserve every card in order');
    const used = row.widths.reduce((sum, w) => sum + w, 0) + (row.widths.length - 1) * 12;
    assert.ok(used <= width + 0.00001, 'rows must fit the viewport');
    row.widths.forEach((w, j) => {
      assert.ok(w > 2);
      assert.ok(Math.abs((w - 2) / row.height - ratios[row.start + j]) < 0.00001);
    });
  });
  assert.ok(rows.at(-1).height <= 180, 'short last rows are not enlarged');
}
assert.deepEqual(pictureRows([], 640), []);
assert.deepEqual(pictureRows([1], 0), []);

let rects = [
  { left: 0, top: 0, width: 200 }, { left: 212, top: 0, width: 100 }, { left: 324, top: 0, width: 300 },
  { left: 0, top: 250, width: 400 }, { left: 412, top: 250, width: 212 },
  { left: 0, top: 500, width: 200 },
];
const cards = rects.map((_, index) => {
  const classes = new Set();
  return {
    classList: { add: cls => classes.add(cls), remove: cls => classes.delete(cls), contains: cls => classes.has(cls) },
    getBoundingClientRect: () => rects[index], scrollIntoView() {},
  };
});
globalThis.document = { querySelectorAll: () => cards };
select(2); navDown(); assert.equal(currentIndex(), 4, 'down chooses nearest center in the actual next row');
navDown(); assert.equal(currentIndex(), 5, 'short last row remains reachable');
navDown(); assert.equal(currentIndex(), 5, 'down stays at bottom edge');
navUp(); assert.equal(currentIndex(), 3); navUp(); assert.equal(currentIndex(), 1);
navUp(); assert.equal(currentIndex(), 1, 'up stays at top edge');
select(2); navRight(); assert.equal(currentIndex(), 3, 'right follows DOM order across row boundaries');
navLeft(); assert.equal(currentIndex(), 2);
// Responsive reflow: selection uses the current rectangles rather than stale columns.
rects = rects.map((r, i) => ({ ...r, left: 0, top: i * 250, width: 288 }));
select(2); navDown(); assert.equal(currentIndex(), 3); navUp(); assert.equal(currentIndex(), 2);
console.log('Picture layout and responsive selection tests passed.');
