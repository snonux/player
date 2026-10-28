import { bindOfflinePinButton, showOfflinePin } from './offline-pin-dialog.js';
import { t, html, plural, ui } from './i18n.js';
import { shareMusicCollection } from './collection-share.js';
import { downloadMusic } from './music-downloads.js';
import { API } from './api.js';
import { parseShuffleOptions, selectShuffleSongs } from './queue-shuffle.js';
import { resolveCollections } from './collection-resolver.js';
import { insertQueue, queueState, queueUndoToken, undoQueue } from './queue.js';
import { currentPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { state } from './state.js';
import { currentElement } from './selection.js';
import { escapeHtml as esc, toast } from './utils.js';

let contexts = [], collectionIntent = 0;
function activeContext() {
 contexts = contexts.filter(owner => owner.root.isConnected);
 const active = contexts.filter(owner => owner.current());
 return active.findLast(owner => owner.catalog) || active.at(-1) || null;
}
const ACTION_CONTROLS = '[data-collection-play], [data-collection-shuffle], [data-collection-menu], [data-collection-download], [data-collection-share]';
export function collectionButtons(label) {
 // Play and Shuffle stay visible; Download / Pin / Share stay in the DOM (hidden) for binders while More owns the visible entry point and focus restore.
 return html`<span class="playlist-actions" data-collection-actions><button class="btn btn-primary" data-collection-play>▶ ${ui(label)}</button><button class="btn" data-collection-shuffle>Shuffle</button><button type="button" class="icon-btn" data-collection-menu aria-label="More" aria-haspopup="menu" title="More">☰</button><button class="btn" data-collection-download hidden>Download…</button><button class="btn" data-collection-pin hidden>Pin offline…</button><button class="btn" data-collection-share hidden>Share…</button><span role="status" data-collection-status></span></span>`;
}
export function bindCollectionActions(root, collections, current, sourceLabel = t('Selected collections')) {
 const owner = {root, collections, current, sourceLabel, busy:false, generation:0};
 contexts.push(owner);
 root.querySelector('[data-collection-play]').onclick = () => run(owner, 'replace');
 root.querySelector('[data-collection-shuffle]').onclick = () => run(owner, 'shuffle');
 root.querySelector('[data-collection-share]').onclick = () => run(owner, 'share');
 root.querySelector('[data-collection-download]').onclick = () => run(owner, 'download');
 const more = root.querySelector('[data-collection-menu]');
 more.onclick = event => menu(event.currentTarget, owner);
 const pin = root.querySelector('[data-collection-pin]');
 owner.pinUpdate = bindOfflinePinButton(pin, collections, current, sourceLabel);
 // Restore focus to the visible More control after the pin dialog closes.
 if (pin) pin.onclick = () => {
  const selected = collections();
  if (current() && selected.length === 1 && ['album', 'playlist'].includes(selected[0].kind)) showOfflinePin(selected[0], sourceLabel, more);
 };
 return owner;
}
function menu(button, owner) {
 const pin = owner.root.querySelector('[data-collection-pin]');
 const popover = document.createElement('div');
 popover.className = 'shortcut-picker'; popover.setAttribute('popover', 'auto');
 popover.setAttribute('role', 'menu'); popover.setAttribute('aria-label', t('More collection actions'));
 // Pin label is dynamic status text — escape it outside html`` so it is not treated as UI prose.
 popover.innerHTML = html`<button class="btn btn-ghost" data-action="next">Play next</button><button class="btn btn-ghost" data-action="append">Add to queue</button><button class="btn btn-ghost" data-action="options">Shuffle options…</button><button class="btn btn-ghost" data-action="download">Download…</button>`
  + `<button class="btn btn-ghost" data-action="pin"${pin?.disabled ? ' disabled' : ''}>${esc(pin?.textContent || t('Pin offline…'))}</button>`
  + html`<button class="btn btn-ghost" data-action="share">Share…</button>`;
 popover.querySelectorAll('button').forEach(item => item.onclick = () => {
  popover.hidePopover();
  if (item.dataset.action === 'options') shuffleOptions(owner, button);
  else if (item.dataset.action === 'pin') pin?.click();
  else void run(owner, item.dataset.action);
 });
 popover.addEventListener('toggle', event => { if (event.newState === 'closed') { popover.remove(); if(button.isConnected)button.focus(); } });
 document.body.append(popover); popover.showPopover(); popover.querySelector('button').focus();
}
async function run(owner, action, targets, settings = {}) {
 if (owner.busy || !owner.current() || !owner.root.isConnected) return;
 if (!['download','share'].includes(action) && queueState().loading) { toast(t('Loading track… Please wait.'), 'info'); return; }
 const collections = targets || owner.collections();
 if (!collections.length) { toast(t('Select a collection first.'), 'info'); return; }
 const intent = currentPlaybackIntent(), user = state.user, generation = owner.generation, request = ++collectionIntent;
 const current = () => owner.root.isConnected && owner.current() && request === collectionIntent && generation === owner.generation && state.user === user && (['download','share'].includes(action) || isPlaybackIntent(intent)) && (!settings.alive || settings.alive());
 owner.busy = true;
 const status = settings.status || owner.root.querySelector('[data-collection-status]');
 const buttons = [...owner.root.querySelectorAll(ACTION_CONTROLS)];
 buttons.forEach(button => button.disabled = true); status.textContent = t('Loading songs…');
 try {
  let songs = await resolveCollections(API, collections, current);
  if (action === 'shuffle') songs = selectShuffleSongs(songs, settings.options);
  if (!current()) return;
  if (!songs.length) { status.textContent = action === 'shuffle' ? t('No songs match these options.') : t('No songs available.'); return; }
  const focus = owner.root.querySelector('[data-collection-menu]') || owner.root.querySelector(`[data-collection-${action}]`);
  if (action === 'share') { shareMusicCollection(songs, owner.sourceLabel, focus); status.textContent = ''; return true; }
  if (action === 'download') { downloadMusic(songs, focus); status.textContent = ''; return true; }
  if (await insertQueue(songs, ['replace', 'shuffle'].includes(action) ? 'now' : action, {replace:['replace', 'shuffle'].includes(action), shuffled:action === 'shuffle', source:owner.sourceLabel})) {
   const token = queueUndoToken();
   status.textContent = `Added ${songs.length} ${songs.length === 1 ? 'song' : 'songs'}.`;
   if (token) toast(status.textContent, 'info', {label:'Undo', run:() => undoQueue(token)});
   return true;
  }
 } catch(error) { if (current()) status.textContent = `Could not load songs. ${error.message || t('Check your connection and try again.')}`; }
 finally {
  // A canceled dialog may already have started a newer transaction.
  if (generation === owner.generation) {
   owner.busy = false;
   if (owner.root.isConnected) { if (!current()) status.textContent=''; buttons.forEach(button => button.disabled = !owner.collections().length); }
  }
 }
}
export function queueCollectionSelected(action) {
 const context = activeContext();
 if (!context?.current() || !context.root.isConnected) return false;
 const card = currentElement();
 if (card?.dataset.id) return false;
 if (context.catalog && !context.selecting && card) {
  const key = card.dataset.collectionKey;
  if (key) { void run(context,action,[{kind:context.kind,key}]); return true; }
 }
 if (!context.catalog || context.selecting) { void run(context,action); return true; }
 return false;
}
export function cancelCollectionSelection() {
 const context = activeContext();
 if (context?.catalog && context.current() && context.root.isConnected && context.selecting) { context.cancel(); return true; }
 return false;
}
// Add independent native controls to the existing catalog card layout.
export function bindCollectionCatalog(grid, items, kind, keyName, nameName, current) {
 const heading = grid.querySelector('.grid-full');
 const toolbar = document.createElement('div'); toolbar.className = 'playlist-actions';
 toolbar.innerHTML = html`<button class="btn" data-select-collections>Select ${kind === 'playlist' ? 'playlists' : kind === 'artist' ? 'artists' : 'albums'}</button><span data-batch hidden><span data-batch-count>0 selected</span> ${collectionButtons('Play selected')}</span>`;
 // Shortcut operations also need visible loading/error feedback outside selection mode.
 toolbar.append(toolbar.querySelector('[data-collection-status]'));
 heading.append(toolbar);
 const cards = [...grid.querySelectorAll('.album-card')];
 cards.forEach((card,index) => {
  const item = items[index], key = String(item[keyName]), name = item[nameName];
  card.dataset.collectionKey = key; card.setAttribute('role','group');
  const original = card.onclick;
  const open = document.createElement('button'); open.className = 'btn btn-ghost collection-open';
  open.textContent = `Open ${kind}`; open.setAttribute('aria-label',card.getAttribute('aria-label') || `Open ${kind} ${name}`);
  const check = document.createElement('input'); check.type = 'checkbox'; check.hidden = true;
  check.setAttribute('aria-label',`Select ${name}`); check.dataset.collectionCheck = key;
  card.prepend(check); card.querySelector('.card-body').append(open);
  open.onclick = event => { event.stopPropagation(); original?.(event); };
  card.onclick = event => {
   if (event.target.closest('input, button')) return;
   if (owner.selecting) { check.checked = !check.checked; update(); } else original?.(event);
  };
  check.onchange = () => update();
 });
 const selected = () => cards.filter(card => card.querySelector('input').checked).map(card => ({kind,key:card.dataset.collectionKey}));
 const owner = bindCollectionActions(toolbar, selected, current, `Selected ${kind}s`);
 owner.catalog=true; owner.kind=kind; owner.selecting=false;
 const toggle = toolbar.querySelector('[data-select-collections]');
 const update = () => {
  const count = selected().length;
  owner.pinUpdate?.();
  toolbar.querySelector('[data-batch-count]').textContent = `${count} selected`;
  if (!owner.busy) toolbar.querySelectorAll(ACTION_CONTROLS).forEach(button => button.disabled=!count);
 };
 const setSelecting = value => {
  owner.selecting=value; toolbar.querySelector('[data-batch]').hidden=!value;
  toggle.textContent=value?t('Cancel selection'):`Select ${kind === 'playlist' ? 'playlists' : kind === 'artist' ? 'artists' : 'albums'}`;
  cards.forEach(card=>{const check=card.querySelector('input');check.hidden=!value;if(!value)check.checked=false;});
  update();
 };
 owner.cancel=()=>{owner.generation++;owner.busy=false;toolbar.querySelector('[data-collection-status]').textContent='';setSelecting(false);toggle.focus();};
 toggle.onclick=()=>{if(owner.selecting)owner.cancel();else setSelecting(true);};
 update();
 return owner;
}

function shuffleOptions(owner, opener) {
 const modal = document.getElementById('shuffle-options-modal');
 modal.innerHTML = html`<section class="modal" role="dialog" aria-modal="true" aria-labelledby="shuffle-options-title"><div class="modal-header"><h2 id="shuffle-options-title">Shuffle options</h2><button class="icon-btn" data-cancel aria-label="Close shuffle options">✕</button></div><form class="modal-body">${[['genre',t('Genre')],['from',t('From year')],['to',t('To year')],['maximum',t('Maximum songs')]].map(([name,label]) => html`<label>${label} (optional)<input name="${name}" aria-label="${label} (optional)" ${name === 'genre' ? '' : 'inputmode="numeric"'}></label>`).join('')}<p>Blank includes every song. Applies to this collection only.</p><p role="status" data-shuffle-status></p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button type="submit" class="btn btn-primary">Shuffle</button></div></form></section>`;
 const panel = modal.firstElementChild;
 const alive = () => modal.classList.contains('open') && modal.firstElementChild === panel;
 const close = () => {
  modal.classList.remove('open');
  if (owner.busy) {
   // Cancel ownership immediately; late completion cannot unlock a newer action.
   owner.generation++; owner.busy = false;
   if (owner.root.isConnected) {
    owner.root.querySelectorAll(ACTION_CONTROLS).forEach(button => button.disabled = !owner.collections().length);
   }
  }
  if (opener?.isConnected) opener.focus();
 };
 // This dialog owns Escape before the global close path selects a library card.
 modal.onkeydown = event => {
  if (event.key === 'Escape' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.isComposing) {
   event.preventDefault(); event.stopPropagation(); close();
  }
 };
 modal.querySelectorAll('[data-cancel]').forEach(button => button.onclick = close);
 const form = modal.querySelector('form'), status = modal.querySelector('[data-shuffle-status]');
 form.onsubmit = async event => {
  event.preventDefault(); if (owner.busy) return;
  let options;
  try { options = parseShuffleOptions(Object.fromEntries(new FormData(form))); }
  catch (error) { status.textContent = error.message; return; }
  const submit = form.querySelector('[type="submit"]'); submit.disabled = true;
  const success = await run(owner, 'shuffle', null, {options, alive, status});
  if (alive()) { submit.disabled = false; if (success) close(); }
 };
 modal.classList.add('open');
}
