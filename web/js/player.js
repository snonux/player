import { API } from './api.js';
import { state } from './state.js';

let currentMedia = null;
let isPlaying = false;
let progressInterval = null;
let currentMediaIndex = -1;
let reportProgress = true;
let detachWindow = null;
let detachReady = false;
let pendingDetachMessage = null;
let detachedPlaybackState = null;
let nextHandler = null;
let previousHandler = null;

const els = () => ({
  video: document.getElementById('media-video'),
  audio: document.getElementById('media-audio'),
  player: document.getElementById('player'),
  btnPlay: document.getElementById('btn-play'),
  btnPrev: document.getElementById('btn-prev'),
  btnNext: document.getElementById('btn-next'),
  btnMute: document.getElementById('btn-mute'),
  btnFs: document.getElementById('btn-fullscreen'),
  btnMinimize: document.getElementById('btn-minimize'),
  btnRestore: document.getElementById('btn-restore-player'),
  restoreTitle: document.getElementById('player-restore-title'),
  bigPlay: document.getElementById('big-play'),
  coverArt: document.getElementById('cover-art'),
  track: document.getElementById('progress-track'),
  buffered: document.getElementById('progress-buffered'),
  fill: document.getElementById('progress-fill'),
  thumb: document.getElementById('progress-thumb'),
  volume: document.getElementById('volume-slider'),
  timeElapsed: document.getElementById('time-elapsed'),
  timeTotal: document.getElementById('time-total'),
});

export function initPlayer(options = {}) {
  nextHandler = typeof options.onNext === 'function' ? options.onNext : null;
  previousHandler = typeof options.onPrevious === 'function' ? options.onPrevious : null;
  const e = els();
  if (!e.video || !e.audio) return;

  e.btnPlay?.addEventListener('click', togglePlay);
  e.btnPrev?.addEventListener('click', () => triggerPrevious({ forcePlay: true }));
  e.btnNext?.addEventListener('click', () => triggerNext({ forcePlay: true }));
  e.btnMute?.addEventListener('click', toggleMute);
  e.btnFs?.addEventListener('click', toggleFullscreen);
  e.btnMinimize?.addEventListener('click', minimizePlayer);
  e.btnRestore?.addEventListener('click', toggleMinimize);
  e.bigPlay?.addEventListener('click', togglePlay);
  e.bigPlay?.addEventListener('keydown', (ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); togglePlay(); } });

  e.volume?.addEventListener('input', () => {
    const v = parseFloat(e.volume.value);
    e.video.volume = v;
    e.audio.volume = v;
    e.video.muted = v === 0;
    e.audio.muted = v === 0;
    updateMuteIcon();
  });

  [e.video, e.audio].forEach((m) => {
    m.addEventListener('timeupdate', () => {
      if (!m.duration || isNaN(m.duration)) return;
      const pct = (m.currentTime / m.duration) * 100;
      e.fill.style.width = pct.toFixed(2) + '%';
      e.thumb.style.left = pct.toFixed(2) + '%';
      e.timeElapsed.textContent = fmt(m.currentTime);
      updateBufferedRanges(m);
    });
    m.addEventListener('loadedmetadata', () => {
      e.timeTotal.textContent = fmt(isFinite(m.duration) ? m.duration : 0);
      updateBufferedRanges(m);
    });
    ['progress', 'durationchange', 'loadeddata', 'canplay', 'seeked'].forEach((event) => {
      m.addEventListener(event, () => updateBufferedRanges(m));
    });
    m.addEventListener('ended', () => {
      updateUI(false);
      isPlaying = false;
      stopProgressTimer();
      triggerNext({ forcePlay: true });
    });
    m.addEventListener('play', () => { updateUI(true); isPlaying = true; startProgressTimer(); });
    m.addEventListener('pause', () => { updateUI(false); isPlaying = false; stopProgressTimer(); });
  });

  e.video.addEventListener('loadedmetadata', updateFloatingSize);

  function setupVideoDebug(m) {
    const events = ['loadstart','loadeddata','loadedmetadata','canplay','canplaythrough','playing','waiting','stalled','suspend','error','abort','emptied','ended'];
    events.forEach(event => {
      m.addEventListener(event, () => {
        console.log('[video-debug]', event,
          'readyState=', m.readyState,
          'networkState=', m.networkState,
          'paused=', m.paused,
          'src=', m.src?.slice(-40),
          'error=', m.error?.code || 'none',
          'errorMsg=', m.error?.message || '');
      });
    });
  }
  setupVideoDebug(e.video);
  setupVideoDebug(e.audio);

  let seeking = false;
  const seekToFraction = (frac) => {
    const m = currentMediaElement();
    if (!m || !m.duration) return;
    m.currentTime = Math.max(0, Math.min(1, frac)) * m.duration;
  };
  const handlePointer = (ev) => {
    const r = e.track.getBoundingClientRect();
    const clientX = ev.touches ? ev.touches[0].clientX : ev.clientX;
    return (clientX - r.left) / r.width;
  };
  e.track?.addEventListener('click', (ev) => seekToFraction(handlePointer(ev)));
  e.track?.addEventListener('touchstart', (ev) => {
    seeking = true;
    ev.preventDefault();
    seekToFraction(handlePointer(ev));
  }, { passive: false });
  e.track?.addEventListener('touchmove', (ev) => {
    if (!seeking) return;
    ev.preventDefault();
    seekToFraction(handlePointer(ev));
  }, { passive: false });
  e.track?.addEventListener('touchend', () => { seeking = false; });
  e.track?.addEventListener('keydown', (ev) => {
    const m = currentMediaElement();
    if (!m || !m.duration) return;
    if (ev.key === 'ArrowLeft') { ev.preventDefault(); m.currentTime = Math.max(0, m.currentTime - 5); }
    if (ev.key === 'ArrowRight') { ev.preventDefault(); m.currentTime = Math.min(m.duration, m.currentTime + 5); }
  });
}

