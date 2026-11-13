let selectedIndex = -1;

export function initSelection() {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  grid.addEventListener('click', (e) => {
    const card = e.target.closest('.media-card, .media-row');
    if (!card) return;
    selectByElement(card);
  });
  grid.addEventListener('focusin', (e) => {
    const card = e.target.closest('.media-card, .media-row');
    if (card) selectByElement(card);
  });
}

// Moves keyboard focus to the selected card. Set, folder and media cards are
// not focusable themselves; their primary open button is.
export function focusSelectedCard() {
  const el = currentElement();
  const nested = typeof el?.querySelector === 'function'
    ? el.querySelector('.set-open, .folder-open, .media-open, .collection-open')
    : null;
  const target = nested || el;
  target?.focus?.();
  return Boolean(target);
}

export function clearSelection() {
  selectedIndex = -1;
  refreshCards().forEach((c) => c.classList.remove('selected'));
}

function refreshCards() {
  return Array.from(document.querySelectorAll('.media-card, .media-row, .folder-card, .set-card'));
}

function syncedIndex(cards) {
  const domIndex = cards.findIndex((c) => c.classList.contains('selected'));
  if (domIndex >= 0) {
    selectedIndex = domIndex;
    return domIndex;
  }
  selectedIndex = -1;
  return -1;
}

// Compute grid geometry from the rendered cards.
function gridGeometry(cards) {
  const rows = [];
  const rects = cards.map(card => card.getBoundingClientRect());
  const rowMap = rects.map((rect, index) => {
    let row = rows.length - 1;
    if (row < 0 || Math.abs(rect.top - rects[rows[row][0]].top) > 1) {
      rows.push([]);
      row++;
    }
    rows[row].push(index);
    return row;
  });
  return { rows, rowMap, rects };
}

export function select(index) {
  const cards = refreshCards();
  if (!cards.length) { selectedIndex = -1; return; }
  selectedIndex = Math.max(0, Math.min(index, cards.length - 1));
  cards.forEach((c) => c.classList.remove('selected'));
  const el = cards[selectedIndex];
  if (el) {
    el.classList.add('selected');
    el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }
}

export function selectByElement(el) {
  const cards = refreshCards();
  const idx = cards.indexOf(el);
  if (idx >= 0) select(idx);
}

export function next(step = 1) {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(step >= 0 ? 0 : cards.length - 1);
    return;
  }
  select((idx + step + cards.length) % cards.length);
}

export function prev(step = 1) { next(-step); }

// Grid-aware keyboard navigation
export function navLeft() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(cards.length - 1);
    return;
  }
  if (idx > 0) select(idx - 1);
}

export function navRight() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(0);
    return;
  }
  if (idx < cards.length - 1) select(idx + 1);
}

function verticalTarget(cards, idx, delta) {
  const { rows, rowMap, rects } = gridGeometry(cards);
  const row = rows[rowMap[idx] + delta];
  if (!row) return idx;
  const center = rects[idx].left + rects[idx].width / 2;
  return row.reduce((nearest, candidate) => {
    const distance = i => Math.abs(rects[i].left + rects[i].width / 2 - center);
    return distance(candidate) < distance(nearest) ? candidate : nearest;
  }, row[0]);
}

export function navUp() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(cards.length - 1);
    return;
  }
  select(verticalTarget(cards, idx, -1));
}

export function navDown() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(0);
    return;
  }
  select(verticalTarget(cards, idx, 1));
}

export function currentIndex() { return syncedIndex(refreshCards()); }
export function currentElement() {
  const cards = refreshCards();
  const idx = syncedIndex(cards);
  return idx >= 0 ? cards[idx] : null;
}
