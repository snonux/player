import { API } from './api.js';
import { state } from './state.js';
import { insertQueue, queueState } from './queue.js';
import { beginPlaybackIntent, currentPlaybackIntent, isPlaybackIntent } from './playback-intent.js';

// Ordinary native controls preserve global shortcuts and dialog focus handling.
export function mountMusicMix(host, kind, key, current, { insert = insertQueue, queue = queueState } = {}) {
 const section = document.createElement('section');
 section.className = 'grid-full';
 section.setAttribute('aria-label', kind === 'artist' ? 'Artist radio' : 'Instant Mix');
 section.innerHTML = `<h2>${kind === 'artist' ? 'Artist radio' : 'Instant Mix'}</h2><p>From this ${kind} · related songs in your library</p><p class="text-muted">Shared artist credits and genres · up to 50 distinct songs</p><label>Minimum song rating <select aria-label="Minimum song rating"><option value="0">Any rating</option>${[1,2,3,4,5].map(n=>`<option value="${n}">${n}+ stars</option>`).join('')}</select></label> <button class="btn">${kind === 'artist' ? 'Artist radio' : 'Start mix'}</button><p role="status" aria-live="polite"></p>`;
 host.append(section);
 const button = section.querySelector('button'), select = section.querySelector('select'), status = section.querySelector('[role="status"]');
 const alive = () => section.isConnected && current();
 button.onclick = async () => {
  if (queue().loading) { status.textContent = 'Loading track… Please wait.'; return; }
  const user = state.user, intent = beginPlaybackIntent();
  let completionIntent = intent;
  const active = () => alive() && state.user === user && isPlaybackIntent(completionIntent);
  button.disabled = true; select.disabled = true; status.textContent = 'Finding mix songs…';
  try {
   const result = await API.musicMix(kind,key,Number(select.value));
   if (!active()) return;
   if (!result.songs.length) { status.textContent = `No matching songs for ${result.title}. Try a lower rating or add music tags.`; return; }
   const pendingInsertion = insert(result.songs,'now',{replace:true,source:`${kind === 'artist' ? 'Artist radio' : 'Mix'} · ${result.title}`});
   // Queue playback claims its own intent synchronously; retain that claim for
   // completion while allowing any subsequent source action to supersede it.
   completionIntent = currentPlaybackIntent();
   const started = await pendingInsertion;
   if (active()) status.textContent = started ? `${kind === 'artist' ? 'Artist radio' : 'Mix'} · ${result.title} · ${result.songs.length} ${result.songs.length === 1 ? 'song' : 'songs'}` : 'Playback was not started. Try again.';
  } catch (error) {
   if (active()) status.textContent = `Could not start mix. ${error.message || 'Check your connection and library access, then try again.'}`;
  } finally {
   if (alive()) { button.disabled = false; select.disabled = false; if (status.textContent === 'Finding mix songs…') status.textContent = ''; }
  }
 };
 return section;
}
