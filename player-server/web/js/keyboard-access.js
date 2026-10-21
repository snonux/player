// Scoped mnemonics augment the existing vi keys. Semicolon is a prefix for
// page controls; an open menu/dialog owns its own letters. Native text entry,
// range arrows, browser chords and IME composition always keep their keys.
import { selectByElement } from './selection.js';
const controls = 'button, a[href], input:not([type="hidden"]), textarea, select, summary, [tabindex]:not([tabindex="-1"])';
const alphabet = 'abcdefgimnopqrstuvwxyz'; // h/j/k/l remain navigation keys.
const preferred = {
  'btn-play': 'p', 'btn-prev': 'b', 'btn-next': 'e', 'btn-minimize': 'i',
  'btn-fullscreen': 'f', 'btn-player-notes': 'n', 'btn-player-favorite': 'a',
  'btn-bookmarks': 'r', 'btn-save-position': 's', 'btn-mute': 'm',
  'volume-slider': 'u', 'progress-track': 't', 'sidebar-toggle': 's',
  'btn-player-options': 'o', 'btn-cast': 'c', 'btn-zoom-in': 'z', 'btn-zoom-out': 'd',
  'btn-slideshow': 'w', 'btn-download': 'x',
  'admin-toggle': 'a', 'theme-toggle': 't', 'logout-btn': 'o',
  'notes-save': 's', 'notes-delete': 'd', 'notes-textarea': 'n',
};

export function assignShortcutKeys(labels, preferences = []) {
  if (labels.length > alphabet.length) {
    let length = 2;
    while (alphabet.length ** length < labels.length) length++;
    return labels.map((_, index) => {
      let value = index, key = '';
      for (let digit = 0; digit < length; digit++) { key = alphabet[value % alphabet.length] + key; value = Math.floor(value / alphabet.length); }
      return key;
    });
  }
  const used = new Set();
  const assigned = preferences.map(key => {
    if (!alphabet.includes(key) || key?.length !== 1 || used.has(key)) return null;
    used.add(key); return key;
  });
  return labels.map((label, index) => {
    if (assigned[index]) return assigned[index];
    const choices = `${preferences[index] || ''}${label.toLowerCase()}${alphabet}`;
    const key = [...choices].find(char => alphabet.includes(char) && !used.has(char));
    used.add(key);
    return key;
  });
}

function isTextEntry(target) {
  return target?.isContentEditable || target?.matches?.('textarea, input:not([type="button"]):not([type="submit"]):not([type="checkbox"]):not([type="radio"]):not([type="range"]):not([type="file"])');
}
function visible(element) {
  const style = getComputedStyle(element);
  return element.getClientRects().length && style.visibility === 'visible' && style.opacity !== '0' && !element.matches(':disabled, [aria-disabled="true"]') && !element.closest('[inert], [aria-hidden="true"]');
}
function name(element) {
  return element.getAttribute('aria-label') || element.labels?.[0]?.textContent || element.getAttribute('title') || element.textContent.trim() || element.placeholder || element.name || element.tagName;
}

