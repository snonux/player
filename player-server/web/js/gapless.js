import { assertMusicStreamingAllowed, musicPrefetchAllowed } from './network-policy.js';
import { currentPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { mediaPlaybackUrl, waitForCompatStream } from './streamSource.js';
import { decodeGaplessSong, GaplessTransport, GAPLESS_MAX_PCM_BYTES, pcmBytes } from './gapless-transport.js';

const preference = 'player-gapless';
export function initGapless({ audio, player, graph, next, available, boundary, fallback }) {
  const controls = player?.querySelector('.controls');
  if (!controls) return null;
  const panel = document.createElement('details'); panel.className = 'loudness-control';
  const summary = document.createElement('summary'); summary.textContent = 'Gapless';
  const label = document.createElement('label');
  const checkbox = document.createElement('input'); checkbox.type = 'checkbox';
  const caption = document.createElement('span'); caption.textContent = ' Gapless music queue';
  label.appendChild(checkbox); label.appendChild(caption);
  const description = document.createElement('p');
  description.textContent = 'Prepares decoded songs for continuous playback at 1×. Keeps up to 128 MB of decoded audio, plus temporary download and decoding memory. Each song is at most 5 minutes and 32 MB to download. Other speeds preserve pitch with normal streaming. Casting, detached playback and longer or unsupported songs use normal streaming.';
  const status = document.createElement('span'); status.setAttribute('role', 'status');
  for (const element of [summary, label, description, status]) panel.appendChild(element);
  controls.appendChild(panel);
  let enabled = false, operation = 0, controller = null, nextController = null, preparedToken = null;
  try { enabled = localStorage.getItem(preference) === 'true'; } catch {}
  checkbox.checked = enabled;
  function message(reason) { status.textContent = enabled ? reason : 'Gapless off.'; }
  function reset() {
    operation++; controller?.abort(); controller = null;
    nextController?.abort(); nextController = null; preparedToken = null;
    audio.decoded?.cancelNext();
  }
  function eligible(media) {
    return media?.type === 'audio' && Number.isFinite(media.duration) && media.duration > 0 && media.duration <= 300 &&
      !/\.m4b$/i.test(media.file_name || '') && !/(^|[^a-z])audio[\s_-]*books?([^a-z]|$)/i.test(media.rel_path || '');
  }
  function reason(media) {
    if (!available()) return 'Normal streaming · gapless applies to a local Music queue.';
    if (audio.playbackRate !== 1) return 'Normal streaming · gapless requires 1×; other speeds preserve pitch.';
    if (!eligible(media)) return 'Normal streaming · gapless supports songs up to 5 minutes.';
    return '';
  }
  async function readyGraph() {
    const owner = graph();
    if (!owner?.decodedGraph) throw Error('Audio processing is unavailable in this browser');
    return owner.decodedGraph();
  }
  async function decode(media, url, signal, remaining) {
    assertMusicStreamingAllowed(media);
    if (!eligible(media)) throw Error('Next song needs normal streaming (song length or spoken audio)');
    if (/\/compat(?:\?|$)/.test(url)) {
      const result = await waitForCompatStream(url, { signal });
      if (result.state !== 'ready') throw Error(result.reason || 'Preparation cancelled');
    }
    assertMusicStreamingAllowed(media);
    return decodeGaplessSong((await readyGraph()).context, url, { signal, remaining });
  }
  async function prepare(media, url, position, signal) {
    reset();
    if (!enabled) { message(''); return null; }
    const why = reason(media);
    if (why) { message(why); return null; }
    const serial = operation;
    controller = new AbortController();
    const abort = () => controller?.abort(); signal?.addEventListener('abort', abort, { once: true });
    const local = controller;
    if (signal?.aborted) local.abort();
    message('Preparing decoded song…');
    try {
      const { context, input } = await readyGraph();
      const buffer = await decode(media, url, local.signal, GAPLESS_MAX_PCM_BYTES / 2);
      if (serial !== operation || local.signal.aborted) return null;
      const engine = new GaplessTransport({ context, input, buffer, url,
        boundary, changed: text => fallback(text), status: message,
        scheduleGain: (song, when) => graph().scheduleTrack(song, when),
        resetGain: () => graph().resetScheduledTrack() });
      engine.currentTime = position || 0;
      message('Gapless ready · preparing next song.'); return engine;
    } catch (error) {
      if (serial === operation && !local.signal.aborted) message(`Normal streaming · ${error.message}.`);
      return null;
    } finally { signal?.removeEventListener('abort', abort); }
  }
  async function refresh() {
    const engine = audio.decoded;
    if (!engine) return;
    if (!musicPrefetchAllowed()) { nextController?.abort(); engine.cancelNext(); preparedToken=null; message('Next-song preparation paused by Music network settings.'); return; }
    const wanted = next(), intent = currentPlaybackIntent();
    if (wanted?.token === preparedToken) return;
    nextController?.abort(); engine.cancelNext(); preparedToken = wanted?.token || null;
    if (!wanted) { message('Gapless · final song.'); return; }
    const local = new AbortController(); nextController = local;
    message('Gapless · preparing next song…');
    try {
      const url = mediaPlaybackUrl(wanted.media);
      const buffer = await decode(wanted.media, url, local.signal, GAPLESS_MAX_PCM_BYTES - pcmBytes(engine.buffer));
      if (local.signal.aborted || audio.decoded !== engine || !isPlaybackIntent(intent) || next()?.token !== wanted.token) return;
      if (engine.prepareNext({ ...wanted, buffer, url })) message('Gapless ready.');
    } catch (error) {
      if (!local.signal.aborted && audio.decoded === engine) message(`Next song uses normal streaming · ${error.message}.`);
    }
  }
  checkbox.addEventListener('change', () => {
    enabled = checkbox.checked;
    try { localStorage.setItem(preference, String(enabled)); } catch {}
    reset();
    if (audio.decoded && !enabled) fallback('Gapless off · normal streaming.');
    else message(enabled ? 'Gapless applies when you next open a song in the queue.' : '');
  });
  panel.addEventListener('keydown', event => {
    if (event.target === summary && ['Enter', ' '].includes(event.key)) event.stopPropagation();
    if (event.key === 'Escape' && panel.open) { event.preventDefault(); event.stopPropagation(); panel.open = false; summary.focus(); }
  });
  document.addEventListener('music-network:changed', () => { nextController?.abort(); audio.decoded?.cancelNext(); preparedToken=null; void refresh(); });
  document.addEventListener('queue:changed', () => { void refresh(); });
  document.addEventListener('loudness:changed', () => {
    const engine = audio.decoded;
    // Pausing/seek cancellation retains the prepared buffer, but its stopped
    // node and old deadline no longer represent a scheduled track boundary.
    // halt() restored the current track's gain; start() schedules the successor
    // again at its new deadline when playback resumes.
    if (engine?.next?.node && !engine.paused) graph().scheduleTrack(engine.next.media, engine.next.when);
  });
  message('Gapless applies when you open a song in the queue.');
  return { prepare, refresh, reset, message,
    wants(media) {
      if (!enabled) return false;
      const why = reason(media); if (why) message(why);
      return !why;
    } };
}