function updateFloatingSize() {
  const e = els();
  if (!e.player || !e.video) return;
  const vw = e.video.videoWidth || 0;
  const vh = e.video.videoHeight || 0;
  let w, h;
  if (vw > 0 && vh > 0) {
    const aspect = vw / vh;
    const base = 240; // px reference height for the floating player
    h = Math.max(180, Math.min(340, base));
    w = Math.max(240, Math.min(480, Math.round(h * aspect)));
  } else {
    w = 320;
    h = 240;
  }
  e.player.style.setProperty('--floating-w', w + 'px');
  e.player.style.setProperty('--floating-h', h + 'px');
}

function updateBufferedRanges(m) {
  const e = els();
  if (!e.buffered || !m || !m.duration || !isFinite(m.duration)) {
    if (e.buffered) e.buffered.style.background = 'transparent';
    return;
  }

  const ranges = [];
  for (let i = 0; i < m.buffered.length; i++) {
    const start = Math.max(0, Math.min(100, (m.buffered.start(i) / m.duration) * 100));
    const end = Math.max(0, Math.min(100, (m.buffered.end(i) / m.duration) * 100));
    if (end > start) ranges.push([start, end]);
  }
  if (!ranges.length) {
    e.buffered.style.background = 'transparent';
    return;
  }

  const color = 'var(--player-progress-buffered)';
  const stops = ['transparent 0%'];
  for (const [start, end] of ranges) {
    stops.push(`transparent ${start.toFixed(2)}%`);
    stops.push(`${color} ${start.toFixed(2)}%`);
    stops.push(`${color} ${end.toFixed(2)}%`);
    stops.push(`transparent ${end.toFixed(2)}%`);
  }
  stops.push('transparent 100%');
  e.buffered.style.background = `linear-gradient(to right, ${stops.join(', ')})`;
}

export function togglePlay() {
  if (isDetached()) {
    postToDetach({ type: 'detach-command', action: 'toggle-play' });
    return;
  }
  const m = currentMediaElement();
  if (!m) return;
  if (m.paused) { m.play().catch(() => {}); } else { m.pause(); }
}

export function hasLoadedMedia() {
  return !!currentMedia;
}

export function seekRelative(seconds) {
  const amount = Number(seconds);
  if (!currentMedia || !isFinite(amount)) return false;
  if (isDetached()) {
    postToDetach({ type: 'detach-command', action: 'seek-relative', seconds: amount });
    return true;
  }
  const m = currentMediaElement();
  if (!m) return false;
  const upper = m.duration && isFinite(m.duration) ? m.duration : Infinity;
  m.currentTime = Math.max(0, Math.min(upper, (m.currentTime || 0) + amount));
  return true;
}

