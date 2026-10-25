// Roomy windows show every control under the stage. Small windows use a
// popover so secondary actions don't consume the whole media area.
export function initCompactControls(player) {
  if (!player?.classList.contains('floating') ||
      typeof globalThis.HTMLElement?.prototype.showPopover !== 'function') return;
  const transport = player.querySelector('.transport-controls');
  const controls = player.querySelector('.controls');
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
    '.volume-control, .audio-visual-control, .playback-speed-control, .loudness-control, .sleep-timer-control, .lyrics-toggle, .lyrics-language')) {
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
  const roomy = matchMedia('(min-width: 600px) and (min-height: 640px)');
  function arrange() {
    dismiss();
    more.hidden = roomy.matches;
    player.classList.toggle('controls-expanded', roomy.matches);
    panel.classList.toggle('player-inline-options', roomy.matches);
    if (roomy.matches) {
      panel.removeAttribute('popover');
      controls.appendChild(panel);
    } else {
      panel.setAttribute('popover', 'auto');
      player.appendChild(panel);
    }
  }
  roomy.addEventListener('change', arrange);
  arrange();
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
