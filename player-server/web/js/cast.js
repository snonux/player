import { mediaTitle } from './music.js';
// Casting to a Chromecast (or another Google Cast receiver) with Google's Cast
// Web Sender SDK. The SDK only exists in Chrome and Chromium-based browsers
// and only on a secure page (HTTPS or localhost); elsewhere the cast button
// stays hidden and nothing is loaded.
//
// The receiver is Google's Default Media Receiver. It fetches the media
// itself and cannot send the browser's session cookie, so every item is
// cast from a ticket URL (POST /api/media/{id}/cast, see docs/api.md). The
// cast device must reach the server at this page's address.
//
// While a cast session is active, playback.js routes play/pause, seeking and
// item changes here instead of to the local <video>/<audio>; when the session
// ends the local player continues, paused, where the cast device stopped.

import { API } from './api.js';

export const SDK_URL = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';

// How often the playback position is saved while casting, like the local
// player does (playback.js).
const PROGRESS_INTERVAL_MS = 3000;

let deps = {};
let context = null;
let remotePlayer = null;
let remoteController = null;
let castMediaId = null;
let progressTimer = null;
// Latest receiver position in seconds: saved as progress and handed back to
// the local player when the session ends.
let lastRemoteTime = 0;

// castSupported tells whether the Cast SDK can work in this browser: a
// Chromium browser (window.chrome) on a secure page.
export function castSupported(win = window) {
  return !!win?.chrome && !!win.isSecureContext;
}

// castStreamUrl is the absolute URL the receiver plays: the compatibility
// rendition for items the server flags as transcoded, the original otherwise.
export function castStreamUrl(origin, ticket, transcoded) {
  return `${origin}/cast/${encodeURIComponent(ticket)}/${transcoded ? 'compat' : 'stream'}`;
}

// castLoadFields describes what the receiver is told about media: plain data,
// turned into SDK objects by loadOnReceiver.
export function castLoadFields(media, ticket, origin) {
  const transcoded = !!media.transcoded;
  return {
    url: castStreamUrl(origin, ticket.ticket, transcoded),
    contentType: transcoded ? ticket.compat_content_type : ticket.stream_content_type,
    title: mediaTitle(media) || 'Player',
    artist: media.music?.artist || media.music?.artists?.join(', ') || '',
    album: media.music?.album || '',
    imageUrl: media.thumbnail_path ? `${origin}/cast/${encodeURIComponent(ticket.ticket)}/thumbnail` : '',
    isVideo: media.type === 'video',
  };
}

// initCast loads the SDK when it can work. options:
//   button          the cast button, shown once cast devices are available
//   getCurrentMedia current library item (null when nothing is loaded)
//   localPosition   current local playback position in seconds
//   pauseLocal      pauses local playback before the cast takes over
//   onRemoteTime    (seconds, duration) while the receiver plays
//   onRemotePlaying (playing) when the receiver plays or pauses
//   onRemoteEnded   the item finished on the receiver
//   onCastEnded     (mediaId, seconds) when the session ended
//   onCastStarted   (deviceName) when an item started on the receiver
//   onError         (message) when casting failed
export function initCast(options = {}, win = window, doc = document) {
  deps = options;
  if (!castSupported(win) || !options.button) return false;
  options.button.addEventListener('click', toggleCastSession);
  win.__onGCastApiAvailable = (available) => {
    if (available) setUpContext(win);
  };
  const script = doc.createElement('script');
  script.src = SDK_URL;
  script.async = true;
  doc.head.appendChild(script);
  return true;
}

function setUpContext(win) {
  const framework = win.cast?.framework;
  const chromeCast = win.chrome?.cast;
  if (!framework || !chromeCast) return;
  context = framework.CastContext.getInstance();
  context.setOptions({
    receiverApplicationId: chromeCast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
    autoJoinPolicy: chromeCast.AutoJoinPolicy.ORIGIN_SCOPED,
  });
  remotePlayer = new framework.RemotePlayer();
  remoteController = new framework.RemotePlayerController(remotePlayer);

  const castState = framework.CastContextEventType.CAST_STATE_CHANGED;
  context.addEventListener(castState, (ev) => updateButton(ev.castState, framework));
  updateButton(context.getCastState(), framework);

  context.addEventListener(framework.CastContextEventType.SESSION_STATE_CHANGED, (ev) => {
    const S = framework.SessionState;
    if (ev.sessionState === S.SESSION_STARTED) castCurrent();
    if (ev.sessionState === S.SESSION_ENDED) handBack();
  });

  const E = framework.RemotePlayerEventType;
  remoteController.addEventListener(E.CURRENT_TIME_CHANGED, () => {
    if (!isCasting()) return;
    lastRemoteTime = remotePlayer.currentTime || 0;
    deps.onRemoteTime?.(lastRemoteTime, remotePlayer.duration || 0);
  });
  remoteController.addEventListener(E.IS_PAUSED_CHANGED, () => {
    if (isCasting()) deps.onRemotePlaying?.(!remotePlayer.isPaused);
  });
  remoteController.addEventListener(E.PLAYER_STATE_CHANGED, () => {
    if (!isCasting()) return;
    const state = remotePlayer.playerState;
    const idle = win.chrome.cast.media.PlayerState.IDLE;
    // The Default Media Receiver reports IDLE with no media once an item ends.
    if (state === idle && !remotePlayer.isMediaLoaded && lastRemoteTime > 0) {
      reportProgress(true);
      // Nothing left to save or to resume from for this item.
      lastRemoteTime = 0;
      deps.onRemoteEnded?.();
    }
  });
}

