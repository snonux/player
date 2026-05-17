import { initPlayer, loadMediaDirect } from './player.js';

const openerOrigin = window.location.origin;
let currentMedia = null;
let currentIndex = -1;
let progressInterval = null;
let lastStateSent = 0;
let lastKnownPosition = 0;

initPlayer();

const video = document.getElementById('media-video');
const audio = document.getElementById('media-audio');
const btnPlay = document.getElementById('btn-play');
const bigPlay = document.getElementById('big-play');
const btnPrev = document.getElementById('btn-prev');
const btnNext = document.getElementById('btn-next');

btnPrev?.addEventListener('click', () => post({ type: 'detach-prev' }));
btnNext?.addEventListener('click', () => post({ type: 'detach-next' }));
window.__playerDetachCurrentState = currentState;

[video, audio].forEach((el) => {
  el?.addEventListener('play', () => {
    sendState();
    startProgress();
  });
  el?.addEventListener('pause', () => {
    sendState();
    stopProgress();
  });
  el?.addEventListener('timeupdate', () => {
    lastKnownPosition = el.currentTime || lastKnownPosition;
    const now = Date.now();
    if (now - lastStateSent > 500) {
      sendState();
      lastStateSent = now;
    }
  });
  el?.addEventListener('seeked', () => {
    lastKnownPosition = el.currentTime || 0;
    sendState();
  });
  el?.addEventListener('ended', () => post({ type: 'detach-next' }));
});

document.addEventListener('keydown', (ev) => {
  const tag = ev.target?.tagName;
  const editing = tag === 'INPUT' || tag === 'TEXTAREA' || ev.target?.isContentEditable;
  if (editing || ev.ctrlKey || ev.metaKey || ev.altKey) return;

  const imageDelta = fullscreenImageDelta(ev);
  if (imageDelta) {
    ev.preventDefault();
    post({ type: 'detach-nav', delta: imageDelta, play: true });
    return;
  }

  if (ev.shiftKey && ev.code === 'KeyN') {
    ev.preventDefault();
    post({ type: 'detach-next', play: shouldContinuePlayback() });
    return;
  }
  if (ev.shiftKey && ev.code === 'KeyP') {
    ev.preventDefault();
    post({ type: 'detach-prev', play: shouldContinuePlayback() });
    return;
  }

  if (ev.key === ' ' || ev.code === 'Space') {
    ev.preventDefault();
    togglePlayback();
  } else if (ev.key === 'f') {
    ev.preventDefault();
    toggleFullscreen();
  } else if (ev.key === 'ArrowLeft' || ev.key === 'h') {
    ev.preventDefault();
    seekRelative(ev.repeat ? -15 : -5);
  } else if (ev.key === 'ArrowRight' || ev.key === 'l') {
    ev.preventDefault();
    seekRelative(ev.repeat ? 15 : 5);
  } else if (ev.key === 'p') {
    ev.preventDefault();
    togglePlayback();
  } else if (ev.key === 'N') {
    ev.preventDefault();
    post({ type: 'detach-next', play: shouldContinuePlayback() });
  } else if (ev.key === 'P') {
    ev.preventDefault();
    post({ type: 'detach-prev', play: shouldContinuePlayback() });
  }
});

document.addEventListener('fullscreenchange', () => {
  document.getElementById('player')?.classList.toggle('is-fullscreen', !!document.fullscreenElement);
});

window.addEventListener('message', (ev) => {
  if (ev.origin !== openerOrigin || !ev.data || typeof ev.data !== 'object') return;

  switch (ev.data.type) {
    case 'detach-load':
      loadDetachedMedia(ev.data);
      break;
    case 'detach-command':
      handleCommand(ev.data);
      break;
    case 'detach-request-state':
      sendState();
      break;
  }
});

window.addEventListener('beforeunload', () => {
  sendState();
  post({ type: 'detach-closing', state: currentState() });
});

post({ type: 'detach-ready' });

function loadDetachedMedia(payload) {
  currentMedia = payload.media || null;
  currentIndex = Number.isInteger(payload.index) ? payload.index : -1;
  if (!currentMedia) return;

  const resumeFrom = Number(payload.resumeFrom || 0);
  lastKnownPosition = resumeFrom;
  loadMediaDirect(currentMedia, payload.streamUrl, payload.thumbnailUrl, resumeFrom);

  const el = mediaElement();
  if (!el) {
    sendState();
    return;
  }
  if (typeof payload.volume === 'number') el.volume = payload.volume;
  el.muted = !!payload.muted;

  seekWhenReady(el, resumeFrom);
  if (payload.play) playWhenReady(el);
  else showPlayPrompt();
  sendState();
}

