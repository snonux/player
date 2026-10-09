import { API, redirectToLogin } from './api.js';
import { state } from './state.js';
import {
  clearCropMode,
  initImageViewer,
  resetCropPosition,
  resetImageZoom,
  pauseSlideshow,
  stopSlideshow,
  toggleCrop,
  shiftCropPosition,
  cycleCropPosition,
  zoomIn,
  zoomOut,
  resetZoom,
  toggleSlideshow,
  isSlideshowActive,
} from './imageViewer.js';
import {
  detachedIsPlaying,
  initDetach,
  isDetached,
  postToDetach,
  sendDetachedLoad,
  toggleDetach,
} from './detach.js';
import { mediaErrorReason, mediaPlaybackUrl, waitForCompatStream } from './streamSource.js';
import { toast } from './utils.js';

export {
  cycleCropPosition,
  isDetached,
  isSlideshowActive,
  resetZoom,
  shiftCropPosition,
  toggleCrop,
  toggleDetach,
  toggleSlideshow,
  zoomIn,
  zoomOut,
};

let currentMedia = null;
let isPlaying = false;
let progressInterval = null;
let currentMediaIndex = -1;
let reportProgress = true;
let nextHandler = null;
let previousHandler = null;
// Set while the current item's compat stream is being probed and its media
// element therefore has no source yet: { controller, autoplay, announced }.
// autoplay records a play request made in the meantime.
let pendingSource = null;
// Where the current audio/video item is loaded from: { url, resumeFrom,
// library }. Kept so a failed load can be retried; null for images.
let currentSource = null;
// Optional initPlayer({ onPlaybackFailed }) callback, see failPlayback.
let failureHandler = null;

function currentMediaElement() {
  const e = els();
  if (currentMedia?.type === 'audio') return e.audio;
  if (currentMedia?.type === 'video') return e.video;
  return null;
}

function setCurrentMediaState(media, index) {
  currentMedia = media;
  if (Number.isInteger(index)) currentMediaIndex = index;
}

function effectiveDuration() {
  const m = currentMediaElement();
  if (!m) return 0;
  if (isFinite(m.duration) && m.duration > 0) return m.duration;
  return currentMedia?.duration || 0;
}

const els = () => ({
  video: document.getElementById('media-video'),
  audio: document.getElementById('media-audio'),
  player: document.getElementById('player'),
  image: document.getElementById('media-image'),
  btnPlay: document.getElementById('btn-play'),
  btnPrev: document.getElementById('btn-prev'),
  btnNext: document.getElementById('btn-next'),
  btnZoomIn: document.getElementById('btn-zoom-in'),
  btnZoomOut: document.getElementById('btn-zoom-out'),
  btnSlideshow: document.getElementById('btn-slideshow'),
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
  cropIndicator: document.getElementById('crop-indicator'),
});

export function initPlayer(options = {}) {
  nextHandler = typeof options.onNext === 'function' ? options.onNext : null;
  previousHandler = typeof options.onPrevious === 'function' ? options.onPrevious : null;
  failureHandler = typeof options.onPlaybackFailed === 'function' ? options.onPlaybackFailed : null;
  const e = els();
  if (!e.video && !e.audio) return;

  bindControlButtons(e);
  initImageViewer({ els, isImageMode, playNext: () => navigateImage(1) });
  initDetach({
    els,
    currentMediaElement,
    getCurrentMedia: () => currentMedia,
    getCurrentMediaIndex: () => currentMediaIndex,
    setCurrentMediaState,
    loadMedia,
    localPlaybackState,
    cancelPendingSource,
    requestPlay,
    triggerPrevious,
    triggerNext,
    triggerNavigate,
  });
  bindVolume(e);
  [e.video, e.audio].forEach((m) => {
    bindMediaEvents(e, m);
    setupMediaDebug(m);
  });
  e.video.addEventListener('loadedmetadata', updateFloatingSize);
  document.addEventListener('fullscreenchange', () => {
    if (e.player && !document.fullscreenElement) clearCropMode();
  });
  bindSeekTrack(e);
}

