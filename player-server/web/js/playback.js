import { currentPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { mediaTitle, musicSubtitle } from './music.js';
import { createMediaSession } from './media-session.js';
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
  panImage,
  toggleSlideshow,
  isSlideshowActive,
} from './imageViewer.js';
import {
  detachedIsPlaying,
  detachedPosition,
  initDetach,
  isDetached,
  postToDetach,
  sendDetachedLoad,
  toggleDetach,
  stopDetached,
} from './detach.js';
import { mediaErrorReason, mediaPlaybackUrl, waitForCompatStream } from './streamSource.js';
import { toast } from './utils.js';
import { initSeekPreview } from './seekPreview.js';
import { initAudioVisuals } from './audioVisuals.js';
import { initPlaybackSpeed } from './playback-speed.js';
import { initCompactControls } from './compactControls.js';
import { initKeyboardAccess } from './keyboard-access.js';
import { initLyrics, seekLyric } from './lyrics.js';
import { initPlaybackPersonal } from './playback-personal.js';
import { castItem, castPosition, castDuration, castSeek, castTogglePlay, initCast, isCasting, stopCasting } from './cast.js';

export {
  cycleCropPosition,
  isDetached,
  isSlideshowActive,
  resetZoom,
  panImage,
  shiftCropPosition,
  toggleCrop,
  toggleDetach,
  stopDetached,
  toggleSlideshow,
  zoomIn,
  zoomOut,
};

let currentMedia = null;
let isPlaying = false;
let progressInterval = null;
let progressWrites = Promise.resolve();
let currentMediaIndex = -1;
let reportProgress = true;
let sourceEpoch = 0;
let completingEpoch = null;
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
let resetSeekPreview = () => {};
let audioVisuals = null;
let lyrics = null;
let personal = null;
let osSession = null;
let playbackSpeed = null;
let currentThumbnailUrl = '';
let pausedReturnHint = false;

function currentMediaElement() {
  const e = els();
  if (currentMedia?.type === 'audio') return e.audio;
  if (currentMedia?.type === 'video') return e.video;
  return null;
}

function setCurrentMediaState(media, index) {
  currentMedia = media;
  personal?.setMedia(media);
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
  btnCast: document.getElementById('btn-cast'),
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

  if (options.personalActions) personal = initPlaybackPersonal({
    getMedia: () => currentMedia, getElement: currentMediaElement, available: () => !isDetached(),
  });

  audioVisuals = initAudioVisuals(e);
  lyrics = initLyrics({ ...e, getPosition: () => isCasting() ? castPosition() : e.audio.currentTime, onSeek: seconds => {
    if (isCasting()) {
      const duration = castDuration() || effectiveDuration();
      if (!Number.isFinite(seconds) || seconds < 0 || !Number.isFinite(duration) || duration <= 0 || seconds > duration) return false;
      castSeek(seconds);
      return true;
    }
    return !isDetached() && seekLyric(e.audio, seconds);
  } });
  playbackSpeed = initPlaybackSpeed({ ...e, ownsSource: () => !isDetached(), remoteStep: delta => {
    postToDetach({ type: 'detach-command', action: 'speed-step', delta }); return true;
  } });
  initCompactControls(e.player);
  initKeyboardAccess();
  osSession = createMediaSession({ session: globalThis.navigator?.mediaSession, Metadata: globalThis.MediaMetadata,
    getElement: currentMediaElement, ownsSource: () => !isDetached() && !isCasting() && !!currentMedia,
    play: playFromSystem, pause: pauseFromSystem,
    previous: options.onSessionPrevious || (() => triggerPrevious({ forcePlay: true })), next: options.onSessionNext || (() => triggerNext({ forcePlay: true })) });
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
    releaseLocalSession: () => osSession?.clear(),
    onPausedReattach: showPausedReturnHint,
    requestPlay,
    setPlaybackSpeed,
    setPlaybackLoudness,
    triggerPrevious,
    triggerNext,
    triggerNavigate,
  });
  document.addEventListener('queue:changed', () => {
    if (isDetached()) postToDetach({ type: 'detach-loudness-context', albumContext: currentPlaybackLoudness()?.albumContext === true });
  });
  bindVolume(e);
  [e.video, e.audio].forEach((m) => {
    bindMediaEvents(e, m);
    setupMediaDebug(m);
  });
  e.video.addEventListener('loadedmetadata', updateFloatingSize);
  document.addEventListener('fullscreenchange', () => {
    if (e.player && !document.fullscreenElement) {
      e.player.classList.remove('is-fullscreen');
      clearCropMode();
    }
  });
  bindSeekTrack(e);
  if (options.cast) bindCast(e);
  resetSeekPreview = initSeekPreview({
    track: e.track, player: e.player, video: e.video,
    getVideo: () => isDetached() ? null : currentMediaElement(),
    formatTime: fmt,
  });
}

