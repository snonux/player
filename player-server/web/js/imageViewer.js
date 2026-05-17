let els = () => ({});
let isImageMode = () => false;
let playNext = () => {};

let zoomScale = 1;
let panX = 0;
let panY = 0;
let isPanning = false;
let panStart = { x: 0, y: 0 };
let slideshowTimer = null;
let slideshowPausedUntil = 0;
let cropMode = false;
let cropPositionX = 50;
let cropPositionY = 50;
const CROP_PRESETS = [
  { x: 50, y: 50, label: 'C' },
  { x: 50, y: 0, label: 'T' },
  { x: 50, y: 100, label: 'B' },
  { x: 0, y: 50, label: 'L' },
  { x: 100, y: 50, label: 'R' },
];
let cropPresetIndex = 0;

export function initImageViewer(options = {}) {
  els = typeof options.els === 'function' ? options.els : els;
  isImageMode = typeof options.isImageMode === 'function' ? options.isImageMode : isImageMode;
  playNext = typeof options.playNext === 'function' ? options.playNext : playNext;

  const e = els();
  e.btnZoomIn?.addEventListener('click', () => { zoomIn(); pauseSlideshow(); });
  e.btnZoomOut?.addEventListener('click', () => { zoomOut(); pauseSlideshow(); });
  e.btnSlideshow?.addEventListener('click', toggleSlideshow);

  e.image?.addEventListener('mousedown', (ev) => {
    if (zoomScale > 1) {
      isPanning = true;
      panStart = { x: ev.clientX - panX, y: ev.clientY - panY };
      e.image.style.cursor = 'grabbing';
      ev.preventDefault();
    }
  });
  e.image?.addEventListener('wheel', (ev) => {
    ev.preventDefault();
    if (ev.deltaY < 0) zoomIn();
    else zoomOut();
  }, { passive: false });
  window.addEventListener('mousemove', (ev) => {
    if (!isPanning) return;
    panX = ev.clientX - panStart.x;
    panY = ev.clientY - panStart.y;
    applyImageTransform();
  });
  window.addEventListener('mouseup', () => {
    if (isPanning) {
      isPanning = false;
      const image = els().image;
      if (image) image.style.cursor = zoomScale > 1 ? 'grab' : 'zoom-in';
    }
  });
}

function setImageZoom(s) {
  zoomScale = Math.max(0.5, Math.min(s, 5));
  applyImageTransform();
}

function applyImageTransform() {
  const e = els();
  if (!e.image) return;
  e.image.style.transform = `translate(${panX}px, ${panY}px) scale(${zoomScale})`;
  e.image.style.cursor = zoomScale > 1 ? 'grab' : 'zoom-in';
}

export function resetImageZoom() {
  zoomScale = 1;
  panX = 0;
  panY = 0;
  applyImageTransform();
}

export function zoomIn() {
  setImageZoom(zoomScale * 1.25);
}

export function zoomOut() {
  setImageZoom(zoomScale / 1.25);
}

export function resetZoom() {
  resetImageZoom();
}

export function toggleSlideshow() {
  if (slideshowTimer) stopSlideshow();
  else startSlideshow();
}

export function isSlideshowActive() {
  return !!slideshowTimer;
}

function startSlideshow() {
  if (slideshowTimer) clearInterval(slideshowTimer);
  slideshowTimer = setInterval(() => {
    if (Date.now() < slideshowPausedUntil) return;
    if (!isImageMode()) { stopSlideshow(); return; }
    playNext();
  }, 5000);
  const btn = els().btnSlideshow;
  if (btn) btn.textContent = '⏸';
}

export function stopSlideshow() {
  clearInterval(slideshowTimer);
  slideshowTimer = null;
  const btn = els().btnSlideshow;
  if (btn) btn.textContent = '⏵';
}

export function pauseSlideshow() {
  slideshowPausedUntil = Date.now() + 10000;
}

export function toggleCrop() {
  const e = els();
  if (!e.player) return;
  if (!document.fullscreenElement) return;
  cropMode = !cropMode;
  e.player.classList.toggle('crop-mode', cropMode);
  if (!cropMode) {
    resetCropPosition();
  } else {
    applyCropPosition();
  }
}

function applyCropPosition() {
  const e = els();
  if (!e.player) return;
  e.player.style.setProperty('--crop-x', cropPositionX + '%');
  e.player.style.setProperty('--crop-y', cropPositionY + '%');
  updateCropIndicator();
}

function updateCropIndicator() {
  const e = els();
  if (!e.cropIndicator) return;
  if (!cropMode) {
    e.cropIndicator.classList.add('hidden');
    return;
  }
  const snap = 5;
  let label = '';
  if (Math.abs(cropPositionX - 50) <= snap && Math.abs(cropPositionY - 50) <= snap) label = 'C';
  else if (Math.abs(cropPositionX - 50) <= snap && cropPositionY <= snap) label = 'T';
  else if (Math.abs(cropPositionX - 50) <= snap && cropPositionY >= 95) label = 'B';
  else if (cropPositionX <= snap && Math.abs(cropPositionY - 50) <= snap) label = 'L';
  else if (cropPositionX >= 95 && Math.abs(cropPositionY - 50) <= snap) label = 'R';
  else label = Math.round(cropPositionX) + ',' + Math.round(cropPositionY);
  e.cropIndicator.textContent = label;
  e.cropIndicator.classList.remove('hidden');
}

export function resetCropPosition() {
  cropPositionX = 50;
  cropPositionY = 50;
  cropPresetIndex = 0;
  applyCropPosition();
}

export function clearCropMode() {
  cropMode = false;
  const e = els();
  e.player?.classList.remove('crop-mode');
  resetCropPosition();
}

export function shiftCropPosition(dx, dy) {
  if (!cropMode) return false;
  cropPositionX = Math.max(0, Math.min(100, cropPositionX + dx));
  cropPositionY = Math.max(0, Math.min(100, cropPositionY + dy));
  applyCropPosition();
  return true;
}

export function cycleCropPosition() {
  if (!cropMode) return;
  cropPresetIndex = (cropPresetIndex + 1) % CROP_PRESETS.length;
  const preset = CROP_PRESETS[cropPresetIndex];
  cropPositionX = preset.x;
  cropPositionY = preset.y;
  applyCropPosition();
}