function bindControlButtons(e) {
  e.btnPlay?.addEventListener('click', togglePlay);
  e.btnPrev?.addEventListener('click', () => isImageMode() ? navigateImage(-1, { manual: true }) : triggerPrevious({ forcePlay: true }));
  e.btnNext?.addEventListener('click', () => isImageMode() ? navigateImage(1, { manual: true }) : triggerNext({ forcePlay: true }));
  e.btnMute?.addEventListener('click', toggleMute);
  e.btnFs?.addEventListener('click', toggleFullscreen);
  e.btnMinimize?.addEventListener('click', minimizePlayer);
  e.btnRestore?.addEventListener('click', toggleMinimize);
  e.bigPlay?.addEventListener('click', togglePlay);
  // The big play overlay handles its own activation keys; stopping
  // propagation keeps the global Space/Enter shortcuts from toggling again
  // or opening the selected grid item.
  e.bigPlay?.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter' || ev.key === ' ') {
      ev.preventDefault();
      ev.stopPropagation();
      togglePlay();
    }
  });
}

function bindVolume(e) {
  e.volume?.addEventListener('input', () => {
    const v = parseFloat(e.volume.value);
    e.video.volume = v;
    e.audio.volume = v;
    e.video.muted = v === 0;
    e.audio.muted = v === 0;
    updateMuteIcon();
  });
}