// bindCast wires the cast button (cast.js). While casting, the local element
// stays paused and the transport controls show and steer the cast device.
function bindCast(e) {
  initCast({
    button: e.btnCast,
    getCurrentMedia: () => (isDetached() ? null : currentMedia),
    localPosition: () => currentMediaElement()?.currentTime || 0,
    pauseLocal: () => {
      cancelPendingSource();
      currentMediaElement()?.pause();
      stopProgressTimer();
      osSession?.clear();
    },
    onRemoteTime: (seconds, duration) => {
      showPosition(e, seconds, duration || effectiveDuration());
      lyrics?.update();
    },
    onRemotePlaying: (playing) => updateUI(playing),
    onRemoteEnded: () => triggerNext({ forcePlay: true, completed: true }),
    onCastStarted: (device) => {
      playbackSpeed?.setCasting(true);
      updateUI(true);
      setPlayerStatus(`Playing on ${device}`);
    },
    onCastEnded: (mediaId, seconds) => {
      playbackSpeed?.setCasting(false);
      setPlayerStatus('');
      updateUI(false);
      const m = currentMediaElement();
      if (m && currentMedia?.id === mediaId) seekWhenMetadataReady(m, seconds);
      publishLocalSession();
    },
    onError: (message) => { toast(message, 'error'); publishLocalSession(); },
  });
}

