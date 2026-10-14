// A separate, paused decoder shares the playback URL (including compat/share
// credentials). It exists only during hover: no server-side frame generation,
// no background preload, and no changes to the playing element.
export function initSeekPreview({ track, player, video, getVideo, formatTime }) {
  if (!track || !player || !video) return () => {};
  let popup, canvas, time, status, positions;
  let decoder = null;
  let debounce = null;
  let timeout = null;
  let target = 0;
  let requested = null;
  let failed = false;

  function clearTimers() {
    clearTimeout(debounce);
    clearTimeout(timeout);
    debounce = timeout = null;
  }

  function releaseDecoder() {
    const old = decoder;
    decoder = null; // Late events from a previous source cannot paint.
    if (old) {
      old.removeAttribute('src');
      old.load();
    }
  }

  function reset() {
    clearTimers();
    releaseDecoder();
    requested = null;
    failed = false;
    if (popup) popup.hidden = true;
  }

  function ensurePopup() {
    if (popup) return;
    popup = document.createElement('div');
    popup.className = 'seek-preview';
    positions = document.createElement('style');
    document.head.appendChild(positions);
    popup.setAttribute('aria-hidden', 'true');
    canvas = document.createElement('canvas');
    canvas.width = 160;
    canvas.height = 90;
    time = document.createElement('div');
    time.className = 'seek-preview-time';
    status = document.createElement('div');
    status.className = 'seek-preview-status';
    popup.append(canvas, time, status);
    player.appendChild(popup);
  }

  function unavailable() {
    clearTimers();
    releaseDecoder();
    failed = true;
    canvas.hidden = true;
    status.textContent = 'Preview unavailable';
  }

  function seek() {
    if (!decoder || failed || decoder.readyState < 1 || decoder.seeking) return;
    // Avoid the exact EOF, which does not consistently produce a frame.
    requested = Math.min(target, Math.max(0, decoder.duration - 0.05));
    try {
      if (Math.abs(decoder.currentTime - requested) < 0.01 && decoder.readyState >= 2) {
        paint();
      } else {
        decoder.currentTime = requested;
      }
    } catch {
      unavailable();
    }
  }

  function paint() {
    if (!decoder || popup.hidden || requested === null) return;
    const latest = Math.min(target, Math.max(0, decoder.duration - 0.05));
    // Mouse motion while a seek was pending: discard that frame, seek once
    // more to the latest position, rather than displaying it under a new time.
    if (Math.abs(latest - requested) > 0.01) {
      seek();
      return;
    }
    if (decoder.readyState < 2 || !decoder.videoWidth || !decoder.videoHeight) return;
    try {
      const ctx = canvas.getContext('2d');
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      const scale = Math.min(canvas.width / decoder.videoWidth, canvas.height / decoder.videoHeight);
      const w = decoder.videoWidth * scale;
      const h = decoder.videoHeight * scale;
      ctx.drawImage(decoder, (canvas.width - w) / 2, (canvas.height - h) / 2, w, h);
      canvas.hidden = false;
      status.textContent = '';
      clearTimeout(timeout);
    } catch {
      unavailable();
    }
  }

  function startDecoder(source) {
    const media = document.createElement('video');
    decoder = media;
    media.muted = true;
    media.playsInline = true;
    media.preload = 'auto';
    const current = (fn) => () => { if (decoder === media) fn(); };
    media.addEventListener('loadedmetadata', current(() => {
      if (!Number.isFinite(media.duration) || media.duration <= 0) unavailable();
      else seek();
    }));
    media.addEventListener('seeked', current(paint));
    media.addEventListener('loadeddata', current(() => { if (!media.seeking) seek(); }));
    media.addEventListener('error', current(unavailable));
    media.src = source;
    media.load();
  }

  function move(ev) {
    if (ev.pointerType && ev.pointerType !== 'mouse') return;
    const active = getVideo();
    const source = active?.getAttribute('src');
    const duration = active?.duration;
    if (active !== video || !source || active.error || !Number.isFinite(duration) || duration <= 0) {
      reset();
      return;
    }
    const rect = track.getBoundingClientRect();
    if (rect.width <= 0) return;
    const fraction = Math.max(0, Math.min(1, (ev.clientX - rect.left) / rect.width));
    target = fraction * duration;
    ensurePopup();
    popup.hidden = false;
    canvas.hidden = true;
    time.textContent = formatTime(target);
    status.textContent = failed ? 'Preview unavailable' : 'Loading preview…';
    const bounds = player.getBoundingClientRect();
    const width = Math.min(160, Math.max(0, bounds.width - 16));
    const left = Math.max(8, Math.min(bounds.width - width - 8, rect.left + fraction * rect.width - bounds.left - width / 2));
    positions.textContent = `#player .seek-preview { --preview-left: ${left}px; --preview-bottom: ${Math.max(8, bounds.bottom - rect.top + 8)}px; }`;
    clearTimeout(debounce);
    clearTimeout(timeout);
    if (failed) return;
    // One pending request per decoder; a stalled source falls back to time.
    timeout = setTimeout(unavailable, 5000);
    debounce = setTimeout(() => {
      debounce = null;
      if (!decoder) startDecoder(source);
      else seek();
    }, 100);
  }

  track.addEventListener('pointermove', move);
  track.addEventListener('pointerleave', reset);
  track.addEventListener('pointercancel', reset);
  track.addEventListener('blur', reset);
  // Source reloads, switches and failures invalidate the entire hover session.
  ['emptied', 'loadstart', 'error'].forEach((event) => video.addEventListener(event, reset));
  return reset;
}
