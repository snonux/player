let currentSelection, disposeCurrentSelection;

// Background library refresh must not discard failed snapshots or interrupt a
// batch. A navigation/account change invalidates the selection's own lease.
export function preservePodcastSelection() {
  if (currentSelection?.()) return true;
  disposeCurrentSelection?.();
  return false;
}

// Podcast selection uses feed/episode identity, including episodes without a
// downloaded media row. Each rendered page owns its selection and its lease.
export function bindPodcastMultiselect(host, entries, { apply, current = () => true, refresh, notify = () => {}, downloads = false, deleteMessage } = {}) {
  disposeCurrentSelection?.();
  let selecting = false, busy = false, disposed = false;
  const removers = [];
  const listen = (element, name, handler) => {
    element.addEventListener(name, handler, true);
    removers.push(() => element.removeEventListener(name, handler, true));
  };
  const selected = new Set();
  const toolbar = document.createElement('div');
  toolbar.className = 'grid-full media-selection podcast-selection';
  toolbar.innerHTML = '<button type="button" class="btn" data-select>Select episodes</button><span data-mode hidden><span role="status" aria-live="polite" data-count></span><label>Selected episode actions <select data-operation><option value="completed">Mark completed</option><option value="uncompleted">Mark uncompleted</option><option value="delete">Delete episodes</option></select></label><button type="button" class="btn" data-all>Select all visible</button><button type="button" class="btn" data-apply>Apply</button><button type="button" class="btn" data-cancel>Cancel selection</button></span>';
  host.prepend(toolbar);
  const choose = toolbar.querySelector('[data-select]'), mode = toolbar.querySelector('[data-mode]');
  const operation = toolbar.querySelector('[data-operation]');
  if (downloads) { const option = document.createElement('option'); option.value = 'remove-download'; option.textContent = 'Delete downloaded files'; operation.appendChild(option); }
  const key = ep => JSON.stringify([ep.feed_key, ep.episode_key]);
  const alive = () => !disposed && toolbar.isConnected && current();
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    for (const remove of removers) remove();
    removers.length = 0;
    selected.clear();
    // Controllers may outlive a route; release its detached episode snapshots.
    entries.length = 0;
    if (disposeCurrentSelection === dispose) {
      currentSelection = disposeCurrentSelection = null;
    }
  };
  disposeCurrentSelection = dispose;
  currentSelection = () => alive() && (selecting || busy);
  const update = () => {
    if (disposed) return;
    choose.hidden = selecting; mode.hidden = !selecting;
    toolbar.querySelector('[data-count]').textContent = `${selected.size} selected${busy ? ' · Updating…' : ''}`;
    toolbar.querySelectorAll('button, select').forEach(button => { button.disabled = busy; });
    toolbar.querySelector('[data-apply]').disabled = busy || !selected.size;
    for (const { card, ep, check } of entries) {
      if (!card.contains(check)) card.prepend(check);
      check.hidden = !selecting; check.checked = selected.has(key(ep)); check.disabled = busy;
      card.classList.toggle('bulk-selected', check.checked);
      card.classList.toggle('podcast-selecting', selecting);
    }
  };
  const toggle = ep => {
    if (busy || !alive()) return;
    selecting = true;
    selected.has(key(ep)) ? selected.delete(key(ep)) : selected.add(key(ep)); update();
  };
  choose.onclick = () => { if (!alive()) return; selecting = true; update(); entries[0]?.check.focus(); };
  toolbar.querySelector('[data-all]').onclick = () => { if (!alive()) return; entries.forEach(({ep}) => selected.add(key(ep))); update(); };
  const refreshAndFocus = async () => {
    await refresh?.();
    if (host.isConnected && current()) host.querySelector('[data-select]')?.focus();
  };
  toolbar.querySelector('[data-cancel]').onclick = async () => {
    if (!alive()) return;
    selecting = false; selected.clear(); update(); choose.focus(); await refreshAndFocus();
  };
  toolbar.querySelector('[data-apply]').onclick = async () => {
    if (busy || !selected.size || !alive()) return;
    const action = operation.value;
    if ((action === 'delete' || action === 'remove-download') && !confirm(deleteMessage?.(action, selected.size) || `Delete ${selected.size} selected episodes from this browser’s podcast lists? Listening history stays saved.`)) return;
    if (!alive()) return;
    busy = true; update();
    let failed = 0, updated = 0;
    for (const { ep } of entries.filter(({ep}) => selected.has(key(ep)))) {
      if (!alive()) break;
      try {
        await apply(ep, action, alive);
        if (!alive()) break;
        selected.delete(key(ep)); updated++;
      } catch { failed++; }
    }
    busy = false;
    if (!alive()) return;
    notify(failed ? `${failed} episodes could not be updated. Still selected; retry the action.` : `Updated ${updated} episodes`, failed ? 'error' : 'info');
    if (!failed) selecting = false;
    update();
    if (!failed) { choose.focus(); await refreshAndFocus(); }
  };
  for (const entry of entries) {
    const { card, ep } = entry;
    const check = document.createElement('input'); check.type = 'checkbox'; check.className = 'media-selection-check';
    check.setAttribute('aria-label', `Select ${ep.title || 'episode'}`); card.prepend(check); entry.check = check;
    listen(card, 'click', event => {
      if (!selecting && !busy) return;
      if (event.target !== check) event.preventDefault();
      event.stopImmediatePropagation(); toggle(ep);
    });
    listen(card, 'dblclick', event => {
      if (!selecting && !busy) return;
      event.preventDefault(); event.stopImmediatePropagation();
    });
  }
  listen(host, 'keydown', event => {
    if (!alive() || !selecting || busy || event.key !== 'Escape') return;
    event.preventDefault(); event.stopImmediatePropagation(); toolbar.querySelector('[data-cancel]').click();
  });
  update();
  return { refresh: update, dispose, get active() { return alive() && (selecting || busy); } };
}
