import { listeningProgressAccount } from './listening-progress.js';
import { API } from './api.js';

// History stays in the existing Settings dialog, retaining its focus trap and
// Escape hierarchy. Controls use native form semantics without new shortcuts.
export function initListeningHistory() {
  const host = document.getElementById('settings-modal')?.querySelector('.modal');
  if (!host) return;
  const panel = document.createElement('details');
  panel.className = 'listening-history';
  panel.innerHTML = '<summary>Listening history</summary><p class="text-muted text-sm">New music plays appear after 60 counted listening seconds. Offline plays appear after syncing. Earlier personal counters are not backfilled. History is separate from counters and resume.</p><p data-status role="status"></p><div class="appearance-controls"><label><input type="checkbox" data-record> Record listening history</label><label>Keep history <select data-days><option value="30">30 days</option><option value="90">90 days</option><option value="365">1 year</option><option value="0">Forever</option></select></label></div><button type="button" class="btn btn-ghost" data-refresh>Refresh history</button> <button type="button" class="btn btn-ghost" data-clear>Clear history</button><div data-confirm hidden><p data-question></p><button type="button" class="btn btn-ghost" data-cancel>Cancel</button> <button type="button" class="btn btn-danger" data-confirm>Confirm</button></div><ol data-events></ol><button type="button" class="btn btn-ghost" data-more hidden>Show older plays</button>';
  host.appendChild(panel);
  const q = selector => panel.querySelector(selector);
  let loaded = false, busy = false, cursor = {}, settings = {recording: true, retention_days: 365}, pending = null;
  function controls() { for (const el of panel.querySelectorAll('button,input,select')) el.disabled = busy || (!loaded && !el.matches('[data-refresh]')); }
  function sync() { q('[data-record]').checked = settings.recording; q('[data-days]').value = String(settings.retention_days); }
  function status(message) { q('[data-status]').textContent = message; }
  function ask(message, action, opener) {
    const owner = listeningProgressAccount();
    pending = {action: () => {if (owner === listeningProgressAccount()) action(owner);}, opener}; q('[data-question]').textContent = message; q('[data-confirm]').hidden = false; q('[data-cancel]').focus();
  }
  function cancel() { const opener = pending?.opener; pending = null; q('[data-confirm]').hidden = true; sync(); opener?.focus(); }
  async function load(more = false) {
    if (busy) return;
    const owner = listeningProgressAccount();
    busy = true; controls(); status('Loading listening history…');
    try {
      const result = await API.listeningHistory(more ? cursor : {}, owner);
      if (owner !== listeningProgressAccount()) return;
      settings = result.settings; sync(); loaded = true;
      cursor = result.next_before_id ? {before_id: result.next_before_id, before_ms: result.next_before_ms} : {};
      if (!more) q('[data-events]').replaceChildren();
      for (const event of result.events) {
        const li = document.createElement('li');
        li.textContent = `${event.title}${event.artist ? ` · ${event.artist}` : ''} · ${new Date(event.played_at).toLocaleString()}`;
        q('[data-events]').appendChild(li);
      }
      q('[data-more]').hidden = !cursor.before_id;
      status(q('[data-events]').children.length ? (settings.recording ? 'Recent qualified plays, newest first.' : 'Recording paused. Plays from this paused period will not be added later.') : 'No recorded plays.');
    } catch { status('Could not load listening history. Connect to the server and retry.'); }
    finally { busy = false; controls(); }
  }
  async function change(action) {
    busy = true; controls();
    try { await action(); busy = false; await load(); }
    catch { status('History could not be changed. Connect to the server and retry.'); sync(); }
    finally { busy = false; controls(); }
  }
  q('[data-refresh]').onclick = () => load();
  q('[data-more]').onclick = () => load(true);
  q('[data-record]').onchange = () => change(() => API.configureListeningHistory({...settings, recording: q('[data-record]').checked}, listeningProgressAccount()));
  q('[data-days]').onchange = () => { const days = Number(q('[data-days]').value); ask('Shortening retention permanently removes older history. Increasing it cannot restore removed plays.', owner => change(() => API.configureListeningHistory({...settings, retention_days: days}, owner)), q('[data-days]')); };
  q('[data-clear]').onclick = () => ask('Erase history for this account on every device? Personal counters and resume remain. This cannot be undone.', owner => change(() => API.clearListeningHistory(owner)), q('[data-clear]'));
  q('[data-cancel]').onclick = cancel;
  q('button[data-confirm]').onclick = () => { const action = pending?.action; cancel(); action?.(); };
  panel.addEventListener('keydown', event => { if (event.key === 'Escape' && pending) {event.preventDefault(); event.stopImmediatePropagation(); cancel();} }, true);
  panel.addEventListener('toggle', () => { if (panel.open && !loaded) void load(); });
  document.addEventListener('listening-progress:account', () => { loaded = false; busy = false; cursor = {}; q('[data-events]').replaceChildren(); q('[data-more]').hidden = true; cancel(); controls(); status('Refresh history for this account.'); });
  document.addEventListener('listening-progress:error', event => status(event.detail));
  controls();
}
