const STORAGE_KEY = 'kiss-theme';

export function initThemes() {
  const saved = localStorage.getItem(STORAGE_KEY) || 'dark';
  apply(saved);
  const btn = document.getElementById('theme-toggle');
  if (btn) btn.addEventListener('click', toggle);
}

export function apply(theme) {
  document.documentElement.setAttribute('data-theme', theme);
  localStorage.setItem(STORAGE_KEY, theme);
  const btn = document.getElementById('theme-toggle');
  if (btn) btn.textContent = theme === 'dark' ? '☀' : '🌙';
}

export function toggle() {
  const current = document.documentElement.getAttribute('data-theme') || 'dark';
  apply(current === 'dark' ? 'light' : 'dark');
}
