import { API } from './api.js';
import { state, setMedia } from './state.js';

let currentMedia = null;
let isPlaying = false;
let progressTimer = null;
let progressInterval = null;
let currentMediaIndex = -1;

const els = () => ({
  video: document.getElementById('media-video'),
  audio: document.getElementById('media-audio'),
  player: document.getElementById('player'),
  btnPlay: document.getElementById('btn-play'),
  btnPrev: document.getElementById('btn-prev'),
  btnNext: document.getElementById('btn-next'),
  btnMute: document.getElementById('btn-mute'),
  btnFs: document.getElementById('btn-fullscreen'),
  btnToggleStage: document.getElementById('btn-toggle-stage'),
  bigPlay: document.getElementById('big-play'),
  coverArt: document.getElementById('cover-art'),
  track: document.getElementById('progress-track'),
  fill: document.getElementById('progress-fill'),
  thumb: document.getElementById('progress-thumb'),
  volume: document.getElementById('volume-slider'),
  timeElapsed: document.getElementById('time-elapsed'),
  timeTotal: document.getElementById('time-total'),
});

export function initPlayer() {
  const e = els();
  if (!e.video || !e.audio) return;

  e.btnPlay?.addEventListener('click', togglePlay);
  e.btnPrev?.addEventListener('click', playPrevious);
  e.btnNext?.addEventListener('click', playNext);
  e.btnMute?.addEventListener('click', toggleMute);
  e.btnFs?.addEventListener('click', toggleFullscreen);
  e.btnToggleStage?.addEventListener('click', toggleStage);
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
    });
    m.addEventListener('loadedmetadata', () => {
      e.timeTotal.textContent = fmt(isFinite(m.duration) ? m.duration : 0);
    });
    m.addEventListener('ended', () => {
      updateUI(false);
      isPlaying = false;
      stopProgressTimer();
      playNext();
    });
    m.addEventListener('play', () => { updateUI(true); isPlaying = true; startProgressTimer(); });
    m.addEventListener('pause', () => { updateUI(false); isPlaying = false; stopProgressTimer(); });
  });

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

export function togglePlay() {
  const m = currentMediaElement();
  if (!m) return;
  if (m.paused) { m.play().catch(() => {}); } else { m.pause(); }
}

export function selectAndPlay(media, index, resumeFrom = 0) {
  currentMedia = media;
  currentMediaIndex = index ?? -1;
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

function loadMedia(media, resumeFrom = 0) {
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
    e.video.currentTime = resumeFrom;
  } else {
    e.audio.pause();
    e.video.pause(); e.video.src = '';
    e.video.style.display = 'none';
    e.audio.style.display = 'none';   /* keep playing but invisible so cover-art fills stage */
    e.audio.src = src;
    e.audio.currentTime = resumeFrom;
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
  // Reset progress visual
  e.fill.style.width = '0%';
  e.thumb.style.left = '0%';
}

function currentMediaElement() {
  const e = els();
  if (currentMedia?.type === 'audio') return e.audio;
  return e.video;
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

export function toggleStage() {
  const e = els();
  e.player?.classList.toggle('collapsed');
  console.log('toggleStage: collapsed =', e.player?.classList.contains('collapsed'));
}

export function exitFullscreenIfNeeded() {
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {});
    els().player?.classList.remove('is-fullscreen');
  }
}

function startProgressTimer() {
  stopProgressTimer();
  // Report every 3s while playing
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
  const idx = currentMediaIndex;
  if (idx >= 0) {
    const cards = Array.from(document.querySelectorAll('.media-card, .media-row'));
    if (cards[idx]) cards[idx].classList.add('playing');
  }
}

export function playPrevious() {
  const list = state.media;
  if (!list.length) return;
  const idx = currentMediaIndex > 0 ? currentMediaIndex - 1 : list.length - 1;
  selectAndPlay(list[idx], idx);
}

export function playNext() {
  const list = state.media;
  if (!list.length) return;
  const idx = currentMediaIndex >= 0 && currentMediaIndex + 1 < list.length ? currentMediaIndex + 1 : 0;
  selectAndPlay(list[idx], idx);
}

export function currentMediaId() { return currentMedia?.id; }

function fmt(s) {
  if (!isFinite(s) || s < 0) return '0:00';
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  const mm = String(m).padStart(2, '0');
  const ss = String(sec).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}
