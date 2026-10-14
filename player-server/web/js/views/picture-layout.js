const STORAGE_KEY = 'kiss-picture-layout';

// Bound extreme panoramas/portraits so filenames and actions remain usable.
// object-fit: contain still shows the whole image, including these outliers.
export function pictureRatio(width, height) {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return 1;
  return Math.max(0.5, Math.min(3, width / height));
}

// Ordered, justified rows: no masonry reordering and no enlarged last row.
export function pictureRows(ratios, width, gap = 12, targetHeight = 180) {
  if (!Number.isFinite(width) || width <= 2) return [];
  const rows = [];
  let start = 0;
  while (start < ratios.length) {
    let end = start;
    let sum = 0;
    while (end < ratios.length) {
      const ratio = ratios[end];
      const nextSum = sum + ratio;
      const nextHeight = (width - (end - start) * gap - (end - start + 1) * 2) / nextSum;
      if (sum && nextHeight < targetHeight) {
        const height = (width - (end - start - 1) * gap - (end - start) * 2) / sum;
        if (Math.abs(height - targetHeight) < Math.abs(nextHeight - targetHeight)) break;
      }
      sum = nextSum;
      end++;
      if (nextHeight <= targetHeight) break;
    }
    const available = width - (end - start - 1) * gap - (end - start) * 2;
    const height = Math.max(1, Math.min(available / sum, end === ratios.length ? targetHeight : Infinity));
    rows.push({ start, end, height, widths: ratios.slice(start, end).map(ratio => ratio * height + 2) });
    start = end;
  }
  return rows;
}

export function initPictureLayout(grid) {
  const button = document.getElementById('picture-layout-toggle');
  if (!grid || !button) return;
  let enabled = false;
  try { enabled = localStorage.getItem(STORAGE_KEY) === 'pictures'; } catch { /* Storage can be disabled. */ }
  const sheet = document.createElement('style');
  sheet.id = 'picture-layout-sizes';
  document.head.append(sheet);

  function layout() {
    const cards = Array.from(grid.children).filter(el => el.classList.contains('media-card'));
    const active = enabled && cards.some(el => el.classList.contains('image-card'));
    grid.classList.toggle('picture-grid', active);
    button.setAttribute('aria-pressed', String(enabled));
    if (!active) { sheet.textContent = ''; return; }
    const width = grid.getBoundingClientRect().width;
    const gap = Number.parseFloat(getComputedStyle(grid).columnGap) || 0;
    const ratios = cards.map(el => Number(el.dataset.pictureRatio) || 16 / 9);
    const rows = pictureRows(ratios, width, gap);
    const rules = [];
    for (const row of rows) {
      for (let index = row.start; index < row.end; index++) {
        const position = Array.prototype.indexOf.call(grid.children, cards[index]) + 1;
        const selector = `#media-grid.picture-grid > :nth-child(${position})`;
        const cardWidth = Math.floor(row.widths[index - row.start] * 1000) / 1000;
        rules.push(`${selector}{width:${cardWidth}px} ${selector} .thumb-wrap{height:${row.height}px}`);
      }
    }
    // Geometry lives in a scoped stylesheet; elements never receive inline styles.
    sheet.textContent = rules.join('\n');
  }

  button.addEventListener('click', () => {
    enabled = !enabled;
    try { localStorage.setItem(STORAGE_KEY, enabled ? 'pictures' : 'grid'); } catch { /* Keep the session preference. */ }
    layout(); // Retain cards, selection, pagination and keyboard focus.
  });
  let previousWidth = -1;
  new ResizeObserver(() => {
    const width = grid.getBoundingClientRect().width;
    if (width !== previousWidth) { previousWidth = width; layout(); }
  }).observe(grid);
  new MutationObserver(layout).observe(grid, { childList: true });
  layout();
}