export function initKeyboardAccess() {
  if (!globalThis.MutationObserver || !document.documentElement || !document.body?.appendChild) return;
  if (document.documentElement.dataset.keyboardAccess) return;
  document.documentElement.dataset.keyboardAccess = 'ready';
  let active = false, prefix = '', items = [], scope = null, frame = 0, requestedScope = null;
  const decorations = new Map();
  const generatedLabels = new Map();
  const status = document.createElement('div');
  status.className = 'shortcut-status';
  status.setAttribute('role', 'status');
  status.hidden = true;
  document.body.appendChild(status);
  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'icon-btn';
  toggle.id = 'keyboard-access-toggle';
  toggle.textContent = '⌨';
  toggle.title = 'Show control shortcuts (; or F2)';
  toggle.setAttribute('aria-label', 'Show control shortcuts');
  toggle.addEventListener('click', () => { active = !active; prefix = ''; refresh(); });
  (document.querySelector('.header-main, .transport-controls, .login-form') || document.body).appendChild(toggle);

  function localScope() {
    const popovers = [...document.querySelectorAll('[popover]:popover-open')];
    if (popovers.length) return popovers.at(-1);
    const dialogs = [...document.querySelectorAll('.modal-overlay.open')];
    return document.activeElement?.closest('.modal-overlay.open') || dialogs.at(-1) || document.querySelector('.sidebar.open');
  }
  function targets(root) {
    return [...root.querySelectorAll(controls)].filter(element => visible(element) &&
      element !== toggle && !element.closest('.shortcut-status') &&
      // A role=button set row and its inner cover button are independent.
      !element.matches('#media-grid') && !element.closest('button button'));
  }
  function clear() {
    for (const [element, label] of generatedLabels) if (element.getAttribute('aria-label') === label) element.removeAttribute('aria-label');
    generatedLabels.clear();
    for (const [element, decoration] of decorations) {
      element.removeAttribute('data-shortcut-key');
      element.removeAttribute('aria-keyshortcuts');
      if (decoration?.classList.contains('shortcut-field-hint')) decoration.remove();
      else if (decoration?.isConnected) decoration.replaceWith(document.createTextNode(decoration.textContent));
    }
    decorations.clear();
  }
  function decorate(element, key) {
    if (!element.hasAttribute('aria-label')) {
      const label = name(element);
      element.setAttribute('aria-label', label);
      generatedLabels.set(element, label);
    }
    element.dataset.shortcutKey = key;
    // aria-keyshortcuts describes the literal letter only in local scopes.
    if (!active && localScope()) element.setAttribute('aria-keyshortcuts', key);
    if (element.matches('input, select, textarea')) {
      const badge = document.createElement('span');
      badge.className = 'shortcut-field-hint';
      badge.setAttribute('aria-hidden', 'true');
      badge.textContent = key;
      element.before(badge);
      decorations.set(element, badge);
      return;
    }
    if (key.length === 1) {
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        if (node.parentElement.closest('svg, kbd, [aria-hidden="true"]')) continue;
        const index = node.textContent.toLowerCase().indexOf(key);
        if (index < 0) continue;
        // Keep the whole label in one flex item: splitting a button's text
        // into siblings lets its icon/text gap split the word itself.
        const label = document.createElement('span');
        label.className = 'shortcut-label';
        const letter = document.createElement('span');
        letter.className = 'shortcut-letter';
        letter.textContent = node.textContent[index];
        label.append(node.textContent.slice(0, index), letter, node.textContent.slice(index + 1));
        node.replaceWith(label);
        decorations.set(element, label);
        return;
      }
    }
    decorations.set(element, null);
  }
  function refresh() {
    clear();
    const local = localScope();
    scope = local || requestedScope || document.fullscreenElement || document.body;
    document.body.classList.toggle('shortcut-mode', active);
    status.hidden = !active;
    status.textContent = `Control shortcuts${prefix ? ': ' + prefix : ''} · press the shown letters · Esc cancels`;
    toggle.setAttribute('aria-pressed', String(active));
    if (document.fullscreenElement && status.parentElement !== document.fullscreenElement) document.fullscreenElement.appendChild(status);
    else if (!document.fullscreenElement && status.parentElement !== document.body) document.body.appendChild(status);
    items = active || local ? targets(scope) : [];
    const keys = assignShortcutKeys(items.map(name), items.map(element => preferred[element.id] || (element.matches('select[aria-label="Audio visualization"]') ? 'v' : element.matches('.lyrics-toggle') ? 'y' : '')));
    items = items.map((element, index) => ({ element, key: keys[index] }));
    for (const { element, key } of items) if (key.startsWith(prefix)) decorate(element, key);
    observer.takeRecords();
  }
  const observer = new MutationObserver(() => {
    if (frame) return;
    frame = requestAnimationFrame(() => { frame = 0; refresh(); });
  });
  observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'hidden', 'disabled', 'aria-label'] });
  document.addEventListener('toggle', () => { prefix = ''; refresh(); }, true);
  document.addEventListener('fullscreenchange', refresh);
  document.addEventListener('focusin', () => { if (scope !== (localScope() || document.fullscreenElement || document.body)) refresh(); });

  function activate(element) {
    active = false; prefix = ''; requestedScope = null; refresh();
    element.focus();
    element.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    if (element instanceof HTMLSelectElement) { openPicker(element); return; }
    if (element.matches('button, a[href], summary, input[type="checkbox"], input[type="radio"], input[type="file"], [role="button"]')) element.click();
  }
  function openPicker(select) {
    const picker = document.createElement('div');
    picker.className = 'shortcut-picker';
    picker.setAttribute('popover', 'auto');
    picker.setAttribute('role', 'group');
    picker.setAttribute('aria-label', select.getAttribute('aria-label') || 'Choose an option');
    for (const option of select.options) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'btn btn-ghost';
      button.textContent = option.label;
      button.disabled = option.disabled || option.parentElement.disabled;
      button.setAttribute('aria-pressed', String(option.selected));
      button.addEventListener('click', () => {
        select.value = option.value;
        picker.hidePopover();
        select.dispatchEvent(new Event('input', { bubbles: true }));
        select.dispatchEvent(new Event('change', { bubbles: true }));
        select.focus();
      });
      picker.appendChild(button);
    }
    picker.addEventListener('toggle', event => { if (event.newState === 'closed') { picker.remove(); select.focus(); } });
    select.parentElement.appendChild(picker);
    picker.showPopover();
    refresh();
    picker.querySelector('button:not(:disabled)')?.focus();
  }

  function openMediaActions(card) {
    const originals = [...card.querySelectorAll('.card-actions button, button[data-action]')];
    if (!originals.length) return;
    selectByElement(card);
    const menu = document.createElement('div');
    menu.className = 'shortcut-picker';
    menu.setAttribute('popover', 'auto');
    menu.setAttribute('role', 'group');
    menu.setAttribute('aria-label', 'Media actions');
    const heading = document.createElement('h3');
    heading.textContent = card.getAttribute('aria-label') || card.querySelector('.title')?.textContent || 'Media actions';
    menu.appendChild(heading);
    for (const original of originals) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'btn btn-ghost';
      button.textContent = original.getAttribute('aria-label') || original.title || original.textContent;
      button.disabled = original.disabled;
      button.addEventListener('click', () => {
        menu.hidePopover();
        card.focus();
        original.click();
      });
      menu.appendChild(button);
    }
    menu.addEventListener('toggle', event => { if (event.newState === 'closed') { menu.remove(); if (!document.querySelector('.modal-overlay.open')) card.focus(); } });
    (document.fullscreenElement || document.body).appendChild(menu);
    menu.showPopover();
    refresh();
    menu.querySelector('button:not(:disabled)')?.focus();
  }
  document.addEventListener('media:actions', event => openMediaActions(event.detail));
  document.addEventListener('click', event => {
    if (event.pointerType !== 'touch' || event.target.closest('button, a, input, select, textarea')) return;
    const card = event.target.closest('#media-grid .media-card[data-id], #media-grid .media-row[data-id]');
    if (!card) return;
    event.preventDefault(); event.stopImmediatePropagation(); openMediaActions(card);
  }, true);

  document.addEventListener('keydown', event => {
    if (event.defaultPrevented || event.isComposing || event.repeat || event.ctrlKey || event.altKey || event.metaKey) return;
    const typing = isTextEntry(event.target);
    const local = localScope();
    if (!active && !local && !typing && (event.key === 'a' || event.key === 'ContextMenu' || (event.key === 'F10' && event.shiftKey))) {
      if (event.target.closest?.('#queue-panel')) return;
      const card = document.querySelector('#media-grid .selected');
      if (card) { event.preventDefault(); event.stopImmediatePropagation(); openMediaActions(card); return; }
    }
    if (!active && !local && !typing && event.key === 'o') {
      const player = document.querySelector('.player.open:not(.hidden):not(.minimized)');
      if (player) {
        event.preventDefault(); event.stopImmediatePropagation();
        const more = player.querySelector('#btn-player-options');
        if (more && visible(more)) more.click();
        else { requestedScope = player; active = true; refresh(); }
        return;
      }
    }
    if (event.key === 'F2' || (event.key === ';' && (!typing || active))) {
      event.preventDefault(); event.stopImmediatePropagation();
      active = !active; prefix = ''; requestedScope = null; refresh(); return;
    }
    if (active && event.key === 'Escape') {
      event.preventDefault(); event.stopImmediatePropagation();
      active = false; prefix = ''; requestedScope = null; refresh(); return;
    }
    if (local?.matches('[popover]') && event.key === 'Escape') {
      event.preventDefault(); event.stopImmediatePropagation();
      local.hidePopover();
      if (local.id === 'player-options') document.getElementById('btn-player-options')?.focus();
      return;
    }
    if (local?.matches('[popover]') && event.key === 'Tab') {
      const at = items.findIndex(item => item.element === document.activeElement);
      const index = (at + (event.shiftKey ? -1 : 1) + items.length) % items.length;
      if (items[index]) { event.preventDefault(); event.stopImmediatePropagation(); items[index].element.focus(); }
      return;
    }
    // Native inputs retain text, range arrows, select typeahead and Tab.
    if (!active && (typing || event.target.matches?.('input, textarea, select, [role="slider"], #progress-track'))) return;
    if (!active && !local) return;
    if (scope !== (local || requestedScope || document.fullscreenElement || document.body)) { prefix = ''; refresh(); }
    if (local?.matches('[popover]') && ['Enter', ' '].includes(event.key) && event.target.matches?.('button')) {
      event.preventDefault(); event.stopImmediatePropagation(); activate(event.target); return;
    }
    if (event.key === 'Enter' && event.target.matches?.('.set-row[role="button"]')) {
      event.preventDefault(); event.stopImmediatePropagation(); event.target.click(); return;
    }
    if (local?.matches('[popover], .sidebar') && ['ArrowDown', 'ArrowUp', 'Home', 'End', 'j', 'k'].includes(event.key)) {
      const at = items.findIndex(item => item.element === document.activeElement);
      const delta = ['ArrowUp', 'k'].includes(event.key) ? -1 : 1;
      const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (at + delta + items.length) % items.length;
      if (items[index]) { event.preventDefault(); event.stopImmediatePropagation(); items[index].element.focus(); }
      return;
    }
    if (event.key === 'Backspace' && active && prefix) { event.preventDefault(); event.stopImmediatePropagation(); prefix = prefix.slice(0, -1); refresh(); return; }
    if (!/^[a-z]$/i.test(event.key) || event.shiftKey) return;
    const next = prefix + event.key.toLowerCase();
    const matches = items.filter(item => item.key.startsWith(next) && visible(item.element));
    if (!matches.length) {
      if (active || local?.matches('[popover], .sidebar')) { event.preventDefault(); event.stopImmediatePropagation(); prefix = ''; refresh(); }
      return;
    }
    event.preventDefault(); event.stopImmediatePropagation();
    const exact = matches.find(item => item.key === next);
    if (exact) activate(exact.element);
    else { prefix = next; refresh(); }
  }, true);
  refresh();
}
