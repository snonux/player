import { focusSearch } from '../search.js';
import { focusSelectedCard } from '../selection.js';

/** Matches `.sidebar` overlay breakpoint in layout.css (max-width: 860px). */
export const NARROW_SIDEBAR_MQ = '(max-width: 860px)';

export function isNarrowSidebar() {
  return Boolean(globalThis.matchMedia?.(NARROW_SIDEBAR_MQ)?.matches);
}

export function closeSidebar() {
  const sidebar = document.getElementById('sidebar');
  if (!sidebar) return false;
  const wasOpen = sidebar.classList.contains('open');
  sidebar.classList.remove('open');
  document.querySelector('.page')?.classList.remove('has-sidebar');
  return wasOpen;
}

/**
 * After a navigation choice (set, folder, music, podcast), hide the overlay
 * sidebar on narrow viewports and move focus into the content. Desktop keeps
 * the sidebar open; Esc / ✕ / ☰ stay available for manual close/toggle.
 */
export function dismissSidebarAfterNav() {
  if (!isNarrowSidebar()) return false;
  const sidebar = document.getElementById('sidebar');
  if (!sidebar?.classList.contains('open')) return false;
  closeSidebar();
  if (sidebar.contains(document.activeElement)) document.activeElement.blur?.();
  const focusContent = () => {
    if (focusSelectedCard()) return;
    document.getElementById('media-grid')
      ?.querySelector('.folder-open, .set-open, .media-open, .song-open, button, [href]')
      ?.focus?.();
  };
  document.addEventListener('library:navigated', focusContent, { once: true });
  return true;
}

export function initHelp() {
  const modal = document.getElementById('help-modal');
  const closeBtn = document.getElementById('help-close');
  const toggleBtn = document.getElementById('help-toggle');

  document.getElementById('help-search')?.addEventListener('input', filterHelp);
  filterHelp();

  // Fullscreen only paints descendants of its target. Restore the help
  // overlay after any close path, including the shared Escape handler.
  if (modal) {
    const parent = modal.parentNode;
    const next = modal.nextSibling;
    const restorePosition = () => {
      if ((!modal.classList.contains('open') || !document.fullscreenElement) && modal.parentNode !== parent) {
        const active = document.activeElement;
        const keepFocus = modal.classList.contains('open') && modal.contains(active);
        parent.insertBefore(modal, next);
        if (keepFocus) active.focus();
      }
    };
    document.addEventListener('fullscreenchange', restorePosition);
    new MutationObserver(restorePosition).observe(modal, { attributes: true, attributeFilter: ['class'] });
  }

  toggleBtn?.addEventListener('click', toggleHelp);
  document.getElementById('help-hint')?.addEventListener('click', toggleHelp);
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
  if (!modal.classList.contains('open')) {
    const input = document.getElementById('help-search');
    if (input) input.value = '';
    filterHelp();
    if (document.fullscreenElement && !document.fullscreenElement.contains(modal)) {
      document.fullscreenElement.appendChild(modal);
    }
  }
  modal.classList.toggle('open');
}

export function showSearch() {
  focusSearch();
}

export function focusHelpSearch() {
  const input = document.getElementById('help-search');
  input?.focus();
  input?.select();
}

function filterHelp() {
  const modal = document.getElementById('help-modal');
  if (!modal) return;
  const query = (document.getElementById('help-search')?.value || '').trim().toLowerCase();
  let matches = 0;
  modal.querySelectorAll('.help-row').forEach((row) => {
    row.hidden = !row.textContent.toLowerCase().includes(query);
    if (!row.hidden) matches++;
  });
  const empty = document.getElementById('help-no-results');
  if (empty) empty.hidden = matches !== 0;
}
