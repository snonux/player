const STORAGE_KEY = 'kiss-thumbnail-size';
const SCALES = [0.65, 0.8, 1, 1.3, 1.65];
const DEFAULT = 2;

export function initThumbnailSize(grid) {
  if (!grid) return {};
  const smaller = document.getElementById('thumbnail-smaller');
  const larger = document.getElementById('thumbnail-larger');
  const reset = document.getElementById('thumbnail-reset');
  let index = DEFAULT;
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved !== null && /^[0-4]$/.test(saved)) index = Number(saved);
  } catch { /* Storage can be disabled. */ }

  function apply(next) {
    const focused = document.activeElement;
    index = Math.max(0, Math.min(SCALES.length - 1, next));
    grid.dataset.thumbnailSize = String(index);
    grid.dataset.thumbnailScale = String(SCALES[index]);
    smaller.disabled = index === 0;
    larger.disabled = index === SCALES.length - 1;
    reset.textContent = `${Math.round(SCALES[index] * 100)}%`;
    reset.setAttribute('aria-label', `Preview size ${reset.textContent}; reset to 100%`);
    // A disabled button loses native focus. Keep a visible, usable control
    // focused when a click or Enter reaches either size boundary.
    if ((focused === smaller && smaller.disabled) ||
        (focused === larger && larger.disabled)) reset.focus({ preventScroll: true });
    grid.dispatchEvent(new Event('thumbnail-size:changed'));
  }
  function change(delta) {
    apply(delta === 0 ? DEFAULT : index + delta);
    try { localStorage.setItem(STORAGE_KEY, String(index)); } catch { /* Keep the session preference. */ }
  }
  smaller.addEventListener('click', () => change(-1));
  larger.addEventListener('click', () => change(1));
  reset.addEventListener('click', () => change(0));
  apply(index);
  return {
    thumbnailSmaller: () => change(-1),
    thumbnailLarger: () => change(1),
    thumbnailReset: () => change(0),
  };
}
