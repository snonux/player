const key = 'player-navigation';
export const musicSections = ['albums', 'artists', 'playlists'];
export function normalizeNavigation(value) {
  const visible = Object.fromEntries(musicSections.map(name => [name, value?.visible?.[name] !== false]));
  const landing = musicSections.includes(value?.landing) && visible[value.landing] ? value.landing : 'library';
  return { landing, visible };
}
export function readNavigationPreferences() {
  try { return normalizeNavigation(JSON.parse(localStorage.getItem(key))); }
  catch { return normalizeNavigation(); }
}
export function coldLanding(preferences, { search = '', hash = '', historyNavigation = false } = {}) {
  return search || hash || historyNavigation ? 'library' : normalizeNavigation(preferences).landing;
}
function openMusicView(openCollection, name) {
  document.getElementById('settings-modal')?.classList.remove('open');
  document.addEventListener('library:navigated', () => {
    const target = document.querySelector('#media-grid .selected, #media-grid button, #media-grid [tabindex="0"]');
    (target || document.getElementById('settings-toggle'))?.focus();
  }, { once: true });
  openCollection(name);
}
export function initNavigationPreferences(openCollection) {
  let value = readNavigationPreferences();
  function render() {
    for (const name of musicSections) {
      const shortcut = document.getElementById(`music-${name}`);
      if (shortcut) shortcut.hidden = !value.visible[name];
      const input = document.getElementById(`navigation-${name}`);
      if (input) input.checked = value.visible[name];
    }
    const select = document.getElementById('navigation-landing');
    if (select) select.value = value.landing;
  }
  function save(next) {
    value = normalizeNavigation(next);
    render();
    const status = document.getElementById('navigation-status');
    try {
      localStorage.setItem(key, JSON.stringify(value));
      status.textContent = 'Navigation saved on this device.';
    } catch {
      status.textContent = 'Navigation changed for this window. Device storage is unavailable; it may reset when you reopen Player.';
    }
  }
  document.getElementById('navigation-landing')?.addEventListener('change', e => save({ ...value, landing: e.target.value, visible: { ...value.visible, [e.target.value]: true } }));
  for (const name of musicSections) {
    document.getElementById(`navigation-${name}`)?.addEventListener('change', e => save({ ...value, visible: { ...value.visible, [name]: e.target.checked } }));
  }
  document.querySelectorAll('[data-music-open]').forEach(btn => {
    const name = btn.getAttribute('data-music-open');
    if (!musicSections.includes(name)) return;
    btn.addEventListener('click', () => openMusicView(openCollection, name));
  });
  document.getElementById('navigation-reset')?.addEventListener('click', () => save(normalizeNavigation()));
  window.addEventListener('storage', e => { if (e.key === key || e.key === null) { value = readNavigationPreferences(); render(); } });
  render();
}
