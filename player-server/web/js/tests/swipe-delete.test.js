import assert from 'node:assert/strict';
import { bindSwipeDelete } from '../swipe-delete.js';

function row(width = 400) {
  const listeners = {};
  const classes = new Set();
  return {
    dataset: {}, style: {}, captured: null,
    classList: { add: c => classes.add(c), remove: c => classes.delete(c), contains: c => classes.has(c) },
    addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
    getBoundingClientRect: () => ({ width }),
    setPointerCapture(id) { this.captured = id; },
    fire(type, init = {}) {
      const event = { type, isTrusted: true, pointerType: 'touch', isPrimary: true, pointerId: 1, clientX: 0, clientY: 0,
        prevented: false, stopped: false, preventDefault() { this.prevented = true; }, stopPropagation() { this.stopped = true; }, ...init };
      for (const fn of listeners[type] || []) fn(event);
      return event;
    },
  };
}

function swipe(r, dx, { dy = 0, pointerType = 'touch', end = 'pointerup' } = {}) {
  r.fire('pointerdown', { clientX: 300, clientY: 100, pointerType });
  r.fire('pointermove', { clientX: 300 + dx / 2, clientY: 100 + dy / 2, pointerType });
  r.fire('pointermove', { clientX: 300 + dx, clientY: 100 + dy, pointerType });
  r.fire(end, { clientX: 300 + dx, clientY: 100 + dy, pointerType });
}

// A long right-to-left swipe deletes once; the row springs back.
{
  const r = row(); let deletes = 0;
  bindSwipeDelete(r, () => deletes++);
  assert.equal(r.classList.contains('swipe-delete'), true);
  swipe(r, -200);
  assert.equal(deletes, 1);
  assert.equal(r.style.transform, '');
  assert.equal(r.classList.contains('swipe-delete-active'), false);
  // The click that follows the gesture neither plays nor opens the row.
  const click = r.fire('click');
  assert.equal(click.prevented, true);
  assert.equal(click.stopped, true);
  // A scripted click (the row's own Delete action) is never dropped.
  swipe(r, -200);
  assert.equal(r.fire('click', { isTrusted: false }).prevented, false);
}

// Left-to-right, short, vertical, cancelled and mouse drags never delete.
{
  const r = row(); let deletes = 0;
  bindSwipeDelete(r, () => deletes++);
  swipe(r, 200);
  swipe(r, -60);
  swipe(r, -200, { dy: 300 });
  swipe(r, -200, { end: 'pointercancel' });
  swipe(r, -200, { pointerType: 'mouse' });
  assert.equal(deletes, 0);
}

// The distance is 35% of the row, at least 72px and at most 160px.
{
  const narrow = row(300), wide = row(1200); let deletes = 0;
  bindSwipeDelete(narrow, () => deletes++);
  bindSwipeDelete(wide, () => deletes++);
  swipe(narrow, -100); swipe(wide, -150);
  assert.equal(deletes, 0);
  swipe(narrow, -110); swipe(wide, -170);
  assert.equal(deletes, 2);
}

// Disabled rows (grid layout, selection mode) ignore the gesture, and
// binding twice does not delete twice.
{
  const r = row(); let deletes = 0, enabled = false;
  bindSwipeDelete(r, () => deletes++, { enabled: () => enabled });
  bindSwipeDelete(r, () => deletes++);
  swipe(r, -200);
  assert.equal(deletes, 0);
  enabled = true;
  swipe(r, -200);
  assert.equal(deletes, 1);
}

console.log('swipe-delete tests passed');
