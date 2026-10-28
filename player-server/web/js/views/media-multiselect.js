import { currentElement } from '../selection.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { toast } from '../utils.js';

// Each rendered page owns its selection. Navigation discards it, so hidden
// items from another folder or filter can never be included in an operation.
export function bindMediaMultiselect(grid, onComplete) {
  const cards = [...grid.querySelectorAll('.media-card[data-id], .media-row[data-id]')];
  if (!cards.length) return;
  const selected = new Set();
  let selecting = false, busy = false;
  const toolbar = document.createElement('div');
  toolbar.className = 'grid-full media-selection';
  toolbar.hidden = true;
  toolbar.innerHTML = '<span role="status" aria-live="polite" data-count></span><label>Selected item actions <select data-operation><option value="favorite">Add to favorites</option><option value="unfavorite">Remove from favorites</option><option value="finished">Mark as finished</option><option value="not_started">Mark as not started</option></select></label><button type="button" class="btn" data-apply>Apply</button><button type="button" class="btn btn-ghost" data-cancel>Cancel selection</button>';
  grid.prepend(toolbar);
  const apply = toolbar.querySelector('[data-apply]');
  const cancel = toolbar.querySelector('[data-cancel]');
  const operation = toolbar.querySelector('[data-operation]');
  const update = () => {
    toolbar.hidden = !selecting;
    grid.dataset.mediaSelecting = String(selecting);
    toolbar.querySelector('[data-count]').textContent = `${selected.size} selected${busy ? ' · Updating…' : ''}`;
    apply.disabled = busy || !selected.size;
    cancel.disabled = operation.disabled = busy;
    for (const card of cards) {
      const checked = selected.has(card.dataset.id);
      card.classList.toggle('bulk-selected', checked);
      const check = card.querySelector('[data-media-check]');
      check.hidden = !selecting;
      check.checked = checked;
      check.disabled = busy;
    }
  };
  const toggle = card => {
    if (busy) return;
    selecting = true;
    selected.has(card.dataset.id) ? selected.delete(card.dataset.id) : selected.add(card.dataset.id);
    update();
  };
  cancel.onclick = () => {
    selecting = false;
    selected.clear();
    update();
    cards[0].querySelector('.media-open')?.focus();
  };
  const onSelectionKey = event => {
    if (!toolbar.isConnected || !selecting || busy) return;
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopImmediatePropagation(); cancel.click();
    } else if (event.key === 'Enter' && event.target.matches('.media-open')) {
      event.preventDefault(); event.stopImmediatePropagation();
      const card = currentElement() || event.target.closest('[data-id]');
      if (cards.includes(card)) toggle(card);
    }
  };
  toolbar.addEventListener('keydown', onSelectionKey, true);
  apply.onclick = async () => {
    if (busy || !selected.size) return;
    const action = operation.value;
    const batch = [...selected];
    busy = true;
    update();
    let failed = 0;
    for (const id of batch) {
      // Stop if navigation replaced this page while the request was pending.
      if (!toolbar.isConnected) break;
      try {
        if (action === 'favorite' || action === 'unfavorite') {
          const card = cards.find(card => card.dataset.id === id);
          const button = card.querySelector('[data-action="favorite"]');
          const desired = action === 'favorite';
          if ((button?.getAttribute('aria-pressed') === 'true') !== desired) {
            const result = await API.favorite(id);
            const favorite = result.favorite;
            if (typeof favorite !== 'boolean' || favorite !== desired) throw new Error('Favorite changed; retry');
            const item = state.media.find(item => String(item.id) === id);
            if (item) item.favorite = item.is_favorite = favorite;
            if (button) {
              button.setAttribute('aria-pressed', String(favorite));
              button.classList.toggle('active', favorite);
              button.textContent = favorite ? '♥ Favorited' : '♡ Favorite';
              button.setAttribute('aria-label', favorite ? 'Favorited' : 'Favorite');
            }
          }
        } else {
          await API.progressStatus(id, action);
          const item = state.media.find(item => String(item.id) === id);
          if (item) item.position_seconds = action === 'finished' ? (item.duration || 0) : 0;
        }
        selected.delete(id);
      } catch {
        failed++;
      }
    }
    busy = false;
    if (!toolbar.isConnected) return;
    // Keep the toolbar visible after success so keyboard focus stays usable.
    update();
    cancel.focus();
    toast(failed ? `${failed} items could not be updated. Still selected; try again.` : `Updated ${batch.length} items`, failed ? 'error' : 'info');
    if (!failed) await onComplete?.();
  };
  for (const card of cards) {
    card.addEventListener('keydown', onSelectionKey, true);
    const check = document.createElement('input');
    check.type = 'checkbox';
    check.dataset.mediaCheck = '';
    check.className = 'media-selection-check';
    check.hidden = true;
    check.setAttribute('aria-label', `Select ${card.getAttribute('aria-label') || card.querySelector('.title')?.textContent || 'item'}`);
    card.appendChild(check);
    // Included in the existing More menu and operable with native keyboard activation.
    const choose = document.createElement('button');
    choose.type = 'button';
    choose.dataset.action = 'select-items';
    choose.className = 'card-action-extra';
    choose.setAttribute('aria-label', 'Select items');
    choose.textContent = 'Select items';
    choose.onclick = event => { event.stopPropagation(); toggle(card); };
    card.querySelector('.card-actions')?.appendChild(choose);
    let timer, origin, suppressUntil = 0;
    const clear = () => { clearTimeout(timer); timer = null; };
    card.addEventListener('pointerdown', event => {
      clear();
      if (event.button !== 0 || !event.isPrimary || event.target.closest('.card-actions, input')) return;
      origin = { x: event.clientX, y: event.clientY };
      timer = setTimeout(() => { timer = null; if (!card.isConnected) return; suppressUntil = Date.now() + 1200; toggle(card); }, 550);
    });
    card.addEventListener('pointermove', event => {
      if (origin && Math.hypot(event.clientX - origin.x, event.clientY - origin.y) > 10) clear();
    });
    for (const name of ['pointerup', 'pointercancel', 'pointerleave', 'lostpointercapture']) card.addEventListener(name, clear);
    card.addEventListener('contextmenu', event => { if (selecting || timer) event.preventDefault(); });
    card.addEventListener('dragstart', clear);
    card.addEventListener('click', event => {
      if (event.target.closest('[data-action="select-items"]')) return;
      if (Date.now() < suppressUntil) { event.preventDefault(); event.stopImmediatePropagation(); suppressUntil = 0; return; }
      if (!selecting) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      toggle(card);
    }, true);
    card.addEventListener('dblclick', event => {
      if (selecting) { event.preventDefault(); event.stopImmediatePropagation(); }
    }, true);
  }
  update();
}
