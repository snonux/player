const key = 'player-stream-quality';
export function streamQuality() {
  try { return localStorage.getItem(key) || 'auto'; } catch { return 'auto'; }
}
export function qualityPlaybackUrl(media) {
  const selected = streamQuality();
  if (media.type !== 'audio' || selected === 'auto') return `/api/media/${media.id}/${media.transcoded ? 'compat' : 'stream'}`;
  if (selected === 'original') return `/api/media/${media.id}/stream`;
  return `/api/media/${media.id}/compat?profile=${encodeURIComponent(selected)}`;
}
export function initStreamQuality(player) {
  const controls = player?.querySelector('.controls');
  if (!controls || !document.createElement) return;
  const panel = document.createElement('details');
  panel.className = 'loudness-control';
  panel.innerHTML = '<summary>Stream quality</summary><label>Quality <select aria-label="Stream quality"></select></label><p>Saved on this browser; applies next time you open a track. Auto uses compatibility conversion when needed. Original may fail if your browser cannot decode it. Local files, downloads, shared links and Cast use their own playback policy.</p><span role="status"></span><button type="button" class="btn btn-sm">Reload profiles</button>';
  const select = panel.querySelector('select'), status = panel.querySelector('[role="status"]');
  if (!select || !status) return;
  panel.addEventListener('keydown', event => {
    if (event.target.tagName === 'SUMMARY' && ['Enter', ' '].includes(event.key)) event.stopPropagation();
    if (event.key === 'Escape' && panel.open) { event.preventDefault(); event.stopPropagation(); panel.open = false; panel.querySelector('summary').focus(); }
  });
  function options(profiles = []) {
    select.replaceChildren();
    for (const p of [{id:'auto',label:'Auto (compatibility when needed)'},{id:'original',label:'Original (no conversion)'},...profiles]) {
      const o = document.createElement('option'); o.value=p.id; o.textContent=p.label; select.appendChild(o);
    }
    const saved = streamQuality();
    if (![...select.options].some(o => o.value === saved)) { const o=document.createElement('option'); o.value=saved; o.textContent=`Unavailable profile (${saved})`; select.appendChild(o); }
    select.value=saved;
  }
  options();
  async function load() {
    status.textContent='Loading server profiles…';
    try { const response=await fetch('/api/stream-profiles'); if (!response.ok) throw Error(); options(await response.json()); status.textContent='Server profiles ready.'; }
    catch { status.textContent='Server profiles unavailable. Choose Auto or Original, or reload.'; }
  }
  select.addEventListener('change', () => { try { localStorage.setItem(key,select.value); status.textContent='Saved. Applies when you next open a track.'; } catch { status.textContent='Could not save quality on this browser.'; options(); } });
  panel.querySelector('button').addEventListener('click', load);
  controls.appendChild(panel); load();
}
