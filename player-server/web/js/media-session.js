import { mediaTitle } from './music.js';

// One owner per actual playback window. OS clients receive the browser's
// decoded/cached image; they never need a library session cookie themselves.
export function createMediaSession({ session, Metadata, getElement, ownsSource, play, pause, previous, next,
  fetchImage = (url, options) => fetch(url, options), urls = URL }) {
  let epoch = 0, media = null, artwork = '', image = null;
  const revoke = () => { if (image) urls.revokeObjectURL(image); image = null; };
  function clear() {
    epoch++; media = null; artwork = ''; revoke();
    if (!session) return;
    session.metadata = null;
    session.playbackState = 'none';
    try { session.setPositionState?.(); } catch (_) { /* unsupported */ }
    for (const action of ['play', 'pause', 'previoustrack', 'nexttrack', 'seekbackward', 'seekforward', 'seekto']) {
      try { session.setActionHandler(action, null); } catch (_) { /* unsupported action */ }
    }
  }
  function publish() {
    if (!session || !Metadata || !media) return;
    session.metadata = new Metadata({ title: mediaTitle(media),
      artist: media.music?.artist || media.music?.artists?.join(', ') || 'Unknown artist',
      album: media.music?.album || '', artwork: image ? [{ src: image }] : [] });
  }
  function refresh() {
    if (!media || !session) return;
    if (!ownsSource()) { clear(); return; }
    const element = getElement();
    session.playbackState = element && !element.paused && !element.ended && !element.error ? 'playing' : 'paused';
    const duration = element?.duration;
    try {
      if (!media.live_radio && Number.isFinite(duration) && duration > 0) session.setPositionState?.({ duration,
        playbackRate: element.playbackRate || 1, position: Math.max(0, Math.min(duration, element.currentTime || 0)) });
      else session.setPositionState?.();
    } catch (_) { /* An unsupported position API must not interrupt audio. */ }
  }
  function select(value, art = '') {
    clear();
    if (!session || !Metadata || value?.type !== 'audio' || !ownsSource()) return;
    media = value; artwork = art; const current = epoch;
    publish();
    const guarded = callback => details => { if (current === epoch && ownsSource()) callback(details); };
    const seek = delta => { const el = getElement(); if (el) el.currentTime = Math.max(0, Math.min(el.duration || Infinity, el.currentTime + delta)); };
    const actions = media.live_radio ? { play, pause } : { play, pause, previoustrack: previous, nexttrack: next,
      seekbackward: d => seek(-(d.seekOffset || 15)), seekforward: d => seek(d.seekOffset || 15),
      seekto: d => { const el = getElement(); if (el && Number.isFinite(d.seekTime)) el.currentTime = Math.max(0, Math.min(el.duration || Infinity, d.seekTime)); } };
    for (const [action, callback] of Object.entries(actions)) {
      try { session.setActionHandler(action, callback ? guarded(callback) : null); } catch (_) { /* platform action support differs */ }
    }
    refresh();
    if (artwork) void (async () => {
      try {
        const response = await fetchImage(artwork, { credentials: 'same-origin' });
        if (!response.ok) return;
        const blob = await response.blob();
        if (current !== epoch || !ownsSource() || !blob.type.startsWith('image/')) return;
        image = urls.createObjectURL(blob); publish();
      } catch (_) { /* Artwork is optional; keep this track's text without old art. */ }
    })();
  }
  return { select, refresh, clear };
}
