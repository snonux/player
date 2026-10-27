// Explicit per-window sharing, independent of playback progress/history writes.
import { state } from './state.js';
import { listeningOccurrenceID } from './listening-progress.js';
import { currentLibraryPresenceSnapshot } from './playback.js';
import { rendererActive } from './renderer.js';

import { createMusicPresence } from './music-presence-controller.js';

export function localMusicPresenceSnapshot() {
  return rendererActive() ? null : currentLibraryPresenceSnapshot();
}

export function mountMusicPresence(parent, { getSnapshot = localMusicPresenceSnapshot, getAccount = () => state.user?.id } = {}) {
  const panel = document.createElement('details');
  panel.className = 'card-body music-column music-presence';
  panel.innerHTML = '<summary>Listeners</summary><label><input type="checkbox"> Share my listening</label><p class="text-muted">Off by default. Sharing shows your username, client and current song to signed-in people who can access that song. Local server-library music only; downloads, public shares, radio and remote output are excluded.</p><p role="status" data-presence-status></p><button class="btn" type="button" data-presence-retry>Retry</button><ul data-presence-list></ul>';
  parent.append(panel);
  const summary = panel.querySelector('summary');
  panel.addEventListener('keydown', event => {
    if (event.target === summary && ['Enter',' '].includes(event.key)) event.stopPropagation();
    if (event.key === 'Escape' && panel.open) { event.preventDefault(); event.stopPropagation(); panel.open = false; summary.focus(); }
  });
  const toggle = panel.querySelector('input'), list = panel.querySelector('ul'), status = panel.querySelector('[data-presence-status]'), retry = panel.querySelector('button');
  const controller = createMusicPresence({
    clientID: listeningOccurrenceID(), snapshot: getSnapshot,
    async request(method, owner, body) {
      const response = await fetch(`/api/music/presence?expected_user_id=${owner}`, { method, credentials:'include', cache:'no-store', signal:AbortSignal.timeout(8000), headers: { 'Content-Type':'application/json' }, ...(body ? {body:JSON.stringify(body)} : {}) });
      if (!response.ok) throw Error('Listeners unavailable');
      return response.status === 204 ? null : response.json();
    },
    render(value) {
      toggle.checked = value.sharing;
      status.textContent = value.message || (value.open && !value.rows.length ? 'No listeners sharing right now.' : '');
      retry.hidden = !value.message.includes('unavailable');
      list.replaceChildren();
      for (const row of value.rows) {
        const item = document.createElement('li');
        const title = document.createElement('p'), song = document.createElement('p'), progress = document.createElement('p');
        title.textContent = `${row.username} · ${row.client} · ${row.state === 'playing' ? 'Playing' : 'Paused'}`;
        song.textContent = [row.title, row.artist].filter(Boolean).join(' · ');
        progress.textContent = `${presenceTime(row.position_seconds)} / ${presenceTime(row.duration)}`;
        item.append(title,song,progress); list.append(item);
      }
    },
  });
  let account;
  const sync = () => { const next = getAccount(); if (next !== account) { account = next; controller.setAccount(next || null); } void controller.refresh(); };
  toggle.addEventListener('change', () => { const checked = toggle.checked; sync(); controller.setSharing(checked); });
  panel.addEventListener('toggle', () => controller.setOpen(panel.open));
  retry.addEventListener('click', () => void controller.refresh());
  const events = ['play','pause','seeked','ended','error','emptied','loadeddata'];
  const audio = document.getElementById('media-audio');
  for (const name of events) audio?.addEventListener(name,sync);
  document.addEventListener('player:media-opened',sync);
  const interval = setInterval(sync,10000), identityInterval = setInterval(() => { if (getAccount() !== account) sync(); },500);
  const pagehide = () => { account = undefined; controller.clear(); };
  const pageshow = event => { if (event.persisted) { account = undefined; sync(); } };
  window.addEventListener('pagehide',pagehide);
  window.addEventListener('pageshow',pageshow);
  sync();
  return () => { clearInterval(interval); clearInterval(identityInterval); for (const name of events) audio?.removeEventListener(name,sync); document.removeEventListener('player:media-opened',sync); window.removeEventListener('pagehide',pagehide); window.removeEventListener('pageshow',pageshow); controller.clear(); panel.remove(); };
}

function presenceTime(seconds) { const value = Math.max(0,Math.floor(Number(seconds)||0)); return `${Math.floor(value/60)}:${String(value%60).padStart(2,'0')}`; }
