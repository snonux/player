// Match the native Android audio choices. This is a browser preference shared
// by music and spoken audio; it never changes a Cast receiver's rate.
export const PLAYBACK_SPEEDS = [0.5, 1, 1.25, 1.5, 2];
const preferenceKey = 'player-audio-speed';
export function normalizePlaybackSpeed(value) {
  const rate = Number(value);
  return PLAYBACK_SPEEDS.includes(rate) ? rate : 1;
}

export function initPlaybackSpeed({ audio, player, ownsSource = () => true, remoteStep = () => false }) {
  const controls = player?.querySelector('.controls');
  if (!audio || !controls || !document.createElement) return null;
  const label = document.createElement('label');
  label.className = 'playback-speed-control hidden';
  const caption = document.createElement('span');
  caption.textContent = 'Speed';
  const select = document.createElement('select');
  select.id = 'playback-speed';
  select.setAttribute('aria-label', 'Playback speed');
  select.title = 'Audio speed (< slower, > faster); choose 1× Normal to reset';
  for (const rate of PLAYBACK_SPEEDS) {
    const option = document.createElement('option');
    option.value = String(rate);
    option.textContent = rate === 1 ? '1× Normal' : `${rate}×`;
    select.appendChild(option);
  }
  const status = document.createElement('span');
  status.id = 'playback-speed-status';
  status.className = 'playback-speed-status';
  status.setAttribute('role', 'status');
  select.setAttribute('aria-describedby', status.id);
  label.appendChild(caption); label.appendChild(select); label.appendChild(status);
  controls.appendChild(label);
  let rate = 1, media = null, casting = false;
  try { rate = normalizePlaybackSpeed(localStorage.getItem(preferenceKey)); } catch {}

  function apply() {
    // load() can reset playbackRate; defaultPlaybackRate also covers source
    // preparation/retry. Never acquire playback or change pause/position here.
    if (media?.type !== 'audio' || casting || !ownsSource()) return;
    audio.defaultPlaybackRate = rate;
    audio.playbackRate = rate;
    audio.preservesPitch = true;
  }
  function sync() {
    label.classList.toggle('hidden', media?.type !== 'audio');
    select.value = String(rate);
    select.disabled = casting;
    status.textContent = casting ? 'Speed unavailable while casting' : '';
  }
  function set(value, { persist = true } = {}) {
    rate = normalizePlaybackSpeed(value);
    if (persist) { try { localStorage.setItem(preferenceKey, String(rate)); } catch {} }
    apply(); sync();
  }
  function step(delta) {
    if (media?.type !== 'audio' || casting) return false;
    if (!ownsSource()) return remoteStep(delta);
    const index = Math.max(0, Math.min(PLAYBACK_SPEEDS.length - 1, PLAYBACK_SPEEDS.indexOf(rate) + delta));
    set(PLAYBACK_SPEEDS[index]);
    return true;
  }
  select.addEventListener('change', () => set(select.value));
  audio.addEventListener('loadedmetadata', apply);
  audio.addEventListener('loadstart', apply);
  globalThis.addEventListener?.('storage', event => {
    if (event.key === preferenceKey || event.key === null) {
      set(event.newValue, { persist: false });
    }
  });
  document.addEventListener('keydown', event => {
    if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.isComposing ||
        event.target?.isContentEditable || ['INPUT', 'SELECT', 'TEXTAREA'].includes(event.target?.tagName) ||
        event.target?.closest?.('#queue-panel, .modal-overlay, .audio-lyrics') || document.querySelector('.modal-overlay.open')) return;
    if ((event.key === '<' || event.key === '>') && step(event.key === '<' ? -1 : 1)) event.preventDefault();
  });
  sync();
  return {
    set, step, get: () => rate,
    setMedia(value) { media = value; apply(); sync(); },
    setCasting(value) { casting = value; apply(); sync(); },
    apply,
  };
}
