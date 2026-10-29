import { state } from './state.js';
import { toast } from './utils.js';
export const setSortFields = {name:'Name',date:'Date added',play_count:'Playbacks',size:'Size',duration:'Duration',date_taken:'Date taken',random:'Random'};
export function normalizeSetSort(value) {
  return value === 'random' ? value : /^(name|date|play_count|size|duration|date_taken)_(asc|desc)$/.test(value || '') ? value : 'name_asc';
}
const key = setId => `player-set-sort:${state.user?.id || 0}:${setId}`;
export function readSetSort(setId) {
  if (!state.user?.id) return 'name_asc';
  try { return normalizeSetSort(localStorage.getItem(key(setId))); } catch { return 'name_asc'; }
}
export function bindSetSort(setId, reload) {
  const menu = document.getElementById('set-sort');
  if (!menu) return;
  menu.hidden = !setId || !!state.music || !!state.virtualSet;
  if (menu.hidden) return;
  const searchSort = state.filters.sort;
  const legacy = ['date','play_count'].includes(searchSort) ? `${searchSort}_desc` : `${searchSort}_asc`;
  const value = searchSort ? normalizeSetSort(searchSort === 'random' || /_(asc|desc)$/.test(searchSort) ? searchSort : legacy) : readSetSort(setId);
  const split = value.lastIndexOf('_'), field = split < 0 ? value : value.slice(0,split), direction = value.slice(split+1);
  const focused = menu.contains(document.activeElement) ? document.activeElement.getAttribute('aria-label') : null;
  menu.innerHTML = `<summary>Sort: ${setSortFields[field]}</summary><label>Order by <select aria-label="Sort by">${Object.entries(setSortFields).map(([id,label])=>`<option value="${id}" ${id===field?'selected':''}>${label}</option>`).join('')}</select></label><label>Direction <select aria-label="Sort direction" ${field==='random'?'disabled':''}><option value="asc" ${direction==='asc'?'selected':''}>Ascending</option><option value="desc" ${direction==='desc'?'selected':''}>Descending</option></select></label><p>Folders first, alphabetical. Unknown metadata appears last.</p>`;
  menu.querySelector('summary').onkeydown = event => {
    // The library's Enter/Space shortcuts must leave this native disclosure usable.
    if (event.key === 'Enter' || event.key === ' ') event.stopPropagation();
    if (event.key === 'Escape') { menu.open = false; event.stopPropagation(); }
  };
  if (focused) menu.querySelector(`[aria-label="${focused}"]`)?.focus();
  menu.querySelectorAll('select').forEach(select => select.onchange = () => {
    const selects = menu.querySelectorAll('select');
    const next = selects[0].value === 'random' ? 'random' : `${selects[0].value}_${selects[1].value}`;
    try { localStorage.setItem(key(setId), next); }
    catch { toast('Could not save sort preference', 'error'); return; }
    if (state.filters.sort) {
      state.filters.sort = next;
      const search = document.getElementById('search-input');
      if (search) search.value = search.value.replace(/\bsort:\S+/g, `sort:${next}`);
    }
    reload({page:0});
  });
}
