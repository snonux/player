// Secondary player actions always live in one popover so the floating player
// shows only the stage, title, transport and seek bar.
export function initCompactControls(player) {
  if (!player?.classList.contains('floating') ||
      typeof globalThis.HTMLElement?.prototype.showPopover !== 'function') return;
  const transport = player.querySelector('.transport-controls');
  if (!transport || player.querySelector('#player-options')) return;

  const panel = document.createElement('div');
  panel.id = 'player-options';
  panel.className = 'player-options';
  panel.setAttribute('popover', 'auto');
  panel.setAttribute('role', 'group');
  panel.setAttribute('aria-label', 'More player controls');
  const header = document.createElement('div');
  header.className = 'player-options-header';
  const title = document.createElement('h3');
  title.textContent = 'Player controls';
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'icon-btn';
  close.textContent = '✕';
  close.setAttribute('aria-label', 'Close controls');
  close.setAttribute('popovertarget', panel.id);
  close.setAttribute('popovertargetaction', 'hide');
  header.append(title, close);
  panel.appendChild(header);
  for (const control of player.querySelectorAll(
    '#btn-save-position, #btn-bookmarks, #btn-player-notes, #btn-player-favorite, ' +
    '.volume-control, .audio-visual-control, .playback-speed-control, .loudness-control, .sleep-timer-control, .lyrics-toggle, .lyrics-language, .lyrics-provider-controls')) {
    panel.appendChild(control);
  }
  const more = document.createElement('button');
  more.type = 'button';
  more.id = 'btn-player-options';
  more.className = 'icon-btn';
  more.textContent = '⋯';
  more.setAttribute('aria-label', 'More player controls');
  more.setAttribute('aria-controls', panel.id);
  more.setAttribute('aria-expanded', 'false');
  more.setAttribute('popovertarget', panel.id);
  transport.insertBefore(more, player.querySelector('#btn-fullscreen'));
  player.appendChild(panel);
  panel.addEventListener('toggle', () => {
    more.setAttribute('aria-expanded', String(panel.matches(':popover-open')));
  });
  function dismiss() {
    if (!panel.matches(':popover-open')) return;
    panel.hidePopover();
    more.focus();
  }
  document.addEventListener('keydown', event => {
    if (!panel.matches(':popover-open')) return;
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
    if (event.target.closest('#btn-save-position, #btn-bookmarks, #btn-player-notes, #btn-player-favorite, .lyrics-toggle')) dismiss();
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
