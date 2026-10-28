// Secondary player actions live in one menu so the floating player shows
// stage, title, transport (prev/play/next), optional Lyrics, overflow, and seek.
const SECONDARY =
  '#btn-minimize, #btn-save-position, #btn-bookmarks, #btn-player-notes, #btn-player-favorite, ' +
  '.volume-control, .audio-visual-control, .playback-speed-control, .loudness-control, .sleep-timer-control, ' +
  '.lyrics-language, .lyrics-provider-controls';

export function initCompactControls(player) {
  if (!player?.classList.contains('floating') || player.querySelector('#player-options')) return;
  const transport = player.querySelector('.transport-controls');
  if (!transport) return;

  const supportsPopover = typeof globalThis.HTMLElement?.prototype.showPopover === 'function';
  const panel = document.createElement('div');
  panel.id = 'player-options';
  panel.className = 'player-options';
  panel.setAttribute('role', 'group');
  panel.setAttribute('aria-label', 'More player controls');
  if (supportsPopover) panel.setAttribute('popover', 'auto');
  else panel.hidden = true;

  const header = document.createElement('div');
  header.className = 'player-options-header';
  const title = document.createElement('h3');
  title.textContent = 'Player controls';
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'icon-btn';
  close.textContent = '✕';
  close.setAttribute('aria-label', 'Close controls');
  if (supportsPopover) {
    close.setAttribute('popovertarget', panel.id);
    close.setAttribute('popovertargetaction', 'hide');
  } else {
    close.addEventListener('click', () => dismiss());
  }
  header.append(title, close);
  panel.appendChild(header);
  for (const control of player.querySelectorAll(SECONDARY)) panel.appendChild(control);

  const more = document.createElement('button');
  more.type = 'button';
  more.id = 'btn-player-options';
  more.className = 'icon-btn';
  more.textContent = '⋯';
  more.setAttribute('aria-label', 'More player controls');
  more.setAttribute('aria-controls', panel.id);
  more.setAttribute('aria-expanded', 'false');
  if (supportsPopover) more.setAttribute('popovertarget', panel.id);
  else more.addEventListener('click', () => { if (isOpen()) dismiss(); else openMenu(); });

  // Lyrics stay on the strip when present; language/provider stay in the menu.
  const lyricsToggle = player.querySelector('.lyrics-toggle');
  if (lyricsToggle) transport.insertBefore(lyricsToggle, player.querySelector('#btn-fullscreen'));
  transport.insertBefore(more, player.querySelector('#btn-fullscreen'));
  // No Popover API: keep at most five strip controls besides the menu button
  // (playback group counts as one; cast/fullscreen/image stay with their media).
  if (!supportsPopover) foldOverflow(transport, panel, more);

  player.appendChild(panel);

  function isOpen() {
    return supportsPopover ? panel.matches(':popover-open') : !panel.hidden;
  }
  function openMenu() {
    if (supportsPopover) panel.showPopover();
    else {
      panel.hidden = false;
      more.setAttribute('aria-expanded', 'true');
    }
  }
  function dismiss() {
    if (!isOpen()) return;
    if (supportsPopover) panel.hidePopover();
    else {
      panel.hidden = true;
      more.setAttribute('aria-expanded', 'false');
    }
    more.focus();
  }

  if (supportsPopover) {
    panel.addEventListener('toggle', () => {
      more.setAttribute('aria-expanded', String(panel.matches(':popover-open')));
    });
  } else {
    document.addEventListener('pointerdown', event => {
      if (!isOpen()) return;
      if (panel.contains(event.target) || more.contains(event.target)) return;
      dismiss();
    }, true);
  }
  document.addEventListener('keydown', event => {
    if (!isOpen()) return;
    if (event.key === 'Escape') {
      const disclosure = event.target?.closest?.('.loudness-control details[open]');
      if (disclosure) {
        event.preventDefault(); event.stopImmediatePropagation();
        disclosure.open = false; disclosure.querySelector('summary')?.focus();
        return;
      }
      event.preventDefault();
      event.stopImmediatePropagation();
      dismiss();
    } else if (event.key === '?' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target?.tagName)) {
      dismiss();
    }
  }, true);
  panel.addEventListener('change', event => {
    if (event.target.tagName === 'SELECT') dismiss();
  });
  panel.addEventListener('click', event => {
    if (event.target.closest('#btn-minimize, #btn-save-position, #btn-bookmarks, #btn-player-notes, #btn-player-favorite')) dismiss();
  }, true);
  // Firefox may consume Escape to leave native fullscreen before dispatching
  // a key event to the page. Close its controls on that path too.
  document.addEventListener('fullscreenchange', () => {
    if (!document.fullscreenElement) dismiss();
  });
  new MutationObserver(() => {
    if (player.classList.contains('minimized') || player.classList.contains('hidden') || !player.classList.contains('open')) dismiss();
  }).observe(player, { attributes: true, attributeFilter: ['class'] });
}

function foldOverflow(transport, panel, more) {
  const reserved = new Set([
    transport.querySelector('.transport-group'),
    transport.querySelector('.lyrics-toggle'),
    more,
    transport.querySelector('#btn-cast'),
    transport.querySelector('#btn-fullscreen'),
    transport.querySelector('#crop-indicator'),
  ]);
  let visible = [...reserved].filter(Boolean).filter(el => el !== more && el.id !== 'crop-indicator').length;
  for (const child of [...transport.children]) {
    if (reserved.has(child) || child.classList?.contains('image-only')) continue;
    if (visible < 5) {
      visible += 1;
      continue;
    }
    panel.appendChild(child);
  }
}