export function selectAndPlay(media, index, resumeFrom = 0) {
  currentMedia = media;
  currentMediaIndex = index ?? -1;
  if (isDetached()) {
    isPlaying = true;
    stopProgressTimer();
    sendDetachedLoad(media, resumeFrom, true);
    highlightPlayingCard();
    return;
  }
  loadMedia(media, resumeFrom);
  isPlaying = true;
  const m = currentMediaElement();
  if (m) {
    console.log('selectAndPlay: type=', media.type, 'src=', m.src, 'readyState=', m.readyState);
    m.play().catch((err) => { console.error('play() failed:', err); });
  } else {
    console.error('selectAndPlay: no media element found for type', media.type);
  }
  highlightPlayingCard();
}

export function loadMediaDirect(media, streamUrl, thumbnailUrl, resumeFrom = 0) {
  currentMedia = media;
  currentMediaIndex = -1;
  reportProgress = false;
  const e = els();
  const isVideo = media.type === 'video';
  const src = streamUrl;
  if (isVideo) {
    e.video.pause();
    e.audio.pause(); e.audio.src = '';
    e.video.style.display = '';
    e.audio.style.display = 'none';
    e.coverArt?.classList.add('hidden');
    e.video.src = src;
    e.video.load();
    seekWhenMetadataReady(e.video, resumeFrom);
  } else {
    e.audio.pause();
    e.video.pause(); e.video.src = '';
    e.video.style.display = 'none';
    e.audio.style.display = 'none';
    e.audio.src = src;
    seekWhenMetadataReady(e.audio, resumeFrom);
    if (e.coverArt) {
      if (thumbnailUrl) {
        e.coverArt.src = thumbnailUrl;
        e.coverArt.classList.remove('hidden');
      } else {
        e.coverArt.classList.add('hidden');
        e.coverArt.src = '';
      }
    }
  }
  e.player?.classList.add('open');
  e.btnPlay.textContent = '⏸';
  e.bigPlay?.classList.add('hidden');
  e.timeTotal.textContent = fmt(media.duration ?? 0);
  e.buffered && (e.buffered.style.background = 'transparent');
  e.fill.style.width = '0%';
  e.thumb.style.left = '0%';
  updateMinimizedTitle();
  updateFloatingSize();
}

function loadMedia(media, resumeFrom = 0) {
  reportProgress = true;
  const e = els();
  const isVideo = media.type === 'video';
  const src = `/api/media/${media.id}/stream`;
  if (isVideo) {
    e.video.pause();
    e.audio.pause(); e.audio.src = '';
    e.video.style.display = '';
    e.audio.style.display = 'none';
    e.coverArt?.classList.add('hidden');
    e.video.src = src;
    e.video.load();
    seekWhenMetadataReady(e.video, resumeFrom);
  } else {
    e.audio.pause();
    e.video.pause(); e.video.src = '';
    e.video.style.display = 'none';
    e.audio.style.display = 'none';
    e.audio.src = src;
    seekWhenMetadataReady(e.audio, resumeFrom);
    if (e.coverArt) {
      if (media.thumbnail_path) {
        e.coverArt.src = `/api/media/${media.id}/thumbnail`;
        e.coverArt.classList.remove('hidden');
      } else {
        e.coverArt.classList.add('hidden');
        e.coverArt.src = '';
      }
    }
  }
  e.player?.classList.add('open');
  e.btnPlay.textContent = '⏸';
  e.bigPlay?.classList.add('hidden');
  e.timeTotal.textContent = fmt(media.duration ?? 0);
  e.buffered && (e.buffered.style.background = 'transparent');
  e.fill.style.width = '0%';
  e.thumb.style.left = '0%';
  updateMinimizedTitle();
  updateFloatingSize();
}

function currentMediaElement() {
  const e = els();
  if (currentMedia?.type === 'audio') return e.audio;
  return e.video;
}

function seekWhenMetadataReady(m, seconds) {
  const target = Number(seconds || 0);
  if (!m || !isFinite(target) || target <= 0) return;
  const seek = () => {
    try {
      m.currentTime = target;
    } catch {}
  };
  if (m.readyState >= HTMLMediaElement.HAVE_METADATA) seek();
  else m.addEventListener('loadedmetadata', seek, { once: true });
}

