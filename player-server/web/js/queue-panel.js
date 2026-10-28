import { offlineMusicMode } from './offline-context.js';
import { t, html, plural } from './i18n.js';
import { saveQueueAsPlaylist } from './queue-playlist.js';
import { queueState, jumpQueue, moveQueue, removeQueue, clearQueue, undoQueue, resumeQueue, retryQueueRestore, cycleQueueRepeat, shuffleQueue, setQueueAutoplay, savedSessionState, continueSavedSession, replaceSavedSession, keepOtherSession } from './queue.js';
import { mediaTitle, musicSubtitle } from './music.js';
import { escapeHtml as esc } from './utils.js';

let panel, selected = 0, opener, originalParent;
const pageSize = 200;
export function isQueueFocused() { return !!panel?.classList.contains('open') && panel.contains(document.activeElement); }
export function toggleQueue() {
  if (panel.classList.contains('open')) { closeQueue(); return; }
  opener = document.activeElement;
  if (document.fullscreenElement) document.fullscreenElement.appendChild(panel);
  selected = Math.max(0, queueState().index);
  panel.classList.add('open');
  syncMode(); render(); focusRow();
}
function closeQueue() {
  panel.classList.remove('open'); document.body.classList.remove('queue-open');
  if (panel.parentElement !== originalParent) originalParent.appendChild(panel);
  if (opener?.isConnected) opener.focus();
}
function syncMode() {
  if (!panel.classList.contains('open')) return;
  const narrow = window.matchMedia('(max-width: 699px)').matches;
  panel.classList.toggle('modal-overlay', narrow);
  panel.setAttribute('role', narrow ? 'dialog' : 'complementary');
  if (narrow) panel.setAttribute('aria-modal', 'true'); else panel.removeAttribute('aria-modal');
  document.body.classList.toggle('queue-open', !narrow);
}
export function initQueuePanel() {
  panel = document.getElementById('queue-panel');
  originalParent = panel.parentElement;
  document.addEventListener('fullscreenchange', () => {
    if (panel.classList.contains('open')) (document.fullscreenElement || originalParent).appendChild(panel);
  });
  document.querySelectorAll('[data-queue-toggle]').forEach(button => button.addEventListener('click', toggleQueue));
  panel.addEventListener('click', async e => {
    const action = e.target.closest('[data-queue-action]');
    if (!action) return;
    const at = Number(action.closest('[data-queue-index]')?.dataset.queueIndex ?? selected);
    switch (action.dataset.queueAction) {
      case 'save-playlist': saveQueueAsPlaylist(queueState().entries, () => panel.querySelector('[data-queue-action="queue-more"]')?.focus(), closeQueue); return;
      case 'continue-session':
        if (queueState().entries.length && !window.confirm('Replace this queue with the saved session? Playback will stay paused.')) return;
        await continueSavedSession(); break;
      case 'replace-session':
        if (window.confirm('Replace the saved session with this queue? The other client keeps its current playback.')) await replaceSavedSession(); break;
      case 'keep-other': keepOtherSession(); break;
      case 'shuffle-menu': openQueueShuffleMenu(action); return;
      case 'queue-more': openQueueMoreMenu(action); return;
      case 'shuffle': case 'shuffle-tracks': case 'shuffle-albums': case 'reshuffle': await shuffleQueue({reshuffle: action.dataset.queueAction === 'reshuffle', mode: action.dataset.queueAction === 'shuffle-albums' ? 'albums' : action.dataset.queueAction === 'shuffle-tracks' ? 'tracks' : null}); panel.querySelector('[data-queue-action="shuffle-menu"]')?.focus(); return;
      case 'repeat': cycleQueueRepeat(); panel.querySelector('[data-queue-action="repeat"]')?.focus(); return;
      case 'autoplay': setQueueAutoplay(!(queueState().autoplay?.enabled)); panel.querySelector('[data-queue-action="autoplay"]')?.focus(); return;
      case 'resume': await resumeQueue(); break;
      case 'retry': await retryQueueRestore(); break;
      case 'close': closeQueue(); return;
      case 'menu': selected = at; openRowMenu(); return;
      case 'play': selected = at; await jumpQueue(at); break;
      case 'up': moveQueue(at, -1); selected = Math.max(0, at - 1); break;
      case 'down': moveQueue(at, 1); selected = Math.min(queueState().entries.length - 1, at + 1); break;
      case 'remove': removeQueue(at); selected = Math.min(at, queueState().entries.length - 1); break;
      case 'clear': clearQueue(); selected = 0; break;
      case 'undo': undoQueue(); selected = Math.max(0, queueState().index); break;
      case 'previous': selected = Math.max(0, selected - pageSize); break;
      case 'next': selected = Math.min(queueState().entries.length - 1, selected + pageSize); break;
      case 'now': selected = Math.max(0, queueState().index); break;
    }
    render(); focusRow();
  });
  panel.addEventListener('focusin', e => {
    const row = e.target.closest('[data-queue-index]');
    if (row) selected = Number(row.dataset.queueIndex);
  });
  document.addEventListener('queue:changed', () => {
    if (!panel.classList.contains('open')) return;
    const focused = isQueueFocused(), toolbarAction = document.activeElement?.dataset.queueAction;
    render();
    const focusMap = { shuffle: 'shuffle-menu', 'shuffle-tracks': 'shuffle-menu', 'shuffle-albums': 'shuffle-menu', reshuffle: 'shuffle-menu', 'continue-session': 'queue-more', 'replace-session': 'queue-more', 'keep-other': 'queue-more', 'save-playlist': 'queue-more', clear: 'queue-more', undo: 'queue-more' };
    const focusAction = focusMap[toolbarAction] || toolbarAction;
    if (['repeat', 'shuffle-menu', 'autoplay', 'queue-more'].includes(focusAction)) panel.querySelector(`[data-queue-action="${focusAction}"]`)?.focus();
    else if (focused) focusRow();
  });
  window.addEventListener('resize', syncMode);
  document.addEventListener('queue:open', () => { if (!panel.classList.contains('open')) toggleQueue(); else focusRow(); });
}
function focusRow() {
  (panel.querySelector(`[data-queue-index="${selected}"]`) || panel.querySelector('[data-queue-action="queue-more"]') || panel.querySelector('[data-queue-action="close"]'))?.focus();
  panel.querySelector(`[data-queue-index="${selected}"]`)?.scrollIntoView({ block: 'nearest' });
}
function openRowMenu() {
  const row = panel.querySelector(`[data-queue-index="${selected}"]`);
  if (!row) return;
  const menu = document.createElement('div');
  menu.className = 'shortcut-picker';
  menu.setAttribute('popover', 'auto');
  menu.setAttribute('role', 'menu');
  menu.setAttribute('aria-label', 'Queue track actions');
  menu.dataset.queueIndex = String(selected);
  for (const original of row.querySelectorAll('[data-queue-action]:not([data-queue-action="menu"]):not([data-queue-action="play"])')) {
    const button = original.cloneNode(true);
    button.hidden = false;
    button.textContent = original.getAttribute('aria-label') || original.textContent;
    menu.appendChild(button);
  }
  menu.addEventListener('toggle', e => { if (e.newState === 'closed') { menu.remove(); focusRow(); } });
  panel.appendChild(menu); menu.showPopover(); menu.querySelector('button:not(:disabled)')?.focus();
}
function openToolbarMenu(opener, label, items) {
  const menu = document.createElement('div');
  menu.className = 'shortcut-picker';
  menu.setAttribute('popover', 'auto');
  menu.setAttribute('role', 'menu');
  menu.setAttribute('aria-label', label);
  menu.innerHTML = items.map(([action, text, disabled]) => html`<button class="btn btn-ghost" data-queue-action="${action}" ${disabled ? 'disabled' : ''}>${esc(text)}</button>`).join('');
  menu.addEventListener('toggle', e => { if (e.newState === 'closed') { menu.remove(); opener.focus(); } });
  panel.appendChild(menu); menu.showPopover(); menu.querySelector('button:not(:disabled)')?.focus();
}
function openQueueShuffleMenu(opener) {
  const q = queueState();
  const busy = q.loading || q.restoreError || q.shuffleEligible < 2;
  openToolbarMenu(opener, t('Shuffle options'), [
    ['shuffle', `${t('Shuffle')}: ${q.shuffle ? (q.shuffleMode === 'albums' ? t('Albums') : t('Tracks')) : t('Off')} · ${t('Toggle')}`, busy && !q.shuffle],
    ['shuffle-tracks', t('Shuffle tracks'), busy],
    ['shuffle-albums', t('Shuffle albums'), busy],
    ...(q.shuffle ? [['reshuffle', t('Reshuffle remaining'), busy]] : []),
  ]);
}
function openQueueMoreMenu(opener) {
  const q = queueState(), session = savedSessionState(), offline = offlineMusicMode();
  const items = [
    ...(!offline ? [
      ['continue-session', t('Continue session'), q.loading],
      ['replace-session', t('Replace saved session'), q.loading],
      ...(session.conflict ? [['keep-other', t('Keep other session'), false]] : []),
      ['save-playlist', t('Save as playlist'), !q.entries.length || q.loading || q.restoreError],
    ] : []),
    ['clear', t('Clear queue'), !q.entries.length || q.loading],
    ['undo', t('Undo'), !q.canUndo || q.loading],
  ];
  openToolbarMenu(opener, t('More queue actions'), items);
}
export function queueKey(e) {
  if (e.target.closest('[data-queue-action]') && ['Enter', ' '].includes(e.key)) return false;
  if (e.target.closest('[popover]')) return false;
  const length = queueState().entries.length;
  switch (e.key) {
    case 'ContextMenu': openRowMenu(); e.preventDefault(); return true;
    case 'F10': if (!e.shiftKey) return false; openRowMenu(); e.preventDefault(); return true;
    case 'Y': cycleQueueRepeat(); e.preventDefault(); return true;
    case 'Q': case 'Escape': closeQueue(); break;
    case 'j': case 'ArrowDown': selected = Math.min(length - 1, selected + 1); break;
    case 'k': case 'ArrowUp': selected = Math.max(0, selected - 1); break;
    case 'Enter': void jumpQueue(selected); break;
    case 'J': moveQueue(selected, 1); selected = Math.min(length - 1, selected + 1); break;
    case 'K': moveQueue(selected, -1); selected = Math.max(0, selected - 1); break;
    case 'Delete': removeQueue(selected); selected = Math.min(selected, queueState().entries.length - 1); break;
    default: return false;
  }
  e.preventDefault(); render(); if (panel.classList.contains('open')) focusRow(); return true;
}
function render() {
  const q = queueState(), session = savedSessionState();
  selected = Math.max(0, Math.min(selected, q.entries.length - 1));
  const start = Math.floor(Math.max(0, selected) / pageSize) * pageSize;
  const ap = q.autoplay || { enabled: false, available: true, status: '' };
  panel.innerHTML = html`<section class="queue-content"><div class="modal-header"><h2 id="queue-title">Now Playing</h2><button class="icon-btn" data-queue-action="close" aria-label="Close queue">✕</button></div><p>${esc(q.label ? `Playing from: ${q.label}` : 'Music queue')}</p><div class="queue-actions"><button class="btn${q.shuffle ? ' btn-primary' : ''}" data-queue-action="shuffle-menu" aria-haspopup="menu" aria-pressed="${q.shuffle}" aria-describedby="queue-shuffle-hint" ${q.loading || q.restoreError || (!q.shuffle && q.shuffleEligible < 2) ? 'disabled' : ''}>${t('Shuffle')}: ${q.shuffle ? (q.shuffleMode === 'albums' ? t('Albums') : t('Tracks')) : t('Off')}</button><button class="btn${q.repeat === 'off' ? '' : ' btn-primary'}" data-queue-action="repeat" aria-pressed="${q.repeat !== 'off'}" title="Cycle queue repeat (Y)" ${q.loading || q.restoreError ? 'disabled' : ''}><span aria-hidden="true">${q.repeat === 'one' ? '🔂' : '🔁'}</span> Repeat: ${q.repeat[0].toUpperCase() + q.repeat.slice(1)}</button><button class="btn${ap.enabled ? ' btn-primary' : ''}" data-queue-action="autoplay" aria-pressed="${ap.enabled}" aria-describedby="queue-autoplay-hint" ${q.restoreError || (!ap.available && !ap.enabled) ? 'disabled' : ''}>${t('Autoplay related music')}</button><button type="button" class="icon-btn" data-queue-action="queue-more" aria-label="${t('More')}" aria-haspopup="menu" title="${t('More')}">☰</button></div><p id="queue-shuffle-hint" class="text-sm">Only upcoming songs change; Play next stays first. Shuffle albums keeps each album’s disc and song order. Library order stays the same.${q.shuffleEligible < 2 ? ' At least two upcoming songs outside Play next are needed to shuffle.' : ''}</p><p id="queue-autoplay-hint" class="text-sm">${t(ap.available ? 'After this queue ends · related songs from your library' : 'Autoplay needs a connection.')}</p><p class="text-sm">j/k select · Enter play · K/J move · Delete remove</p><p role="status">${esc(q.status || (q.active ? t('Current track marked Now') : t('Queue paused. Choose a track to play.')))}</p>${q.restored ? html`<p>Paused at ${Math.floor(q.position / 60)}:${String(Math.floor(q.position % 60)).padStart(2, '0')}</p><button class="btn btn-primary" data-queue-action="resume" ${q.loading ? 'disabled' : ''}>Resume queue</button>` : ''}${q.restoreError ? html`<button class="btn" data-queue-action="retry" ${q.loading ? 'disabled' : ''}>Retry restore</button>` : ''}<p class="text-sm" role="status">${esc(session.message)}</p><ol class="queue-list" role="list" start="${start + 1}">${q.entries.slice(start, start + pageSize).map((entry, offset) => {
    const i = start + offset, title = esc(mediaTitle(entry.media));
    return html`<li tabindex="-1" data-queue-index="${i}" data-entry-id="${entry.id}" class="queue-row${i === q.index ? ' queue-current' : ''}${i === selected ? ' queue-selected' : ''}" ${i === q.index && q.active ? 'aria-current="true"' : ''}><span>${i + 1}${entry.upNext ? ' · Up next (you added)' : ''}${entry.autoplayRelated ? ` · ${t('Autoplay · related music')}` : ''}${i === q.index ? q.active ? ' · Now' : ' · Selected' : ''}</span><button class="btn btn-ghost queue-track" data-queue-action="play" ${q.loading ? 'disabled' : ''} aria-label="Play ${title}">${title}<small>${esc(musicSubtitle(entry.media))}</small></button><div class="queue-actions"><button class="btn btn-ghost" data-queue-action="menu" aria-label="Actions for ${title}" aria-haspopup="menu" title="More">☰</button><button class="btn btn-ghost" data-queue-action="up" hidden aria-label="Move ${title} up" ${q.loading || i === 0 ? 'disabled' : ''}>↑</button><button class="btn btn-ghost" data-queue-action="down" hidden aria-label="Move ${title} down" ${q.loading || i === q.entries.length - 1 ? 'disabled' : ''}>↓</button><button class="btn btn-ghost" data-queue-action="remove" hidden aria-label="Remove ${title}" ${q.loading ? 'disabled' : ''}>Remove</button></div></li>`;
  }).join('')}</ol>${q.entries.length ? '' : html`<p>Queue is empty. Add songs or play an album to start.</p>`}<div class="queue-footer">${q.entries.length > pageSize ? html`<span>Showing ${start + 1}–${Math.min(start + pageSize, q.entries.length)} of ${q.entries.length}</span><button class="btn" data-queue-action="previous" ${start === 0 ? 'disabled' : ''}>Previous page</button><button class="btn" data-queue-action="next" ${start + pageSize >= q.entries.length ? 'disabled' : ''}>Next page</button><button class="btn" data-queue-action="now">Jump to now</button>` : ''}</div></section>`;
}
