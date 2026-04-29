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
export function currentIndex() { return selectedIndex; }
export function currentElement() { return refreshCards()[selectedIndex] ?? null; }