function updateUI(playing) {
  const e = els();
  isPlaying = playing;
  e.btnPlay.textContent = playing ? '⏸' : '▶';
  if (playing) e.bigPlay?.classList.add('hidden');
  else e.bigPlay?.classList.remove('hidden');
}

function toggleMute() {
  const m = currentMediaElement();
  if (!m) return;
  m.muted = !m.muted;
  updateMuteIcon();
}

function updateMuteIcon() {
  const m = currentMediaElement();
  const e = els();
  if (!m || !e.btnMute) return;
  e.btnMute.textContent = m.muted || m.volume === 0 ? '🔇' : '🔊';
}

export function toggleFullscreen() {
  const p = els().player;
  if (!p) return;
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {});
    p.classList.remove('is-fullscreen');
  } else {
    p.requestFullscreen().catch(() => {});
    p.classList.add('is-fullscreen');
  }
}

export function toggleMinimize() {
  const e = els();
  if (!e.player || !e.player.classList.contains('open')) return;
  const willMinimize = !e.player.classList.contains('minimized');
  e.player.classList.toggle('minimized');
  if (willMinimize) exitFullscreenIfNeeded();
  updateMinimizedTitle();
}

function minimizePlayer() {
  const e = els();
  if (!e.player || !e.player.classList.contains('open')) return;
  e.player.classList.add('minimized');
  exitFullscreenIfNeeded();
  updateMinimizedTitle();
}

function updateMinimizedTitle() {
  const e = els();
  if (!e.restoreTitle) return;
  e.restoreTitle.textContent = currentMedia?.file_name ? `Restore: ${currentMedia.file_name}` : 'Restore player';
}

export function exitFullscreenIfNeeded() {
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {});
    els().player?.classList.remove('is-fullscreen');
  }
}

function startProgressTimer() {
  stopProgressTimer();
  if (!reportProgress) return;
  progressInterval = setInterval(() => {
    const m = currentMediaElement();
    if (m && currentMedia && !m.paused) {
      API.progress(currentMedia.id, m.currentTime).catch(() => {});
    }
  }, 3000);
}

function stopProgressTimer() {
  clearInterval(progressInterval);
  progressInterval = null;
}

function highlightPlayingCard() {
  document.querySelectorAll('.media-card, .media-row').forEach((c) => c.classList.remove('playing'));
  if (currentMedia?.id) {
    document.querySelector(`.media-card[data-id="${currentMedia.id}"], .media-row[data-id="${currentMedia.id}"]`)?.classList.add('playing');
  }
}

function triggerPrevious(options = {}) {
  if (previousHandler) {
    previousHandler(options);
    return;
  }
  playPrevious();
}

function triggerNext(options = {}) {
  if (nextHandler) {
    nextHandler(options);
    return;
  }
  playNext();
}

export function playPrevious() {
  const list = state.media;
  if (!list.length) return;
  const currentIdx = currentMediaListIndex();
  const idx = currentIdx > 0 ? currentIdx - 1 : list.length - 1;
  selectAndPlay(list[idx], idx);
}

export function playNext() {
  const list = state.media;
  if (!list.length) return;
  const currentIdx = currentMediaListIndex();
  const idx = currentIdx >= 0 && currentIdx + 1 < list.length ? currentIdx + 1 : 0;
  selectAndPlay(list[idx], idx);
}

function currentMediaListIndex() {
  if (currentMediaIndex >= 0 && state.media[currentMediaIndex]?.id === currentMedia?.id) {
    return currentMediaIndex;
  }
  const idx = state.media.findIndex((m) => m.id === currentMedia?.id);
  return idx >= 0 ? idx : currentMediaIndex;
}

export function currentMediaId() { return currentMedia?.id; }

export function currentMediaInfo() { return currentMedia; }

export function isPlaybackActive() {
  if (isDetached()) return !!detachedPlaybackState?.playing;
  return isPlaying;
}

export function isDetached() { return !!detachWindow && !detachWindow.closed; }

