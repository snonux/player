import { focusSearch } from '../search.js';

export function initHelp() {
  const modal = document.getElementById('help-modal');
  const closeBtn = document.getElementById('help-close');
  const toggleBtn = document.getElementById('help-toggle');

  toggleBtn?.addEventListener('click', toggleHelp);
  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) modal.classList.remove('open');
  });
}

export function toggleSidebar() {
  const sidebar = document.getElementById('sidebar');
  const page = document.querySelector('.page');
  if (!sidebar) return;
  sidebar.classList.toggle('open');
  page?.classList.toggle('has-sidebar', sidebar.classList.contains('open'));
}

export function toggleHelp() {
  const modal = document.getElementById('help-modal');
  if (!modal) return;
  modal.classList.toggle('open');
}

export function showSearch() {
  const bar = document.getElementById('search-bar');
  bar?.classList.remove('hidden');
  focusSearch();
}
