// Optional A02a server-audio client. Local browser playback stays independent;
// selecting "This server's speakers" pauses local output like DLNA/Cast.
import { listeningOccurrenceID } from './listening-progress.js';

let config = null;
let session = '';
let revision = 0;
let active = false;
let volume = 80;
let outputID = '';
let position = 0;
let duration = 0;
let playing = false;
let mediaId = null;
let timer = null;
let deps = {};

export function jukeboxActive() {
  return active;
}

export function jukeboxPosition() {
  return position;
}

export async function initJukebox(options) {
  deps = options || {};
  session ||= listeningOccurrenceID();
  try {
    const res = await fetch('/api/config', { credentials: 'include', signal: AbortSignal.timeout(8000) });
    if (!res.ok) return;
    const body = await res.json();
    config = body.jukebox || null;
  } catch {
    config = null;
  }
}

export function jukeboxAvailable() {
  return !!(config && config.enabled && config.can_control);
}

export function jukeboxLabel() {
  return (config && config.name) || "This server's speakers";
}

async function control(action, extra = {}) {
  const body = {
    session_id: session,
    revision: action === 'load' || action === 'stop' || action === 'set_queue' || action === 'clear'
      ? ++revision
      : revision,
    action,
    ...extra,
  };
  const res = await fetch('/api/jukebox/control', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || 'Server speakers unavailable.');
  }
  return res.json();
}

export async function jukeboxLoad(media, seconds = 0) {
  if (!jukeboxAvailable() || !media || media.type !== 'audio') return false;
  deps.pauseLocal?.();
  deps.beforeRendererStart?.();
  try {
    const status = await control('load', { media_id: media.id, position: seconds || 0 });
    active = true;
    mediaId = media.id;
    position = status.position || seconds || 0;
    duration = status.duration || media.duration || 0;
    volume = status.volume ?? volume;
    outputID = status.output_id || outputID;
    playing = status.state === 'PLAYING';
    deps.onCastStarted?.(jukeboxLabel());
    deps.onRemotePlaying?.(playing);
    clearInterval(timer);
    timer = setInterval(poll, 1500);
    return true;
  } catch (error) {
    deps.onError?.(error.message);
    return false;
  }
}

async function poll() {
  if (!active) return;
  try {
    const status = await control('status');
    position = status.position;
    duration = status.duration || duration;
    playing = status.state === 'PLAYING';
    volume = status.volume ?? volume;
    deps.onRemoteTime?.(position, duration);
    deps.onRemotePlaying?.(playing);
    if (status.state === 'STOPPED' && duration > 0 && position >= duration - 3) {
      clearInterval(timer);
      deps.onRemoteEnded?.();
    }
  } catch (error) {
    clearInterval(timer);
    deps.onError?.(error.message);
  }
}

export function jukeboxPause() {
  return control('pause').then(() => { playing = false; deps.onRemotePlaying?.(false); }).catch(e => deps.onError?.(e.message));
}

export function jukeboxToggle() {
  return control(playing ? 'pause' : 'play').then(status => {
    playing = status.state === 'PLAYING';
    deps.onRemotePlaying?.(playing);
  }).catch(e => deps.onError?.(e.message));
}

export function jukeboxSeek(seconds) {
  return control('seek', { position: seconds }).then(() => { position = seconds; }).catch(e => deps.onError?.(e.message));
}

export async function jukeboxSetVolume(value) {
  const status = await control('volume', { volume: value });
  volume = status.volume;
  return volume;
}

export async function jukeboxSetOutput(id) {
  const status = await control('set_device', { output_id: id });
  outputID = status.output_id;
  return status.devices || [];
}

export function stopJukebox() {
  if (!active && revision === 0) return Promise.resolve(true);
  clearInterval(timer);
  const id = mediaId;
  const seconds = position;
  active = false;
  mediaId = null;
  const done = control('stop').then(() => true).catch(() => false);
  deps.onCastEnded?.(id, seconds);
  return done;
}

export function jukeboxVolume() {
  return volume;
}

export function jukeboxOutputID() {
  return outputID;
}