// showPosition draws seconds of duration on the seek bar and time labels.
function showPosition(e, seconds, duration) {
  if (!duration) return;
  const pct = Math.max(0, Math.min(100, (seconds / duration) * 100));
  e.fill.style.width = pct.toFixed(2) + '%';
  e.thumb.style.left = pct.toFixed(2) + '%';
  e.timeElapsed.textContent = fmt(seconds);
  e.timeTotal.textContent = fmt(duration);
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
    osSession?.refresh();
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
  m.addEventListener('ended', async () => {
    if (m !== currentMediaElement() || !m.ended) return;
    const epoch = sourceEpoch, intent = currentPlaybackIntent();
    if (completingEpoch === epoch) return;
    completingEpoch = epoch;
    const endedID = currentMedia?.id;
    updateUI(false);
    isPlaying = false;
    stopProgressTimer();
    await saveCurrentProgress({ finished: true });
    if (sourceEpoch === epoch && isPlaybackIntent(intent) && currentMedia?.id === endedID && currentMediaElement() === m && m.ended) {
      triggerNext({ forcePlay: true, completed: true });
    }
  });
  m.addEventListener('seeking', () => { sourceEpoch++; });
  m.addEventListener('play', () => { sourceEpoch++; completingEpoch = null; updateUI(true); isPlaying = true; startProgressTimer(); });
  m.addEventListener('pause', () => { updateUI(false); isPlaying = false; stopProgressTimer(); });
  m.addEventListener('playing', () => { if (m === currentMediaElement()) clearPausedReturnHint(); });
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
    if (isCasting()) {
      castSeek(Math.max(0, Math.min(1, frac)) * (castDuration() || effectiveDuration()));
      return;
    }
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
    if (isCasting() && (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight')) {
      ev.preventDefault();
      ev.stopPropagation();
      castSeek(castPosition() + (ev.key === 'ArrowLeft' ? -5 : 5));
      return;
    }
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
    h = 340;
    w = Math.max(360, Math.min(680, Math.round(h * aspect)));
  } else {
    w = 460;
    h = 340;
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
  if (isCasting()) {
    castTogglePlay();
    return;
  }
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
  if (isCasting()) {
    castSeek(castPosition() + (castDuration() || effectiveDuration()) * dp);
    return true;
  }
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
  if (isCasting()) {
    castSeek(castPosition() + amount);
    return true;
  }
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
  clearPausedReturnHint();
  osSession?.clear();
  sourceEpoch++;
  saveCurrentProgress();
  const detached = stopDetached();
  if (reportProgress && detached?.media?.id && detached.currentTime > 0) enqueueProgress(detached.media.id, detached.currentTime);
  if (isCasting()) stopCasting();
  if (reportProgress) document.dispatchEvent?.(new Event('player:closed'));
  audioVisuals?.setMedia(null);
  playbackSpeed?.setMedia(null);
  lyrics?.setMedia(null);
  resetSeekPreview();
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
  currentThumbnailUrl = '';
  updateMusicIdentity(null, '');
  personal?.setMedia(null);
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
  saveCurrentProgress();
  document.dispatchEvent?.(new CustomEvent('player:media-opened', { detail: { media } }));
  currentMedia = media;
  personal?.setMedia(media);
  currentMediaIndex = index ?? -1;
  if (isDetached()) {
    // The hidden opener still routes rate shortcuts by the popup's media kind.
    // setMedia leaves the local element untouched while the popup owns it.
    playbackSpeed?.setMedia(media);
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
  if (isCasting()) {
    // The item shows here but plays on the cast device. A compat stream
    // still being prepared is not cast until it is ready.
    cancelPendingSource();
    isPlaying = true;
    updateUI(true);
    highlightPlayingCard();
    castWhenPlayable(media, resumeFrom);
    return;
  }
  // isPlaying states the intent; a failed load resets it (failPlayback).
  isPlaying = true;
  requestPlay();
  highlightPlayingCard();
}

// Deep links restore the same authenticated library item without autoplay.
export function selectMedia(media, index = -1, resumeFrom = 0) {
  document.dispatchEvent?.(new CustomEvent('player:media-opened', { detail: { media, restored: true } }));
  currentMedia = media;
  currentMediaIndex = index;
  loadMedia(media, resumeFrom);
  isPlaying = false;
  updateUI(false);
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
  clearPausedReturnHint();
  currentThumbnailUrl = thumbnailUrl;
  sourceEpoch++;
  osSession?.clear();
  personal?.setMedia(media);
  resetSeekPreview();
  // Whatever was still being prepared belongs to the previous item.
  cancelPendingSource();
  const e = els();
  e.btnPrev?.setAttribute('aria-label', media.type === 'audio' ? 'Previous track' : 'Previous');
  e.btnNext?.setAttribute('aria-label', media.type === 'audio' ? 'Next track' : 'Next');
  if (media.type === 'image') {
    currentSource = null;
    showImage(e, source.url);
  } else {
    // Remembered so a failed load can be retried (retrySource).
    currentSource = source;
    showAudioVideo(e, media, source.url, thumbnailUrl, source.resumeFrom);
  }
  playbackSpeed?.setMedia(media);
  playbackSpeed?.setCasting(isCasting());
  audioVisuals?.setMedia(media, thumbnailUrl, state.sets.find((set) => set.id === media.set_id)?.name || '');
  lyrics?.setMedia(media, source.url);
  updateMinimizedTitle();
  updateMusicIdentity(media, thumbnailUrl);
  publishLocalSession();
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
  active.classList.toggle('hidden', !isVideo);
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
  e.coverArt.onerror = () => e.coverArt.classList.add('hidden');
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
  playbackSpeed?.apply();
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
  else if (pausedReturnHint) setPlayerStatus('Playback returned here. Press Play to resume.');
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
  const name = mediaTitle(media) || 'this file';
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

// Chromium 144 does not restore OS audio focus for a newly loaded paused
// element after a popup closes. Keep the paused intent and expose the existing
// Play control; Firefox can also resume through its OS controls. No audio is
// started just to acquire focus. Preparation and errors keep their own status.
function showPausedReturnHint() {
  pausedReturnHint = true;
  if (!pendingSource) setPlayerStatus('Playback returned here. Press Play to resume.');
  updateUI(false);
}

function clearPausedReturnHint() {
  if (!pausedReturnHint) return;
  pausedReturnHint = false;
  if (!pendingSource) setPlayerStatus('');
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
  clearPausedReturnHint();
  currentMediaElement()?.pause();
  isPlaying = false;
  stopProgressTimer();
  updateUI(false);
  toast(`Cannot play ${mediaTitle(media) || 'this file'}: ${reason}`, 'error');
  failureHandler?.();
}

// System controls are commands, not toggles. Preserve preparation intent and
// reuse Retry for a failed current source without playing an empty element.
function playFromSystem() {
  if (pendingSource) { setPendingPlay(true); return; }
  const m = currentMediaElement();
  if (!m) return;
  if (!m.getAttribute('src') || m.error) { retrySource(m); return; }
  if (m.paused || m.ended) requestPlay();
}

function pauseFromSystem() {
  if (pendingSource) setPendingPlay(false);
  currentMediaElement()?.pause();
  isPlaying = false;
  stopProgressTimer();
  updateUI(false);
}

function publishLocalSession() {
  if (!currentMedia || isDetached() || isCasting()) return;
  osSession?.select(currentMedia, currentThumbnailUrl);
}

// requestPlay starts playback of the current item, or remembers the wish
// while its compat stream is still being prepared.
export function requestPlay() {
  audioVisuals?.prepare();
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
  audioVisuals?.prepare();
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
  osSession?.refresh();
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
  resetSeekPreview();
  const e = els();
  if (!e.player || !e.player.classList.contains('open')) return;
  const willMinimize = !e.player.classList.contains('minimized');
  e.player.classList.toggle('minimized');
  if (willMinimize) exitFullscreenIfNeeded();
  updateMinimizedTitle();
}

function minimizePlayer() {
  resetSeekPreview();
  const e = els();
  if (!e.player || !e.player.classList.contains('open')) return;
  e.player.classList.add('minimized');
  exitFullscreenIfNeeded();
  updateMinimizedTitle();
}

// Unwind one visible player layer without restoring an already minimized one.
export function escapePlayerView() {
  if (document.fullscreenElement) {
    exitFullscreenIfNeeded();
    return true;
  }
  const player = els().player;
  if (!player?.classList.contains('open') || player.classList.contains('minimized') || player.classList.contains('hidden')) return false;
  if (isImageMode()) stopAndClose();
  else minimizePlayer();
  return true;
}

function updateMusicIdentity(media, thumbnailUrl) {
  const e = els();
  if (!e.player || !document.createElement) return;
  let identity = document.getElementById('player-music-identity');
  if (!identity) {
    identity = document.createElement('div'); identity.id = 'player-music-identity'; identity.className = 'player-music-identity';
    e.player.querySelector('.controls')?.before?.(identity);
  }
  identity.textContent = '';
  identity.hidden = media?.type !== 'audio';
  if (identity.hidden) {
    const art = e.btnRestore?.querySelector('.player-mini-art');
    if (art) { art.hidden = true; art.src = ''; }
    return;
  }
  const title = mediaTitle(media), subtitle = musicSubtitle(media) || 'Unknown artist';
  for (const [text, className] of [[title, 'player-song-title'], [subtitle, 'player-song-subtitle']]) {
    const line = document.createElement('div'); line.className = className; line.textContent = text; line.title = text; identity.appendChild(line);
  }
  if (e.restoreTitle) { e.restoreTitle.textContent = `Restore: ${title} · ${subtitle}`; e.restoreTitle.title = `${title} · ${subtitle}`; }
  if (e.btnRestore) {
    let art = e.btnRestore.querySelector('.player-mini-art');
    if (!art) { art = document.createElement('img'); art.className = 'player-mini-art'; art.alt = ''; e.btnRestore.prepend(art); }
    art.hidden = !thumbnailUrl; art.src = thumbnailUrl || ''; art.onerror = () => { art.hidden = true; };
  }
}

function updateMinimizedTitle() {
  const e = els();
  if (!e.restoreTitle) return;
  const title = mediaTitle(currentMedia), subtitle = musicSubtitle(currentMedia);
  e.restoreTitle.textContent = title ? `Restore: ${title}${subtitle ? ` · ${subtitle}` : ''}` : 'Restore player';
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
      enqueueProgress(currentMedia.id, m.currentTime);
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

// Capture the old source before replacing it; periodic saves alone lose the
// final seconds of short songs and manual track skips.
function saveCurrentProgress({ finished = false } = {}) {
  const element = currentMediaElement();
  if (!reportProgress || isCasting() || isDetached() || !currentMedia || !element ||
      (!finished && element.ended) || !(element.currentTime > 0)) {
    return progressWrites;
  }
  return enqueueProgress(currentMedia.id, element.currentTime, finished);
}

// Keep completion after every earlier position write. The track replacement
// must not reopen the item with another final-position write after "finished".
function enqueueProgress(id, position, finished = false) {
  progressWrites = progressWrites.catch(() => {}).then(async () => {
    await API.progress(id, position);
    if (finished) await API.progressStatus(id, 'finished');
  }).catch(() => {});
  return progressWrites;
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

// Queue recovery reads source position without acquiring or starting playback.
export function queuePlaybackPosition() {
  if (isDetached()) return detachedPosition();
  if (isCasting()) return castPosition();
  const m = currentMediaElement();
  if (pendingSource || !m?.readyState) return currentSource?.resumeFrom ?? 0;
  return m.currentTime || 0;
}

export function currentMediaId() { return currentMedia?.id; }

export function currentMediaInfo() { return currentMedia; }

// castWhenPlayable casts media once the server can stream it: at once for
// an original stream, after the compat probe for a transcoded item.
async function castWhenPlayable(media, resumeFrom) {
  if (media.transcoded) {
    setPlayerStatus(preparingText(media, ''));
    const result = await waitForCompatStream(mediaPlaybackUrl(media));
    setPlayerStatus('');
    if (currentMedia?.id !== media.id) return;
    if (result.state !== 'ready') {
      failPlayback(media, result.reason);
      return;
    }
  }
  if (currentMedia?.id === media.id && isCasting()) await castItem(media, resumeFrom);
}

export function isPlaybackActive() {
  if (isCasting()) return isPlaying;
  if (isDetached()) return detachedIsPlaying() || currentMedia?.type === 'image';
  return isPlaying || currentMedia?.type === 'image';
}

export function isImageMode() {
  return currentMedia?.type === 'image';
}

export function currentPlaybackLoudness() { return audioVisuals?.loudnessState() ?? null; }
export function setPlaybackLoudness(value, options) { audioVisuals?.setLoudnessState(value, options); }
export function setPlaybackLoudnessContext(value) { audioVisuals?.setLoudnessContext(value); }

export function currentPlaybackSpeed() { return playbackSpeed?.get() ?? 1; }

export function setPlaybackSpeed(rate) { playbackSpeed?.set(rate); }

export function stepPlaybackSpeed(delta) { return playbackSpeed?.step(delta) ?? false; }

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
    playbackRate: playbackSpeed?.get() ?? 1,
    loudness: currentPlaybackLoudness(),
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