function updateButton(state, framework) {
  const button = deps.button;
  if (!button) return;
  const S = framework.CastState;
  button.classList.toggle('hidden', state === S.NO_DEVICES_AVAILABLE);
  const connected = state === S.CONNECTED;
  button.classList.toggle('active', connected);
  button.setAttribute('aria-pressed', connected ? 'true' : 'false');
  button.setAttribute('aria-label', connected ? 'Stop casting' : 'Cast to a device');
}

// isCasting tells whether playback currently runs on a cast device.
export function isCasting() {
  return castMediaId !== null && !!context?.getCurrentSession();
}

function toggleCastSession() {
  if (!context) return;
  if (context.getCurrentSession()) {
    stopCasting();
    return;
  }
  // Picking a device starts the session; SESSION_STARTED then casts.
  context.requestSession().catch(() => {});
}

// castCurrent casts the current item from the local position.
function castCurrent() {
  const media = deps.getCurrentMedia?.();
  if (!media || media.type === 'image') return;
  const position = deps.localPosition?.() || 0;
  deps.pauseLocal?.();
  castItem(media, position);
}

// castItem plays media on the receiver from resumeFrom seconds. Used for the
// item the session started with and for every item selected afterwards.
export async function castItem(media, resumeFrom = 0) {
  const session = context?.getCurrentSession();
  if (!session) return false;
  try {
    const ticket = await API.castTicket(media.id);
    const fields = castLoadFields(media, ticket, window.location.origin);
    await loadOnReceiver(session, fields, resumeFrom);
    castMediaId = media.id;
    lastRemoteTime = resumeFrom;
    startProgressTimer();
    deps.onCastStarted?.(session.getCastDevice?.()?.friendlyName || 'the cast device');
    return true;
  } catch (err) {
    console.error('cast failed:', err);
    deps.onError?.(`Cannot cast ${mediaTitle(media) || 'this file'}: the cast device could not load it`);
    return false;
  }
}

function loadOnReceiver(session, fields, resumeFrom) {
  const media = window.chrome.cast.media;
  const info = new media.MediaInfo(fields.url, fields.contentType);
  info.streamType = media.StreamType.BUFFERED;
  info.metadata = fields.isVideo ? new media.GenericMediaMetadata() : new media.MusicTrackMediaMetadata();
  info.metadata.title = fields.title;
  if (!fields.isVideo) { info.metadata.artist = fields.artist; info.metadata.albumName = fields.album; }
  if (fields.imageUrl) info.metadata.images = [new window.chrome.cast.Image(fields.imageUrl)];
  const request = new media.LoadRequest(info);
  request.currentTime = resumeFrom;
  request.autoplay = true;
  return session.loadMedia(request);
}

export function castTogglePlay() {
  remoteController?.playOrPause();
}

// castSeek moves the receiver to seconds, clamped to the item.
export function castSeek(seconds) {
  if (!remotePlayer || !remoteController) return;
  const duration = remotePlayer.duration || Infinity;
  remotePlayer.currentTime = Math.max(0, Math.min(duration, seconds));
  remoteController.seek();
}

export function castPosition() {
  return remotePlayer?.currentTime || 0;
}

export function castDuration() {
  return remotePlayer?.duration || 0;
}

// stopCasting ends the session and the playback on the receiver.
export function stopCasting() {
  context?.endCurrentSession(true);
}

// handBack runs when the session ended, from here or from another sender.
function handBack() {
  if (castMediaId === null) return;
  reportProgress(false);
  const mediaId = castMediaId;
  castMediaId = null;
  stopProgressTimer();
  deps.onCastEnded?.(mediaId, lastRemoteTime);
}

function startProgressTimer() {
  stopProgressTimer();
  progressTimer = setInterval(() => reportProgress(false), PROGRESS_INTERVAL_MS);
}

function stopProgressTimer() {
  clearInterval(progressTimer);
  progressTimer = null;
}

function reportProgress(finished) {
  if (castMediaId === null) return;
  if (finished) {
    API.progressStatus(castMediaId, 'finished').catch(() => {});
    return;
  }
  if (remotePlayer && !remotePlayer.isPaused && lastRemoteTime > 0) {
    API.progress(castMediaId, lastRemoteTime).catch(() => {});
  }
}