function handleCommand(payload) {
  const action = payload?.action;
  if (action === 'toggle-play') {
    togglePlayback();
  } else if (action === 'pause') {
    const el = mediaElement();
    if (!el) return;
    el.pause();
  } else if (action === 'play') {
    const el = mediaElement();
    if (!el) return;
    el.play().catch(() => {});
  } else if (action === 'seek-relative') {
    seekRelative(Number(payload.seconds || 0));
  } else if (action === 'seek-percent') {
    seekPercent(Number(payload.percent || 0));
  }
}

function togglePlayback() {
  const el = mediaElement();
  if (!el) return;
  if (el.paused) el.play().catch(showPlayPrompt);
  else el.pause();
}

function seekRelative(seconds) {
  const el = mediaElement();
  if (!currentMedia || !el || !isFinite(seconds)) return;
  const upper = el.duration && isFinite(el.duration) ? el.duration : Infinity;
  el.currentTime = Math.max(0, Math.min(upper, (el.currentTime || 0) + seconds));
  lastKnownPosition = el.currentTime || 0;
  sendState();
}

function seekPercent(percent) {
  const el = mediaElement();
  if (!currentMedia || !el || !isFinite(percent)) return;
  const dur = (el.duration && isFinite(el.duration) && el.duration > 0) ? el.duration : (currentMedia?.duration || 0);
  if (!dur) return;
  const upper = el.duration && isFinite(el.duration) ? el.duration : Infinity;
  el.currentTime = Math.max(0, Math.min(upper, (el.currentTime || 0) + dur * percent));
  lastKnownPosition = el.currentTime || 0;
  sendState();
}

function toggleFullscreen() {
  const player = document.getElementById('player');
  if (!player) return;
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {});
    player.classList.remove('is-fullscreen');
  } else {
    player.requestFullscreen().catch(() => {});
    player.classList.add('is-fullscreen');
  }
}

function mediaElement() {
  if (currentMedia?.type === 'audio') return audio;
  if (currentMedia?.type === 'video') return video;
  return null;
}

function shouldContinuePlayback() {
  return currentMedia?.type === 'image' || currentState().playing;
}

function fullscreenImageDelta(ev) {
  if (currentMedia?.type !== 'image' || !document.fullscreenElement) return 0;
  switch (ev.key) {
    case 'ArrowLeft':
    case 'ArrowUp':
    case 'h':
    case 'k':
    case 'PageUp':
      return ev.key === 'PageUp' ? -10 : -1;
    case 'ArrowRight':
    case 'ArrowDown':
    case 'j':
    case 'l':
    case 'PageDown':
      return ev.key === 'PageDown' ? 10 : 1;
    default:
      return 0;
  }
}

function seekWhenReady(el, seconds) {
  if (!seconds || seconds < 0) return;
  const seek = () => {
    try {
      el.currentTime = seconds;
    } catch {}
  };
  if (el.readyState >= HTMLMediaElement.HAVE_METADATA) seek();
  else el.addEventListener('loadedmetadata', seek, { once: true });
}

function playWhenReady(el) {
  const play = () => el.play().catch(showPlayPrompt);
  if (el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) play();
  else el.addEventListener('canplay', play, { once: true });
}

function showPlayPrompt() {
  if (btnPlay) btnPlay.textContent = '\u25b6';
  bigPlay?.classList.remove('hidden');
}

function startProgress() {
  stopProgress();
  progressInterval = setInterval(() => {
    const el = mediaElement();
    if (!currentMedia || !el || el.paused) return;
    fetch('/api/progress', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ media_id: currentMedia.id, position_seconds: el.currentTime }),
    }).catch(() => {});
  }, 3000);
}

function stopProgress() {
  clearInterval(progressInterval);
  progressInterval = null;
}

function sendState() {
  post({ type: 'detach-state', state: currentState() });
}

function currentState() {
  const el = mediaElement();
  const positionReady = !el || el.readyState >= HTMLMediaElement.HAVE_METADATA;
  const position = positionReady ? (el?.currentTime || 0) : (lastKnownPosition || 0);
  return {
    media: currentMedia,
    index: currentIndex,
    currentTime: position,
    positionReady,
    duration: el?.duration || currentMedia?.duration || 0,
    playing: currentMedia?.type === 'image' || (!!el && !el.paused),
    volume: el?.volume ?? 1,
    muted: !!el?.muted,
  };
}

function post(message) {
  if (!window.opener || window.opener.closed) return;
  window.opener.postMessage(message, openerOrigin);
}