export function toggleDetach() {
  if (isDetached()) {
    reattachDetached();
    return;
  }
  const e = els();
  if (!e.player) return;

  const snapshot = localPlaybackState();
  const features = 'width=640,height=480,resizable=yes,scrollbars=no,status=no,location=no,menubar=no,toolbar=no';
  detachWindow = window.open('/detach.html', 'playerDetach', features);
  if (!detachWindow) return;

  detachReady = false;
  detachedPlaybackState = snapshot;
  if (currentMedia) {
    pendingDetachMessage = detachedLoadMessage(currentMedia, snapshot.currentTime, snapshot.playing);
  }
  currentMediaElement()?.pause();
  e.player.classList.add('hidden');
  showDetachedPlaceholder(true);
}

export function onDetachReady(popup) {
  if (!popup || popup !== detachWindow) return;
  detachReady = true;
  if (pendingDetachMessage) postToDetach(pendingDetachMessage);
}

export function onDetachClosing(state = null) {
  if (state) detachedPlaybackState = state;
  if (!detachWindow) return;
  reattachDetached({ closePopup: false });
}

function reattachDetached({ closePopup = true } = {}) {
  const popup = detachWindow;
  const state = readDetachedWindowState(popup) || detachedPlaybackState;
  detachWindow = null;
  detachReady = false;
  pendingDetachMessage = null;
  if (closePopup && popup && !popup.closed) {
    popup.postMessage({ type: 'detach-request-state' }, window.location.origin);
    popup.close();
  }
  showDetachedPlaceholder(false);
  els().player?.classList.remove('hidden');

  if (state?.media) {
    currentMedia = state.media;
    currentMediaIndex = Number.isInteger(state.index) ? state.index : currentMediaIndex;
    loadMedia(state.media, state.currentTime || 0);
    const m = currentMediaElement();
    if (m) {
      if (typeof state.volume === 'number') m.volume = state.volume;
      m.muted = !!state.muted;
      if (state.playing) m.play().catch(() => {});
    }
  }
}

function readDetachedWindowState(popup) {
  if (!popup || popup.closed) return null;
  try {
    if (typeof popup.__playerDetachCurrentState === 'function') {
      return popup.__playerDetachCurrentState();
    }
  } catch {}
  return null;
}

function sendDetachedLoad(media, resumeFrom = 0, play = true) {
  const msg = detachedLoadMessage(media, resumeFrom, play);
  pendingDetachMessage = msg;
  postToDetach(msg);
}

function detachedLoadMessage(media, resumeFrom = 0, play = true) {
  const local = localPlaybackState();
  return {
    type: 'detach-load',
    media,
    index: currentMediaIndex,
    streamUrl: `/api/media/${media.id}/stream`,
    thumbnailUrl: media.thumbnail_path ? `/api/media/${media.id}/thumbnail` : '',
    resumeFrom,
    play,
    volume: detachedPlaybackState?.volume ?? local.volume,
    muted: detachedPlaybackState?.muted ?? local.muted,
  };
}

function postToDetach(message) {
  if (!detachWindow || detachWindow.closed) return;
  if (!detachReady && message.type !== 'detach-request-state') {
    pendingDetachMessage = message;
    return;
  }
  detachWindow.postMessage(message, window.location.origin);
}

function localPlaybackState() {
  const m = currentMediaElement();
  return {
    media: currentMedia,
    index: currentMediaIndex,
    currentTime: m?.currentTime || 0,
    duration: m?.duration || currentMedia?.duration || 0,
    playing: !!m && !m.paused,
    volume: m?.volume ?? 1,
    muted: !!m?.muted,
  };
}

function showDetachedPlaceholder(show) {
  const bar = document.getElementById('detached-bar');
  if (!bar) return;
  bar.classList.toggle('hidden', !show);
}

window.addEventListener('message', (ev) => {
  if (ev.origin !== window.location.origin) return;
  if (!ev.data || typeof ev.data !== 'object') return;
  if (ev.data.type === 'detach-ready') {
    onDetachReady(ev.source);
  } else if (ev.data.type === 'detach-closing') {
    onDetachClosing(ev.data.state);
  } else if (ev.data.type === 'detach-state') {
    detachedPlaybackState = ev.data.state;
  } else if (ev.data.type === 'detach-prev') {
    triggerPrevious({ forcePlay: ev.data.play ?? true });
  } else if (ev.data.type === 'detach-next') {
    triggerNext({ forcePlay: ev.data.play ?? true });
  }
});

function fmt(s) {
  if (!isFinite(s) || s < 0) return '0:00';
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  const mm = String(m).padStart(2, '0');
  const ss = String(sec).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}
