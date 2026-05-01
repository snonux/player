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

function refreshCards() {
  return Array.from(document.querySelectorAll('.media-card, .media-row'));
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
  select((selectedIndex + step + cards.length) % cards.length);
}

export function prev(step = 1) { next(-step); }

// Grid-aware keyboard navigation
export function navLeft() {
  const cards = refreshCards();
  if (!cards.length) return;
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const row = Math.floor(selectedIndex / cols);
  const col = selectedIndex % cols;
  if (col > 0) {
    select(selectedIndex - 1);
  } else if (row > 0) {
    // wrap to end of previous row
    select(row * cols - 1);
  }
}

export function navRight() {
  const cards = refreshCards();
  if (!cards.length) return;
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const row = Math.floor(selectedIndex / cols);
  const col = selectedIndex % cols;
  const lastInRow = Math.min((row + 1) * cols, cards.length) - 1;
  if (col < (lastInRow - row * cols)) {
    select(selectedIndex + 1);
  } else if (selectedIndex < cards.length - 1) {
    // wrap to start of next row
    select(Math.min((row + 1) * cols, cards.length - 1));
  }
}

export function navUp() {
  const cards = refreshCards();
  if (!cards.length) return;
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const idxAbove = selectedIndex - cols;
  if (idxAbove >= 0) {
    select(idxAbove);
  }
}

export function navDown() {
  const cards = refreshCards();
  if (!cards.length) return;
  const { cols } = gridGeometry(cards);
  if (cols === 0) return;
  const idxBelow = selectedIndex + cols;
  if (idxBelow < cards.length) {
    select(idxBelow);
  }
}

export function currentIndex() { return selectedIndex; }
export function currentElement() { return refreshCards()[selectedIndex] ?? null; }
