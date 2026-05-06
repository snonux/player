let deps = {};
let detachWindow = null;
let detachReady = false;
let pendingDetachMessage = null;
let detachedPlaybackState = null;

export function initDetach(options = {}) {
  deps = options;
  window.addEventListener('message', (ev) => {
    if (ev.origin !== window.location.origin) return;
    if (!ev.data || typeof ev.data !== 'object') return;
    if (ev.data.type === 'detach-ready') {
      onDetachReady(ev.source);
    } else if (ev.data.type === 'detach-closing') {
      onDetachClosing(ev.data.state);
    } else if (ev.data.type === 'detach-state') {
      detachedPlaybackState = mergeDetachedState(detachedPlaybackState, ev.data.state);
    } else if (ev.data.type === 'detach-prev') {
      deps.triggerPrevious?.({ forcePlay: ev.data.play ?? true });
    } else if (ev.data.type === 'detach-next') {
      deps.triggerNext?.({ forcePlay: ev.data.play ?? true });
    }
  });
}

export function isDetached() {
  return !!detachWindow && !detachWindow.closed;
}

export function detachedIsPlaying() {
  return !!detachedPlaybackState?.playing;
}

export function toggleDetach() {
  if (isDetached()) {
    reattachDetached();
    return;
  }
  const e = deps.els?.() || {};
  if (!e.player) return;

  const snapshot = deps.localPlaybackState?.() || {};
  const features = 'width=640,height=480,resizable=yes,scrollbars=no,status=no,location=no,menubar=no,toolbar=no';
  detachWindow = window.open('/detach.html', 'playerDetach', features);
  if (!detachWindow) return;

  detachReady = false;
  detachedPlaybackState = snapshot;
  const media = deps.getCurrentMedia?.();
  if (media) {
    pendingDetachMessage = detachedLoadMessage(media, snapshot.currentTime, snapshot.playing);
  }
  deps.currentMediaElement?.()?.pause();
  e.player.classList.add('hidden');
  showDetachedPlaceholder(true);
}

export function sendDetachedLoad(media, resumeFrom = 0, play = true) {
  const msg = detachedLoadMessage(media, resumeFrom, play);
  pendingDetachMessage = msg;
  postToDetach(msg);
}

export function postToDetach(message) {
  if (!detachWindow || detachWindow.closed) return;
  if (!detachReady && message.type !== 'detach-request-state') {
    pendingDetachMessage = message;
    return;
  }
  detachWindow.postMessage(message, window.location.origin);
}

function onDetachReady(popup) {
  if (!popup || popup !== detachWindow) return;
  detachReady = true;
  if (pendingDetachMessage) postToDetach(pendingDetachMessage);
}

function onDetachClosing(state = null) {
  if (state) detachedPlaybackState = mergeDetachedState(detachedPlaybackState, state);
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
  deps.els?.().player?.classList.remove('hidden');

  if (state?.media) {
    deps.setCurrentMediaState?.(state.media, Number.isInteger(state.index) ? state.index : undefined);
    deps.loadMedia?.(state.media, state.currentTime || 0);
    const m = deps.currentMediaElement?.();
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

function mergeDetachedState(previous, next) {
  if (!next) return previous;
  if (!previous?.media || !next.media || previous.media.id !== next.media.id) return next;
  const prevTime = Number(previous.currentTime || 0);
  const nextTime = Number(next.currentTime || 0);
  if (prevTime > 0 && nextTime === 0 && next.positionReady !== true) {
    return { ...next, currentTime: prevTime };
  }
  return next;
}

function detachedLoadMessage(media, resumeFrom = 0, play = true) {
  const local = deps.localPlaybackState?.() || {};
  return {
    type: 'detach-load',
    media,
    index: deps.getCurrentMediaIndex?.() ?? -1,
    streamUrl: `/api/media/${media.id}/stream`,
    thumbnailUrl: media.thumbnail_path ? `/api/media/${media.id}/thumbnail` : '',
    resumeFrom,
    play,
    volume: detachedPlaybackState?.volume ?? local.volume,
    muted: detachedPlaybackState?.muted ?? local.muted,
  };
}

function showDetachedPlaceholder(show) {
  const bar = document.getElementById('detached-bar');
  if (!bar) return;
  bar.classList.toggle('hidden', !show);
}
