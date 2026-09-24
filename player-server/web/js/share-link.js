import { toast } from './utils.js';

export function initShareLinkFallback() {
  const modal = document.getElementById('share-link-fallback-modal');
  const close = () => {
    if (modal?.contains(document.activeElement)) document.activeElement.blur();
    modal?.classList.remove('open');
    const opener = modal?.returnFocus;
    const target = opener !== document.body && opener?.isConnected && opener.getClientRects().length
      ? opener
      : document.querySelector('#shares-modal.open .share-row.selected, #media-grid .selected');
    target?.focus();
  };
  document.getElementById('share-link-fallback-close')?.addEventListener('click', () => {
    close();
  });
  modal?.addEventListener('click', (event) => {
    if (event.target === modal) close();
  });
  document.addEventListener('keydown', (event) => {
    if (!modal?.classList.contains('open')) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
    // Keep My Shares shortcuts from acting through the manual-copy dialog.
    event.stopImmediatePropagation();
  }, true);
}

function showManualShareLink(url) {
  const modal = document.getElementById('share-link-fallback-modal');
  const input = document.getElementById('share-link-fallback-url');
  if (!modal || !input) {
    window.prompt?.('Copy this share link:', url);
    return;
  }
  modal.returnFocus = document.activeElement;
  input.value = url;
  modal.classList.add('open');
  input.focus();
  input.select();
}

export async function copyShareLink(url) {
  try {
    if (typeof navigator.clipboard?.writeText !== 'function') {
      throw new Error('Clipboard unavailable');
    }
    await navigator.clipboard.writeText(url);
    toast('Share link copied');
    return true;
  } catch {
    showManualShareLink(url);
    toast('Clipboard unavailable. Copy the link shown.', 'error');
    return false;
  }
}
