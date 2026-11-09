// Swiping a touch row from right to left asks to delete it, like Android's
// episode list. The row follows the finger, then springs back: onDelete runs
// the usual confirmation and the list removes the row once it is deleted.
// Mouse and pen keep their normal behavior; the visible Delete action stays.
// Share of the row width, kept between a deliberate and a comfortable reach.
const THRESHOLD = 0.35;
const MIN_DISTANCE = 72, MAX_DISTANCE = 160; // CSS pixels

export function bindSwipeDelete(row, onDelete, { enabled = () => true } = {}) {
  if (!row?.addEventListener || !row.style || row.dataset?.swipeDelete) return;
  row.dataset.swipeDelete = 'true';
  // pan-y (in CSS) keeps vertical scrolling native and gives us horizontal moves.
  row.classList?.add('swipe-delete');
  let start = null, dx = 0, horizontal = null, swiped = false;

  const reset = () => {
    row.style.transform = '';
    row.classList.remove('swipe-delete-active');
    start = null; dx = 0; horizontal = null;
  };
  row.addEventListener('pointerdown', event => {
    if (event.pointerType !== 'touch' || !event.isPrimary || !enabled()) return;
    start = { x: event.clientX, y: event.clientY, id: event.pointerId };
    dx = 0; horizontal = null;
  });
  row.addEventListener('pointermove', event => {
    if (!start || event.pointerId !== start.id) return;
    const moveX = event.clientX - start.x, moveY = event.clientY - start.y;
    if (horizontal === null && Math.hypot(moveX, moveY) > 10) {
      // Vertical scrolling wins; only a mostly horizontal leftward drag swipes.
      horizontal = Math.abs(moveX) > Math.abs(moveY) * 1.5 && moveX < 0;
      if (!horizontal) { reset(); return; }
      row.classList.add('swipe-delete-active');
      try { row.setPointerCapture?.(event.pointerId); } catch {}
    }
    if (!horizontal) return;
    dx = Math.min(0, moveX);
    row.style.transform = `translateX(${dx}px)`;
  });
  const finish = event => {
    if (!start || event.pointerId !== start.id) return;
    const width = row.getBoundingClientRect().width || 0;
    const delete_ = horizontal && event.type === 'pointerup' && -dx >= Math.min(MAX_DISTANCE, Math.max(MIN_DISTANCE, width * THRESHOLD));
    const moved = Boolean(horizontal);
    reset();
    if (delete_ && enabled()) onDelete();
    if (moved) { swiped = true; setTimeout(() => { swiped = false; }, 400); }
  };
  row.addEventListener('pointerup', finish);
  row.addEventListener('pointercancel', finish);
  // A swipe is not a tap: keep it from also opening or playing the row. Only
  // the browser's own click is dropped; onDelete may click the Delete action.
  row.addEventListener('click', event => {
    if (!swiped || !event.isTrusted) return;
    swiped = false;
    event.preventDefault();
    event.stopPropagation();
  }, true);
}
