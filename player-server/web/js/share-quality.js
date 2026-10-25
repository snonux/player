import { escapeHtml as esc } from './utils.js';

// The profile IDs and labels match M50's server allowlist. Actual choices are
// loaded from that endpoint; the fallback keeps saved policies visible offline.
export function shareQualityLabel(id = '') {
  return ({'':'Auto (compatibility when needed)', original:'Original (no conversion)', aac128:'Balanced · AAC 128 kbps', aac256:'High · AAC 256 kbps', mp3192:'Compatible · MP3 192 kbps', opus96:'Data saver · Opus 96 kbps'})[id] || `Unavailable profile (${id})`;
}
export function shareQualityEditor() {
  return '<details data-share-quality><summary tabindex="0">Audio quality</summary><label>Shared playback and downloads <select name="audio_quality" aria-label="Shared audio quality"></select></label><p class="text-sm text-muted">Auto converts playback only when needed. Original needs a compatible player. Selected encodes apply to playback and downloads. Bitrates are nominal targets.</p><span role="status"></span><button type="button" class="btn btn-sm" data-reload>Reload profiles</button></details>';
}
export function bindShareQuality(root, saved = '') {
  const panel=root.querySelector('[data-share-quality]'); if (!panel) return;
  const select=panel.querySelector('select'), status=panel.querySelector('[role=status]');
  let initialized=false;
  function options(profiles=[]) {
    const current=initialized ? select.value : saved;
    initialized=true;
    const choices=new Map([['',shareQualityLabel('')],['original',shareQualityLabel('original')],...profiles.map(p=>[p.id,p.label])]);
    if (!choices.has(current)) choices.set(current,shareQualityLabel(current));
    select.innerHTML=[...choices].map(([id,label])=>`<option value="${esc(id)}">${esc(label)}</option>`).join(''); select.value=current;
  }
  options();
  async function load() {
    status.textContent='Loading server profiles…';
    try { const response=await fetch('/api/stream-profiles'); if(!response.ok)throw Error(); const profiles=await response.json(); if(!panel.isConnected)return; options(profiles); status.textContent='Profiles ready.'; }
    catch { if(panel.isConnected)status.textContent='Profiles unavailable. Auto and Original remain available. Reload to retry.'; }
  }
  panel.querySelector('[data-reload]').onclick=load;
  panel.addEventListener('keydown',event=>{ if(event.target.tagName==='SUMMARY' && ['Enter',' '].includes(event.key))event.stopPropagation(); });
  load();
}
