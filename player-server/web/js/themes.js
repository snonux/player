const STORAGE_KEY = 'kiss-theme';
const choices = { mode: ['dark', 'light', 'system'], accent: ['blue', 'teal', 'plum'], shape: ['rounded', 'square', 'circle'], fit: ['contain', 'cover'] };
const defaults = { mode: 'dark', accent: 'blue', shape: 'rounded', fit: 'contain' };
let preferences = { ...defaults };
let systemTheme;

function read(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}
function keyFor(name) { return name === 'mode' ? STORAGE_KEY : `kiss-art-${name}`; }
function render() {
  const root = document.documentElement;
  const dark = preferences.mode === 'system' ? systemTheme?.matches !== false : preferences.mode === 'dark';
  root.setAttribute('data-theme', dark ? 'dark' : 'light');
  for (const name of ['accent', 'shape', 'fit']) root.setAttribute(`data-${name}`, preferences[name]);
  const btn = document.getElementById('theme-toggle');
  if (btn) (btn.querySelector?.('[data-icon]') || btn).textContent = dark ? '☀' : '☾';
  for (const name of Object.keys(choices)) {
    const select = document.getElementById(`appearance-${name}`);
    if (select) select.value = preferences[name];
  }
}
function save(name, value) {
  if (!choices[name].includes(value)) return;
  preferences[name] = value;
  render();
  const status = document.getElementById('appearance-status');
  try {
    localStorage.setItem(keyFor(name), value);
    if (status) status.textContent = 'Appearance saved on this device.';
  } catch {
    if (status) status.textContent = 'Appearance changed for this window. Device storage is unavailable; it may reset when you reopen Player.';
  }
}
export function initThemes() {
  systemTheme = window.matchMedia('(prefers-color-scheme: dark)');
  for (const name of Object.keys(choices)) {
    const saved = read(keyFor(name));
    preferences[name] = choices[name].includes(saved) ? saved : defaults[name];
    document.getElementById(`appearance-${name}`)?.addEventListener('change', (event) => save(name, event.target.value));
  }
  render();
  systemTheme.addEventListener('change', render);
  window.addEventListener('storage', (event) => {
    const name = Object.keys(choices).find((name) => keyFor(name) === event.key);
    if (!name && event.key !== null) return;
    for (const field of name ? [name] : Object.keys(choices)) {
      const saved = read(keyFor(field));
      preferences[field] = choices[field].includes(saved) ? saved : defaults[field];
    }
    render();
  });
  document.getElementById('theme-toggle')?.addEventListener('click', toggle);
  const modal = document.getElementById('settings-modal');
  document.getElementById('settings-toggle')?.addEventListener('click', () => modal?.classList.add('open'));
  document.getElementById('settings-close')?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    event.stopPropagation();
    modal.classList.remove('open');
    document.getElementById('settings-toggle')?.focus();
  });
}
export function apply(theme) { save('mode', theme); }
export function toggle() {
  apply(document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
}
