let selectedIndex = -1;

export function initSelection() {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  grid.addEventListener('click', (e) => {
    const card = e.target.closest('.media-card, .media-row');
    if (!card) return;
    selectByElement(card);
  });
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
  if (!cards.length) return { cols: 0, rowMap: [] };
  const first = cards[0].getBoundingClientRect();
  let colCount = 0;
  for (let i = 0; i < cards.length; i++) {
    const r = cards[i].getBoundingClientRect();
    if (Math.round(r.top) !== Math.round(first.top)) break;
    colCount++;
  }
  // rowMap[i] = row index of cards[i]
  const rowMap = cards.map((_, i) => Math.floor(i / colCount));
  return { cols: colCount || 1, rowMap };
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
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const row = Math.floor(idx / cols);
  const col = idx % cols;
  if (col > 0) {
    select(idx - 1);
  } else if (row > 0) {
    // wrap to end of previous row
    select(row * cols - 1);
  }
}

export function navRight() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(0);
    return;
  }
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const row = Math.floor(idx / cols);
  const col = idx % cols;
  const lastInRow = Math.min((row + 1) * cols, cards.length) - 1;
  if (col < (lastInRow - row * cols)) {
    select(idx + 1);
  } else if (idx < cards.length - 1) {
    // wrap to start of next row
    select(Math.min((row + 1) * cols, cards.length - 1));
  }
}

export function navUp() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(cards.length - 1);
    return;
  }
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const idxAbove = idx - cols;
  if (idxAbove >= 0) {
    select(idxAbove);
  }
}

export function navDown() {
  const cards = refreshCards();
  if (!cards.length) return;
  const idx = syncedIndex(cards);
  if (idx < 0) {
    select(0);
    return;
  }
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const idxBelow = idx + cols;
  if (idxBelow < cards.length) {
    select(idxBelow);
  }
}

export function currentIndex() { return syncedIndex(refreshCards()); }
export function currentElement() {
  const cards = refreshCards();
  const idx = syncedIndex(cards);
  return idx >= 0 ? cards[idx] : null;
}
