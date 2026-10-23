// Server-mediated LAN output works in Chrome and Firefox without browser SDKs.
import { API } from './api.js';
import { saveListeningProgress, listeningOccurrenceID } from './listening-progress.js';

let deps = {};
let device = null;
let mediaId = null;
let position = 0;
let duration = 0;
let playing = false;
let timer = null;
let revision = 0;
let polling = false;
let hasPlayed = false;
let failures = 0;
let occurrence = '';
let startedAt = 0;
let pauseRevision = 0;
let needsLoad = false;
let outputSession = '';
let lastStopResult = Promise.resolve(true);
let lastStoppedDeviceID = null;
let retainedStop = null;
let stopPending = false;

async function request(path, body) {
  if (body) body = { ...body, session_id: outputSession, revision: body.revision ?? revision };
  const response = await fetch(`/api/renderers${path}`, {
    signal: AbortSignal.timeout(path ? 15000 : 10000),
    credentials: 'include', method: body ? 'POST' : 'GET',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) throw new Error('Speaker unavailable. Check the server network and try again.');
  return response.json();
}

export const rendererActive = () => device !== null && mediaId !== null;
export const rendererPosition = () => position;
export const rendererDuration = () => duration;

export function initRenderer(options, doc = document) {
  deps = options;
  outputSession ||= listeningOccurrenceID();
  if (!options.button?.after) return;
  if (!doc.querySelector('link[data-renderer-style]')) {
    const style = doc.createElement('link'); style.rel = 'stylesheet'; style.href = '/css/renderer.css'; style.dataset.rendererStyle = ''; doc.head.append(style);
  }
  const castButton = options.button;
  if (!castButton) return;
  const button = doc.createElement('button');
  button.type = 'button';
  button.className = 'icon-btn';
  button.textContent = 'Play on…';
  button.setAttribute('aria-label', 'Play on a network speaker');
  castButton.after(button);
  button.addEventListener('click', () => showPicker(button));
}

async function showPicker(opener) {
  const dialog = document.createElement('dialog');
  dialog.className = 'modal renderer-output-dialog';
  dialog.setAttribute('aria-label', 'Play on a network speaker');
  const heading = document.createElement('h3'); heading.textContent = 'Play on';
  const note = document.createElement('p'); note.setAttribute('role', 'status');
  const list = document.createElement('div'); list.dataset.devices = '';
  const close = document.createElement('button'); close.className = 'btn'; close.textContent = 'Close';
  close.onclick = () => dialog.close();
  const search = document.createElement('button'); search.className = 'btn'; search.textContent = 'Search again';
  dialog.append(heading, note, list, search, close);
  const selectedLabel = document.createElement('p');
  selectedLabel.hidden = !device;
  selectedLabel.textContent = device ? `Selected: ${device.name}` : '';
  dialog.insertBefore(selectedLabel, note);
  const local = document.createElement('button'); local.className = 'btn'; local.textContent = 'Return to this device';
  local.hidden = !device;
  local.onclick = () => { stopRenderer(); dialog.close(); };
  dialog.insertBefore(local, search);
  document.body.append(dialog);
  dialog.addEventListener('close', () => { dialog.remove(); opener.focus(); });
  // Native dialog owns Escape, focus trapping and touch dismissal. Stop app
  // shortcuts while it owns focus; leave Tab/Enter/native typing untouched.
  dialog.addEventListener('keydown', event => event.stopPropagation());
  dialog.showModal();
  search.onclick = searchDevices;
  async function searchDevices() {
    search.disabled = true; note.textContent = 'Looking for speakers on the server network…'; list.replaceChildren();
    try {
      const devices = await request('');
      if (!dialog.open) return;
      note.textContent = devices.length ? 'Choose a DLNA / UPnP speaker.' : 'No speakers found. The speaker and Player server must be on the same network.';
      for (const found of devices) {
        const button = document.createElement('button'); button.className = 'btn'; button.textContent = found.name;
        button.onclick = async () => {
          const media = deps.getCurrentMedia?.();
          if (!media || media.type !== 'audio') { note.textContent = 'Choose a server audio track first.'; return; }
          button.disabled = true;
          if ((device && device.id !== found.id) || (!device && retainedStop)) await stopRenderer();
          if (!(await lastStopResult) && lastStoppedDeviceID !== found.id) {
            note.textContent = 'The previous speaker may still be playing. Reconnect to it before switching outputs.';
            button.disabled = false;
            return;
          }
          deps.beforeRendererStart?.();
          device = found;
          selectedLabel.hidden = false; selectedLabel.textContent = `Selected: ${found.name}`; local.hidden = false;
          const selectionRevision = revision + 1;
          const ok = await rendererLoad(media, deps.localPosition?.() || 0);
          if (selectionRevision !== revision || device !== found) return;
          if (ok) dialog.close();
          else note.textContent = 'Speaker could not play this track. Check its network and format, then retry or return to this device.';
          button.disabled = false;
        };
        list.append(button);
      }
    } catch (error) { note.textContent = error.message; }
    finally { search.disabled = false; }
  }
  await searchDevices();
}

export async function rendererLoad(media, seconds = 0) {
  if (!device || !media || media.type !== 'audio') return false;
  const selected = device;
  const load = ++revision;
  const pause = pauseRevision;
  const nextOccurrence = deps.getListeningOccurrence?.() || listeningOccurrenceID();
  mediaId = media.id; position = seconds; needsLoad = false;
  deps.pauseLocal?.();
  deps.onRemotePlaying?.(false);
  clearInterval(timer);
  try {
    const loaded = await request('/control', { device_id: selected.id, action: 'load', media_id: media.id, position: seconds });
    if (load !== revision || device !== selected) return false;
    const current = deps.getCurrentMedia?.();
    if (current?.id !== media.id || (deps.getListeningOccurrence && deps.getListeningOccurrence() !== nextOccurrence)) {
      await stopRenderer();
      return false;
    }
    mediaId = media.id; position = seconds; duration = loaded.duration || media.duration || media.duration_seconds || 0;
    if (pause !== pauseRevision) await request('/control', { device_id: selected.id, action: 'pause' });
    if (load !== revision || device !== selected) return false;
    playing = pause === pauseRevision; hasPlayed = false; failures = 0; needsLoad = false;
    occurrence = nextOccurrence;
    lastStopResult = Promise.resolve(true);
    lastStoppedDeviceID = null;
    retainedStop = null;
    startedAt = Date.now();
    deps.onCastStarted?.(selected.name);
    deps.onRemotePlaying?.(playing);
    timer = setInterval(poll, 1500);
    return true;
  } catch (error) {
    if (load === revision) {
      mediaId = media.id; position = seconds; playing = false; needsLoad = true;
      deps.onRemotePlaying?.(false);
      deps.onError?.(error.message);
    }
    return false;
  }
}

async function poll() {
  if (!rendererActive() || polling) return;
  polling = true;
  const generation = revision;
  try {
    const status = await request('/control', { device_id: device.id, action: 'status' });
    if (generation !== revision) return;
    failures = 0;
    const previousPosition = position;
    position = status.position; duration = status.duration || duration;
    playing = status.state === 'PLAYING';
    if (playing) hasPlayed = true;
    deps.onRemoteTime?.(position, duration); deps.onRemotePlaying?.(playing);
    if (playing && position > 0) saveListeningProgress(mediaId, position, occurrence).catch(() => {});
    // STOPPED also denotes a failed URI: only a previously playing item
    // at its end counts as queue completion, never a format/network failure.
    if (status.state === 'STOPPED' && hasPlayed && duration > 0 && Math.max(position, previousPosition) >= duration - 3) {
      clearInterval(timer); hasPlayed = false;
      API.progressStatus(mediaId, 'finished').catch(() => {});
      deps.onRemoteEnded?.();
    } else if (status.state === 'STOPPED' && (hasPlayed || Date.now() - startedAt > 15000)) {
      clearInterval(timer);
      deps.onError?.('Speaker stopped. Reconnect or return to this device.');
    }
  } catch (error) {
    if (generation === revision && ++failures === 3) {
      clearInterval(timer); playing = false;
      deps.onRemotePlaying?.(false);
      deps.onError?.('Connection to the speaker was lost. Use Play on to reconnect or return to this device.');
    }
  } finally { polling = false; }
}

async function control(action, seconds) {
  if (!rendererActive()) return;
  try {
    await request('/control', { device_id: device.id, action, ...(seconds === undefined ? {} : { position: seconds }) });
    if (action === 'seek') position = seconds;
    if (action === 'play' || action === 'pause') playing = action === 'play';
    await poll();
  } catch (error) { deps.onError?.(error.message); }
}
export function rendererPause() { pauseRevision++; return control('pause'); }
export const rendererToggle = () => needsLoad ? rendererLoad(deps.getCurrentMedia?.(), position) : control(playing ? 'pause' : 'play');
export const rendererSeek = seconds => control('seek', Math.max(0, Math.min(duration || Infinity, seconds)));
// Returns true only when no outstanding speaker playback is known, or Stop
// was acknowledged. Failed-stop uncertainty survives the paused local handback;
// another call retries the retained device/session with a newer stop revision.
export function stopRenderer() {
  if (!device) {
    if (retainedStop && !stopPending) return sendStop({ ...retainedStop, revision: ++revision });
    return lastStopResult;
  }
  const selected = device; const id = mediaId; const seconds = position;
  ++revision; clearInterval(timer); device = null; mediaId = null;
  lastStoppedDeviceID = selected.id;
  const stopped = sendStop({ device_id: selected.id, session_id: outputSession, revision, action: 'stop' });
  deps.onCastEnded?.(id, seconds);
  return stopped;
}

function sendStop(command) {
  retainedStop = command;
  stopPending = true;
  lastStopResult = request('/control', command).then(() => {
    if (retainedStop === command) retainedStop = null;
    return true;
  }).catch(() => {
    deps.onError?.('Speaker could not be stopped. It may still be playing.');
    return false;
  }).finally(() => { stopPending = false; });
  return lastStopResult;
}
