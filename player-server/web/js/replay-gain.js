export const LOUDNESS_MODES = ['off', 'track', 'album', 'automatic'];
export const loudnessKey = 'player-loudness-v1';
export function loudnessSettings(value = {}) {
  const bounded = n => Number.isFinite(n) ? Math.max(-12, Math.min(12, n)) : 0;
  return { mode: LOUDNESS_MODES.includes(value?.mode) ? value.mode : 'off',
    preamp: bounded(value?.preamp), untagged: bounded(value?.untagged) };
}
export function sameAlbum(items) {
  const text = value => typeof value === 'string' ? value.trim() : '';
  const normalized = value => text(value).split(/\s+/u).join(' ').toLowerCase();
  const identity = item => {
    const m = item?.music;
    if (!m) return null;
    const mbid = text(m.release_mbid);
    if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(mbid)) return `mbid:${mbid.toLowerCase()}`;
    const album = normalized(m.album);
    const artist = normalized(text(m.album_artist) || (m.compilation === true ? 'Various Artists' : m.artist));
    // Folder-derived album identity needs source/set context that isn't carried
    // by every playback request. Unknown identity safely selects track mode.
    return album && artist ? JSON.stringify([album, artist]) : null;
  };
  const first = identity(items[0]);
  return items.length > 1 && first !== null && items.every(item => identity(item) === first);
}
export function replayGain(music, settings, albumContext = false) {
  const prefs = loudnessSettings(settings);
  if (prefs.mode === 'off') return { db: 0, multiplier: 1, message: 'Loudness off.' };
  const mode = prefs.mode === 'automatic' ? albumContext ? 'album' : 'track' : prefs.mode;
  const gain = music?.[`replaygain_${mode}_gain_db`];
  const peak = music?.[`replaygain_${mode}_peak`];
  const tagged = Number.isFinite(gain);
  const requested = tagged ? gain + prefs.preamp : prefs.untagged;
  const knownPeak = Number.isFinite(peak) && peak > 0;
  // Untagged material has no trustworthy corresponding peak; never boost it.
  const ceiling = tagged && knownPeak ? -20 * Math.log10(peak) : 0;
  const db = Math.min(24, requested, ceiling);
  return { db, multiplier: Math.pow(10, db / 20), message:
    `${tagged ? `${mode === 'album' ? 'Album' : 'Track'} tags` : 'No selected gain tag'} · ${db.toFixed(1)} dB.` +
    (requested > db ? ' Gain limited to avoid clipping.' : '') +
    (!knownPeak || !tagged ? ' Peak unknown; positive boost disabled.' : '') };
}

export function createLoudnessControls(controls, changed) {
  let settings;
  try { settings = loudnessSettings(JSON.parse(localStorage.getItem(loudnessKey))); } catch { settings = loudnessSettings(); }
  const panel = document.createElement('div');
  panel.className = 'loudness-control hidden';
  panel.innerHTML = `<label>Loudness <select aria-label="Loudness">${LOUDNESS_MODES.map(mode => `<option value="${mode}">${mode[0].toUpperCase() + mode.slice(1)}</option>`).join('')}</select></label><p>Balance song volume using embedded ReplayGain tags. Local playback only; Cast uses receiver volume.</p><details><summary>Advanced loudness</summary><label>Preamp (dB) <input aria-label="Loudness preamp in decibels" type="number" min="-12" max="12" step="1"></label><label>Untagged gain (dB) <input aria-label="Untagged gain in decibels" type="number" min="-12" max="12" step="1"></label></details><span role="status"></span>`;
  const select = panel.querySelector('select'), inputs = panel.querySelectorAll('input');
  const status = panel.querySelector('[role="status"]'), details = panel.querySelector('details');
  const render = () => { select.value = settings.mode; inputs[0].value = settings.preamp; inputs[1].value = settings.untagged; };
  render();
  function set(value, { persist = true, announce = true } = {}) {
    settings = loudnessSettings(value);
    render();
    if (persist) { try { localStorage.setItem(loudnessKey, JSON.stringify(settings)); } catch {} }
    if (announce) changed();
  }
  panel.addEventListener('change', () => set({ mode: select.value, preamp: Number(inputs[0].value), untagged: Number(inputs[1].value) }));
  globalThis.addEventListener?.('storage', event => {
    if (event.key !== loudnessKey && event.key !== null) return;
    try { set(JSON.parse(event.newValue), { persist: false }); } catch { set({}, { persist: false }); }
  });
  details.addEventListener('keydown', event => {
    if (event.target.tagName === 'SUMMARY' && ['Enter', ' '].includes(event.key)) event.stopPropagation();
    if (event.key === 'Escape' && details.open) {
      event.preventDefault(); event.stopPropagation(); details.open = false; details.querySelector('summary').focus();
    }
  });
  const retry = document.createElement('button');
  retry.type = 'button'; retry.className = 'btn btn-sm'; retry.textContent = 'Retry loudness';
  retry.addEventListener('click', changed); panel.appendChild(retry);
  controls.appendChild(panel);
  return { get settings() { return settings; }, set, update(media, message) {
    panel.classList.toggle('hidden', media?.type !== 'audio'); status.textContent = message;
    retry.hidden = settings.mode === 'off';
  } };
}
