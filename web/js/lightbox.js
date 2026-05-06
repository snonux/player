let currentMediaList = [];
let currentIndex = -1;
let zoomScale = 1;
let panX = 0;
let panY = 0;
let isPanning = false;
let panStart = { x: 0, y: 0 };
let slideshowTimer = null;
let slideshowPausedUntil = 0;
let onNavigateCallback = null;

export function initLightbox({ onNavigate }) {
  onNavigateCallback = onNavigate;

  document.getElementById('lb-close')?.addEventListener('click', close);
  document.getElementById('lb-prev')?.addEventListener('click', prev);
  document.getElementById('lb-next')?.addEventListener('click', next);
  document.getElementById('lb-zoom-in')?.addEventListener('click', zoomIn);
  document.getElementById('lb-zoom-out')?.addEventListener('click', zoomOut);
  document.getElementById('lb-slideshow')?.addEventListener('click', toggleSlideshow);

  // Close on backdrop click (clicking outside the image/toolbar)
  document.getElementById('image-lightbox')?.addEventListener('click', (e) => {
    if (e.target === e.currentTarget) close();
  });

  const img = document.getElementById('lightbox-image');
  if (img) {
    img.addEventListener('mousedown', (e) => {
      if (zoomScale > 1) {
        isPanning = true;
        panStart = { x: e.clientX - panX, y: e.clientY - panY };
        img.style.cursor = 'grabbing';
        e.preventDefault();
      }
    });
    img.addEventListener('wheel', (e) => {
      e.preventDefault();
      if (e.deltaY < 0) zoomIn();
      else zoomOut();
    }, { passive: false });
  }
  window.addEventListener('mousemove', (e) => {
    if (!isPanning) return;
    panX = e.clientX - panStart.x;
    panY = e.clientY - panStart.y;
    applyTransform();
  });
  window.addEventListener('mouseup', () => {
    if (isPanning) {
      isPanning = false;
      const img = document.getElementById('lightbox-image');
      if (img) img.style.cursor = zoomScale > 1 ? 'grab' : 'zoom-in';
    }
  });
}

export function open(mediaArray, startMediaId) {
  currentMediaList = mediaArray.filter((m) => m.type === 'image');
  currentIndex = currentMediaList.findIndex((m) => m.id === startMediaId);
  if (currentIndex === -1) currentIndex = 0;
  render();
  document.getElementById('image-lightbox')?.classList.add('open');
}

export function close() {
  stopSlideshow();
  document.getElementById('image-lightbox')?.classList.remove('open');
  resetZoom();
  currentMediaList = [];
  currentIndex = -1;
}

export function isOpen() {
  return document.getElementById('image-lightbox')?.classList.contains('open');
}

export function next() {
  if (!currentMediaList.length) return;
  currentIndex = (currentIndex + 1) % currentMediaList.length;
  render();
  pauseSlideshow();
}

export function prev() {
  if (!currentMediaList.length) return;
  currentIndex = (currentIndex - 1 + currentMediaList.length) % currentMediaList.length;
  render();
  pauseSlideshow();
}

export function zoomIn() {
  setZoom(zoomScale * 1.25);
  pauseSlideshow();
}

export function zoomOut() {
  setZoom(zoomScale / 1.25);
  pauseSlideshow();
}

export function toggleSlideshow() {
  if (slideshowTimer) stopSlideshow();
  else startSlideshow();
}

export function isSlideshowActive() {
  return !!slideshowTimer;
}

function render() {
  const media = currentMediaList[currentIndex];
  const img = document.getElementById('lightbox-image');
  const meta = document.getElementById('lb-meta');
  const counter = document.getElementById('lb-counter');
  if (!media || !img) return;
  img.src = `/api/media/${media.id}/stream`;
  resetZoom();
  if (meta) {
    const size = media.file_size_bytes ? fmtSize(media.file_size_bytes) : '';
    const res = media.resolution || '';
    meta.textContent = `${media.file_name}${res ? ' — ' + res : ''}${size ? ' — ' + size : ''}`;
  }
  if (counter) {
    counter.textContent = `${currentIndex + 1} / ${currentMediaList.length}`;
  }
}

function setZoom(s) {
  zoomScale = Math.max(0.5, Math.min(s, 5));
  applyTransform();
}

function applyTransform() {
  const img = document.getElementById('lightbox-image');
  if (!img) return;
  img.style.transform = `translate(${panX}px, ${panY}px) scale(${zoomScale})`;
  img.style.cursor = zoomScale > 1 ? 'grab' : 'zoom-in';
}

function resetZoom() {
  zoomScale = 1;
  panX = 0;
  panY = 0;
  applyTransform();
}

function startSlideshow() {
  if (slideshowTimer) clearInterval(slideshowTimer);
  slideshowTimer = setInterval(() => {
    if (Date.now() < slideshowPausedUntil) return;
    if (!isOpen()) { stopSlideshow(); return; }
    next();
  }, 5000);
  const btn = document.getElementById('lb-slideshow');
  if (btn) btn.textContent = '⏸';
}

function stopSlideshow() {
  clearInterval(slideshowTimer);
  slideshowTimer = null;
  const btn = document.getElementById('lb-slideshow');
  if (btn) btn.textContent = '⏵';
}

function pauseSlideshow() {
  slideshowPausedUntil = Date.now() + 10000;
}

function fmtSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  const kb = bytes / 1024;
  if (kb < 1024) return Math.round(kb) + ' KB';
  const mb = kb / 1024;
  if (mb < 1024) return Math.round(mb * 10) / 10 + ' MB';
  return Math.round((mb / 1024) * 10) / 10 + ' GB';
}