// bindMediaEvents wires one <video>/<audio> element to the transport UI.
function bindMediaEvents(e, m) {
  m.addEventListener('timeupdate', () => {
    const dur = effectiveDuration();
    if (!dur) return;
    const pct = (m.currentTime / dur) * 100;
    e.fill.style.width = pct.toFixed(2) + '%';
    e.thumb.style.left = pct.toFixed(2) + '%';
    e.timeElapsed.textContent = fmt(m.currentTime);
    updateBufferedRanges(m);
  });
  m.addEventListener('loadedmetadata', () => {
    const dur = effectiveDuration();
    e.timeTotal.textContent = fmt(dur);
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
  m.addEventListener('error', () => handleElementError(m));
}

function setupMediaDebug(m) {
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

function bindSeekTrack(e) {
  let seeking = false;
  const seekToFraction = (frac) => {
    const m = currentMediaElement();
    const dur = effectiveDuration();
    if (!m || !dur) return;
    m.currentTime = Math.max(0, Math.min(1, frac)) * dur;
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
    const dur = effectiveDuration();
    if (!m || !dur) return;
    // Stop propagation so the global arrow seek does not seek a second time.
    if (ev.key === 'ArrowLeft') { ev.preventDefault(); ev.stopPropagation(); m.currentTime = Math.max(0, m.currentTime - 5); }
    if (ev.key === 'ArrowRight') { ev.preventDefault(); ev.stopPropagation(); m.currentTime = Math.min(dur, m.currentTime + 5); }
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
  if (pendingSource) {
    // Nothing can play yet: toggle whether playback starts once it can.
    setPendingPlay(!pendingSource.autoplay);
    return;
  }
  const m = currentMediaElement();
  if (!m) return;
  // No source (its compat stream failed) or an element error: play() could
  // not recover and, on an empty element, would fire `play` and report
  // position 0 as progress. Load the item again instead.
  if (!m.getAttribute('src') || m.error) {
    retrySource(m);
    return;
  }
  if (m.paused) playElement(m);
  else m.pause();
}

export function hasLoadedMedia() {
  return !!currentMedia;
}

export function seekPercent(deltaPercent) {
  const dp = Number(deltaPercent);
  if (!currentMedia || !isFinite(dp)) return false;
  if (isDetached()) {
    postToDetach({ type: 'detach-command', action: 'seek-percent', percent: dp });
    return true;
  }
  const m = currentMediaElement();
  if (!m) return false;
  const dur = effectiveDuration();
  if (!dur) return false;
  const upper = m.duration && isFinite(m.duration) ? m.duration : dur;
  m.currentTime = Math.max(0, Math.min(upper, (m.currentTime || 0) + dur * dp));
  return true;
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

export function stopAndClose() {
  cancelPendingSource();
  const m = currentMediaElement();
  if (m) m.pause();
  if (currentMedia?.type === 'image') {
    stopSlideshow();
    resetImageZoom();
  }
  exitFullscreenIfNeeded();
  const e = els();
  e.player?.classList.remove('open', 'has-image', 'minimized');
  e.video?.classList.add('hidden');
  e.audio?.classList.add('hidden');
  e.image?.classList.add('hidden');
  e.coverArt?.classList.add('hidden');
  e.bigPlay?.classList.add('hidden');
  e.track?.classList.add('hidden');
  e.timeElapsed?.classList.add('hidden');
  e.timeTotal?.classList.add('hidden');
  e.btnPlay?.classList.add('hidden');
  e.btnZoomIn?.classList.add('hidden');
  e.btnZoomOut?.classList.add('hidden');
  e.btnSlideshow?.classList.add('hidden');
  currentMedia = null;
  currentSource = null;
  currentMediaIndex = -1;
  isPlaying = false;
  stopProgressTimer();
  updateUI(false);
  highlightPlayingCard();
  if (e.fill) e.fill.style.width = '0%';
  if (e.thumb) e.thumb.style.left = '0%';
  if (e.timeElapsed) e.timeElapsed.textContent = '0:00';
  if (e.timeTotal) e.timeTotal.textContent = '0:00';
  if (e.buffered) e.buffered.style.background = 'transparent';
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
  if (media.type === 'image') {
    isPlaying = false;
    highlightPlayingCard();
    return;
  }
  // isPlaying states the intent; a failed load resets it (failPlayback).
  isPlaying = true;
  requestPlay();
  highlightPlayingCard();
}

// loadMediaDirect shows media from explicit URLs. It is used by the detached
// popup and the public share page, which do not report playback progress.
// streamUrl must be the server's playback URL (the compat stream when
// media.transcoded is true).
export function loadMediaDirect(media, streamUrl, thumbnailUrl, resumeFrom = 0) {
  currentMedia = media;
  currentMediaIndex = -1;
  reportProgress = false;
  showMedia(media, { url: streamUrl, resumeFrom, library: false }, thumbnailUrl);
  // A direct load does not play by itself: show the play prompt until the
  // caller (popup) or the user (share page) asks for playback.
  if (media.type !== 'image') updateUI(false);
}

// loadMedia shows a library item in the main window; the caller has already
// set currentMedia.
function loadMedia(media, resumeFrom = 0) {
  reportProgress = true;
  const thumbnailUrl = media.thumbnail_path ? `/api/media/${media.id}/thumbnail` : '';
  showMedia(media, { url: mediaPlaybackUrl(media), resumeFrom, library: true }, thumbnailUrl);
}

// showMedia displays media from source = { url, resumeFrom, library }.
// library marks session-authenticated library playback in the main window
// (as opposed to share and popup URLs); see finishPendingSource.
function showMedia(media, source, thumbnailUrl) {
  // Whatever was still being prepared belongs to the previous item.
  cancelPendingSource();
  const e = els();
  if (media.type === 'image') {
    currentSource = null;
    showImage(e, source.url);
  } else {
    // Remembered so a failed load can be retried (retrySource).
    currentSource = source;
    showAudioVideo(e, media, source.url, thumbnailUrl, source.resumeFrom);
  }
  updateMinimizedTitle();
}

function showImage(e, url) {
  resetImageZoom();
  e.video.pause(); e.video.src = '';
  e.audio.pause(); e.audio.src = '';
  e.video.classList.add('hidden');
  e.audio.classList.add('hidden');
  e.coverArt?.classList.add('hidden');
  e.bigPlay?.classList.add('hidden');
  e.track?.classList.add('hidden');
  e.timeElapsed?.classList.add('hidden');
  e.timeTotal?.classList.add('hidden');
  e.btnPlay?.classList.add('hidden');
  e.btnZoomIn?.classList.remove('hidden');
  e.btnZoomOut?.classList.remove('hidden');
  e.btnSlideshow?.classList.remove('hidden');
  e.image.classList.remove('hidden');
  e.image.src = url;
  e.player?.classList.add('open', 'has-image');
}

function showAudioVideo(e, media, url, thumbnailUrl, resumeFrom) {
  stopSlideshow();
  showTransportControls(e);
  const isVideo = media.type === 'video';
  const active = isVideo ? e.video : e.audio;
  const idle = isVideo ? e.audio : e.video;
  active.pause();
  idle.pause(); idle.src = '';
  // Only the hidden class toggles visibility (no inline styles). Its rule
  // lives in player.css, the one stylesheet every player page loads, so an
  // audio item also hides the empty video box on the share page.
  active.classList.remove('hidden');
  idle.classList.add('hidden');
  showCoverArt(e, isVideo ? '' : thumbnailUrl);
  e.timeTotal.textContent = fmt(media.duration ?? 0);
  e.buffered && (e.buffered.style.background = 'transparent');
  e.fill.style.width = '0%';
  e.thumb.style.left = '0%';
  startSource(active, media, url, resumeFrom);
  updateFloatingSize();
}

function showTransportControls(e) {
  e.track?.classList.remove('hidden');
  e.timeElapsed?.classList.remove('hidden');
  e.timeTotal?.classList.remove('hidden');
  e.btnPlay?.classList.remove('hidden');
  e.image?.classList.add('hidden');
  e.btnZoomIn?.classList.add('hidden');
  e.btnZoomOut?.classList.add('hidden');
  e.btnSlideshow?.classList.add('hidden');
  e.player?.classList.remove('has-image');
  e.player?.classList.add('open');
  e.btnPlay.textContent = '⏸';
  e.bigPlay?.classList.add('hidden');
}

function showCoverArt(e, thumbnailUrl) {
  if (!e.coverArt) return;
  e.coverArt.classList.toggle('hidden', !thumbnailUrl);
  e.coverArt.src = thumbnailUrl || '';
}

// startSource gives the element its source. Plain streams are assigned right
// away. A transcoded item is first probed (see streamSource.js): the element
// stays empty and the "preparing" status is shown until the server has the
// rendition, so a 503 never reaches the element as a bogus decode error.
function startSource(m, media, url, resumeFrom) {
  if (!media.transcoded) {
    attachSource(m, url, resumeFrom);
    return;
  }
  // Drop the previous item's source so readyState/currentTime are reset and
  // resume/play listeners wait for the new stream's metadata.
  m.removeAttribute('src');
  m.load();
  const pending = { controller: new AbortController(), autoplay: false, announced: false };
  pendingSource = pending;
  setPlayerStatus(preparingText(media, ''));
  waitForCompatStream(url, {
    signal: pending.controller.signal,
    onPreparing: (status) => announcePreparing(pending, media, status),
  }).then((result) => finishPendingSource(pending, m, media, url, resumeFrom, result));
}

function attachSource(m, url, resumeFrom) {
  m.src = url;
  m.load();
  seekWhenMetadataReady(m, resumeFrom);
}

// finishPendingSource runs when the probe settles. A probe that was cancelled
// or superseded by another item must not touch the element any more.
function finishPendingSource(pending, m, media, url, resumeFrom, result) {
  if (pendingSource !== pending) return;
  pendingSource = null;
  setPlayerStatus('');
  if (result.state !== 'ready') {
    failPlayback(media, result.reason);
    // An expired session ends library playback like every other API call
    // (api.js): at the login page. Share and popup URLs never redirect: a
    // share viewer has no account, and the popup must not navigate away.
    if (result.status === 401 && currentSource?.library) redirectToLogin();
    return;
  }
  attachSource(m, url, resumeFrom);
  if (pending.autoplay) playElement(m);
}

// retrySource loads the current item again after its compat stream failed or
// the element reported an error, and plays it: the play button, the big play
// prompt and Space/p all end here through togglePlay, so a failure never
// needs a page reload.
function retrySource(m) {
  if (!currentSource || !currentMedia) return;
  // Read before startSource resets the element: keep the reached position.
  const resumeFrom = m.currentTime > 0 ? m.currentTime : currentSource.resumeFrom;
  startSource(m, currentMedia, currentSource.url, resumeFrom);
  updateUI(true);
  requestPlay();
}

// cancelPendingSource stops waiting for a compat stream (item switched,
// player closed or detached), including its retry timer and open request.
function cancelPendingSource() {
  if (!pendingSource) return;
  pendingSource.controller.abort();
  pendingSource = null;
  setPlayerStatus('');
}

function preparingText(media, status) {
  const name = media?.file_name || 'this file';
  if (status === 'busy') return `Server is busy, ${name} is waiting to be prepared…`;
  return `Preparing ${name} for playback…`;
}

// announcePreparing is called before each retry wait. The stage status keeps
// showing the current state; the toast is raised only once, and exists
// because the stage is not visible while the player is minimized.
function announcePreparing(pending, media, status) {
  if (pendingSource !== pending) return;
  const text = preparingText(media, status);
  setPlayerStatus(text);
  if (!pending.announced) toast(text, 'info');
  pending.announced = true;
}

// setPlayerStatus shows text over the stage; an empty text hides it (the
// element is styled away via :empty). The element is created on demand so
// index.html, detach.html and share.html need no extra markup.
function setPlayerStatus(text) {
  let status = document.getElementById('player-status');
  if (!status) {
    const stage = els().player?.querySelector('.stage');
    if (!stage || !text) return;
    status = document.createElement('div');
    status.id = 'player-status';
    status.className = 'player-status';
    status.setAttribute('role', 'status');
    status.setAttribute('aria-live', 'polite');
    stage.appendChild(status);
  }
  status.textContent = text;
}

// handleElementError reacts to a media element's `error` event. Errors of the
// idle element are ignored: clearing its source (src = '') makes browsers
// report an "empty src" error that is not a playback failure.
function handleElementError(m) {
  if (m !== currentMediaElement() || !m.getAttribute('src')) return;
  failPlayback(currentMedia, mediaErrorReason(m.error));
}

// failPlayback tells the user why nothing plays and puts the transport back
// into the "not playing" state. pause() only fires a `pause` event when the
// element was playing, and after a failed probe it never was; the
// onPlaybackFailed callback therefore lets the detached popup report the lost
// play intent to the main window in every case.
function failPlayback(media, reason) {
  currentMediaElement()?.pause();
  isPlaying = false;
  stopProgressTimer();
  updateUI(false);
  toast(`Cannot play ${media?.file_name || 'this file'}: ${reason}`, 'error');
  failureHandler?.();
}

// requestPlay starts playback of the current item, or remembers the wish
// while its compat stream is still being prepared.
export function requestPlay() {
  if (pendingSource) {
    setPendingPlay(true);
    return;
  }
  const m = currentMediaElement();
  if (m) playElement(m);
}

// setPendingPlay records whether playback starts once the stream being
// prepared is ready, and shows that wish on the play button / big play.
function setPendingPlay(wanted) {
  pendingSource.autoplay = wanted;
  updateUI(wanted);
}

// hasPendingPlay tells whether a play request waits for a compat stream. The
// detached popup reports it as "playing" so the wish survives a reattach.
export function hasPendingPlay() {
  return !!pendingSource?.autoplay;
}

function playElement(m) {
  m.play().catch((err) => {
    console.error('play() failed:', err);
    // Autoplay can be refused when the stream became ready long after the
    // click; show the play prompt instead of a pause button that lies.
    if (err?.name === 'NotAllowedError') updateUI(false);
  });
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
    const p = els().player;
    if (p) p.classList.remove('is-fullscreen', 'crop-mode');
    resetCropPosition();
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

function triggerNavigate(delta, options = {}) {
  const step = Number(delta || 0);
  if (!step) return;
  if (step < 0 && previousHandler && step === -1) {
    previousHandler(options);
    return;
  }
  if (step > 0 && nextHandler) {
    nextHandler({ ...options, delta: step });
    return;
  }
  if (step < 0 && nextHandler) {
    nextHandler({ ...options, delta: step });
    return;
  }
  if (step < 0) playPrevious();
  else playNext();
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

export function navigateImage(delta, { manual = false } = {}) {
  if (!isImageMode()) return false;
  const images = state.media.filter((media) => media.type === 'image');
  if (!images.length) return false;
  const index = images.findIndex((media) => media.id === currentMedia?.id);
  const next = images[(index + delta + images.length) % images.length];
  if (manual) pauseSlideshow();
  selectAndPlay(next, state.media.indexOf(next));
  return true;
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
  if (isDetached()) return detachedIsPlaying() || currentMedia?.type === 'image';
  return isPlaying || currentMedia?.type === 'image';
}

export function isImageMode() {
  return currentMedia?.type === 'image';
}

function localPlaybackState() {
  const m = currentMediaElement();
  return {
    media: currentMedia,
    index: currentMediaIndex,
    currentTime: m?.currentTime || 0,
    duration: m?.duration || currentMedia?.duration || 0,
    // A play request waiting for a compat stream counts as playing, so
    // detaching while preparing keeps the wish to play.
    playing: (!!m && !m.paused) || !!pendingSource?.autoplay,
    volume: m?.volume ?? 1,
    muted: !!m?.muted,
  };
}

function fmt(s) {
  if (!isFinite(s) || s < 0) return '0:00';
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  const mm = String(m).padStart(2, '0');
  const ss = String(sec).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}
