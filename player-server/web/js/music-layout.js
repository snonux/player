import { t, html, plural, ui } from './i18n.js';
import { state } from './state.js';
import { escapeHtml as esc, fmtDur, toast } from './utils.js';

export const musicColumns = {
 albums: {rating:'Rating',artist:'Artist',year:'Year',song_count:'Songs',duration:'Duration'},
 songs: {rating:'Rating',artist:'Artist',album:'Album',year:'Year',duration:'Duration'},
};
let pendingFocus = '';
function key(kind) { return `player.music-layout.v1:${location.origin}:${state.user?.id}:${kind}`; }
export function normalizeMusicLayout(value, kind) {
 const columns=musicColumns[kind];
 return {view:kind==='albums' && value?.view==='table'?'table':'grid',
  sort:['title',...Object.keys(columns)].includes(value?.sort)?value.sort:'title',
  descending:value?.descending===true,
  columns:Array.isArray(value?.columns)?Object.keys(columns).filter(key=>value.columns.includes(key)):kind==='albums'?['artist','year']:['artist','album','duration']};
}
export function readMusicLayout(kind) {
 try { return normalizeMusicLayout(JSON.parse(localStorage.getItem(key(kind))),kind); }
 catch { return normalizeMusicLayout(null,kind); }
}
export function writeMusicLayout(kind, layout) {
 try { localStorage.setItem(key(kind), JSON.stringify(normalizeMusicLayout(layout, kind))); return true; }
 catch { return false; }
}
function value(item, column, kind) {
 if(kind==='albums' || column==='rating') return item[column];
 return column==='title' ? item.music?.title || item.file_name : column==='duration' ? item.duration : item.music?.[column];
}
export function sortMusic(items, layout, kind) {
 return [...items].sort((a,b)=>{
  const av=value(a,layout.sort,kind),bv=value(b,layout.sort,kind);
  const missing=v=>v===null || v===undefined || v==='' || (layout.sort==='year' && !v);
  if(missing(av)!==missing(bv))return missing(av)?1:-1;
  const compare=typeof av==='number' && typeof bv==='number'?av-bv:String(av || '').localeCompare(String(bv || ''),undefined,{numeric:true,sensitivity:'base'});
  return (layout.descending?-compare:compare) || String(a.album_key || a.id).localeCompare(String(b.album_key || b.id));
 });
}
export function musicColumnText(item,column,kind) {
 const text=value(item,column,kind);
 return column==='duration'?fmtDur(Number(text || 0)):String(text || '—');
}
export function musicLayoutToolbar(kind,layout) {
 return html`<div class="grid-full music-layout-controls" data-music-layout="${kind}"><label>Sort by <select data-layout-sort aria-label="${t(kind==='albums'?'Album sort':'Song sort')}" data-i18n-aria-label="${esc(JSON.stringify({key:kind==='albums'?'Album sort':'Song sort'}))}">${Object.entries({title:'Title',...musicColumns[kind]}).map(([key,label])=>html`<option value="${key}" data-i18n="${esc(label)}" ${layout.sort===key?'selected':''}>${t(label)}</option>`).join('')}</select></label><button class="btn" data-layout-direction aria-label="Sort direction: ${layout.descending?'descending':'ascending'}">${ui(layout.descending?'↓ Descending':'↑ Ascending')}</button>${kind==='albums'?html`<label>View <select data-layout-view aria-label="Album view"><option value="grid" ${layout.view==='grid'?'selected':''}>Grid</option><option value="table" ${layout.view==='table'?'selected':''}>Table</option></select></label>`:''}<details><summary class="btn">Columns</summary><div class="music-column-options"><span>Title is always shown</span>${Object.entries(musicColumns[kind]).map(([key,label])=>html`<label><input type="checkbox" data-layout-column="${key}" ${layout.columns.includes(key)?'checked':''}>${ui(label)}</label>`).join('')}</div></details></div>`;
}
export function bindMusicLayout(grid,kind,layout,reload,before=()=>{}) {
 const toolbar=grid.querySelector(`[data-music-layout="${kind}"]`);
 if(!toolbar)return;
 const update=(patch,focus)=>{
  before();
  const next={...layout,...patch};
  try { localStorage.setItem(key(kind),JSON.stringify(next)); }
  catch { toast('Could not remember music layout. Check browser storage.','error');return; }
  pendingFocus=focus; reload();
 };
 toolbar.querySelector('details').onkeydown=event=>{
  const disclosure=event.currentTarget;
  // Keep native summary activation while preventing library Enter/play keys.
  // Checkbox keys and all other shortcuts retain their existing owners.
  if(event.target===disclosure.querySelector('summary') && ['Enter',' '].includes(event.key) && !event.ctrlKey && !event.metaKey && !event.altKey) {
   event.stopPropagation();
  }
  if(event.key==='Escape'){
   event.stopPropagation();event.preventDefault();disclosure.open=false;disclosure.querySelector('summary').focus();
  }
 };
 toolbar.querySelector('[data-layout-sort]').onchange=e=>update({sort:e.target.value},'[data-layout-sort]');
 toolbar.querySelector('[data-layout-direction]').onclick=()=>update({descending:!layout.descending},'[data-layout-direction]');
 toolbar.querySelector('[data-layout-view]')?.addEventListener('change',e=>update({view:e.target.value},'[data-layout-view]'));
 toolbar.querySelectorAll('[data-layout-column]').forEach(input=>input.onchange=()=>update({columns:[...toolbar.querySelectorAll('[data-layout-column]:checked')].map(input=>input.dataset.layoutColumn)},`[data-layout-column="${input.dataset.layoutColumn}"]`));
 if(pendingFocus){const control=toolbar.querySelector(pendingFocus);if(control){if(control.closest('details'))control.closest('details').open=true;control.focus();}pendingFocus='';}
}
