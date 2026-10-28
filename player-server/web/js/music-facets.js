import { t, html, plural } from './i18n.js';
import { API } from './api.js';
import { state } from './state.js';
import { escapeHtml as esc } from './utils.js';
import { selectedLibraryIds, clearSelectedLibraries } from './music-libraries.js';
export const facetKeys=['genre','year','role','release_type','compilation','mood','grouping','label'];
export const facetParams=filter=>Object.fromEntries(Object.entries(filter || {}).filter(([,v])=>v));
export const hasFacets=view=>Object.keys(facetParams(view.facets)).length>0;
const labels={genre:'Genre',year:'Year / decade',role:'Role',release_type:'Release type',compilation:'Compilation',mood:'Mood',grouping:'Grouping',label:'Label'};
const roleLabels={track_artist:'Track artist',album_artist:'Album artist',composer:'Composer',performer:'Performer'};
const display=(key,value)=>key==='role' ? roleLabels[value] || value : key==='compilation' ? value==='true'?'Marked':'Not marked' : value;
function activeFilterCount(view) {
 const facets=Object.keys(facetParams(view.facets)).length;
 return facets + (view.favorites?1:0) + (view.rated?1:0) + (selectedLibraryIds().length?1:0);
}
// A toolbar edit takes ownership of its dimensions and removes their typed
// modifiers so the next text edit cannot silently resurrect a removed chip.
export function editMusicFacets(view,key,value) {
 const keys=key ? [key] : facetKeys;
 const explicitFacets={...(view.explicitFacets ?? view.facets ?? {})};
 if(!view.explicitFacets) for(const modifier of Object.keys(view.queryFacets || {})) delete explicitFacets[modifier];
 const queryFacets={...view.queryFacets};
 for(const dimension of keys){delete queryFacets[dimension];delete explicitFacets[dimension];}
 if(key && value) explicitFacets[key]=value;
 if(view.search) {
  const input=document.getElementById?.('search-input');
  if(input) input.value=(input.value.match(/(?:[^\s"]+|"[^"]*")+/gu) || []).filter(token=>!keys.some(dimension=>token.toLowerCase().startsWith(`${dimension}:`))).join(' ');
 }
 return {...view,facets:{...explicitFacets,...queryFacets},...(view.search?{explicitFacets,queryFacets}:{}),...(view.artists && (!key || key==='role')?{role:value || 'artists'}:{})};
}
function bindDisclosureKeys(details) {
 details?.addEventListener('keydown',event=>{
  if(event.target?.tagName==='SUMMARY'&&['Enter',' '].includes(event.key))event.stopPropagation();
  if(event.key==='Escape'&&event.currentTarget.open){event.preventDefault();event.stopPropagation();event.currentTarget.open=false;event.currentTarget.querySelector('summary').focus();}
 });
}
export async function mountMusicFacets(grid,view,current,reload,loading=false) {
 if(view.album || view.artist && view.artists || view.playlists)return;
 let options,failed=false;try {options=loading?{}:await API.musicFacets();}catch {options={};failed=true;}if(!current())return;
 const selected=view.facets || {};
 const years=options.year || [];options.year=[...new Set([...years,...years.map(y=>`${Math.floor(Number(y)/10)*10}-${Math.floor(Number(y)/10)*10+9}`)])].sort().reverse();
 if(options.compilation?.length) options.compilation=['true','false'];
 const select=key=>{if(key==='role' && view.artists)return '';const values=[...new Set([...(options[key] || []),...(selected[key]?[selected[key]]:[])])].filter(Boolean);return values.length?html`<label>${labels[key]} <select data-music-facet="${key}" aria-label="${labels[key]}"><option value="">Any</option>${values.map(v=>html`<option value="${esc(v)}" ${selected[key]===v?'selected':''}>${esc(display(key,v))}</option>`).join('')}</select></label>`:'';};
 const facetCount=Object.keys(facetParams(selected)).length;
 const count=activeFilterCount(view);
 const panel=document.createElement('div');panel.className='grid-full music-facets';
 panel.innerHTML=html`${failed?html`<p role="status">Could not load filter options. <button class="btn" data-retry-facets>Retry filters</button></p>`:''}<details class="music-filters" data-music-filters><summary>${t('Filters')}${count?` (${count})`:''}</summary><div class="music-facet-controls">${select('genre')}${select('year')}<details data-more-facets><summary>More filters${facetCount?` (${facetCount})`:''}</summary><div class="music-facet-controls">${facetKeys.slice(2).map(select).join('')}</div></details><div data-music-filter-extras class="music-filter-extras"></div></div><div class="music-facet-chips">${Object.entries(facetParams(selected)).map(([key,value])=>html`<button class="btn btn-ghost" data-remove-facet="${key}" aria-label="Remove ${labels[key]}: ${esc(display(key,value))}">${labels[key]}: ${esc(display(key,value))} ×</button>`).join('')}${count?html`<button class="btn" data-clear-facets>Clear filters</button>`:''}</div></details>`;
 grid.prepend(panel);
 panel.querySelector('[data-retry-facets]')?.addEventListener('click',reload);
 const filters=panel.querySelector('[data-music-filters]');
 const more=panel.querySelector('[data-more-facets]');
 const apply=(key,value)=>{state.music={...editMusicFacets(state.music,key,value),focusFacet:key,filtersOpen:filters.open,moreFacets:more.open};reload();};
 panel.querySelectorAll('select').forEach(input=>input.onchange=()=>apply(input.dataset.musicFacet,input.value));
 panel.querySelectorAll('[data-remove-facet]').forEach(button=>button.onclick=()=>apply(button.dataset.removeFacet,''));
 panel.querySelector('[data-clear-facets]')?.addEventListener('click',()=>{
  clearSelectedLibraries();
  state.music={...editMusicFacets(state.music),favorites:false,rated:false,filtersOpen:filters.open,moreFacets:more.open};
  reload();
 });
 filters.open=Boolean(view.filtersOpen || view.focusFacet || count);
 more.open=Boolean(view.moreFacets);
 filters.addEventListener('toggle',()=>{if(current())state.music.filtersOpen=filters.open;});
 more.addEventListener('toggle',()=>{if(current())state.music.moreFacets=more.open;});
 if(view.focusFacet) panel.querySelector(`[data-music-facet="${view.focusFacet}"]`)?.focus();
 bindDisclosureKeys(filters);
 bindDisclosureKeys(more);
}
