import { mountMusicHome } from './music-home.js';
import { escapeHtml as esc } from './utils.js';
import { collectionButtons, bindCollectionActions } from './collection-actions.js';

export const personalCollections = ['liked_mix','rediscover'];
export const albumCollections = {
 recently_added: ['Recently added', 'Latest songs added to your accessible library.', 'No albums yet. Ask an administrator to scan music.'],
 recently_played: ['Recently played', 'Your latest qualified music plays.', 'Play music to start your listening history.'],
 most_played: ['Most played', 'Albums ranked by your qualified song plays.', 'No qualified plays yet. Listen to music to build this collection.'],
 random: ['Random albums', 'A fresh selection from your accessible library.', 'No albums available.'],
 favorites: ['Favorite albums', 'Albums you have favorited.', 'Favorite an album using its heart button.'],
 top_rated: ['Top rated albums', 'Albums ranked by your own star ratings.', 'Rate an album to add it here.'],
 liked_mix: ['Liked-album mix', 'A shuffled selection of albums you have favorited. Plays whole albums.', 'Favorite an album to start your personal mix.'],
 rediscover: ['Rediscover older plays', 'Previously played albums, oldest latest qualified play first. Uses your local listening data, not an anniversary.', 'Play music to build your personal listening history.'],
};
export function discoverAlbums(albums, collection, random = Math.random) {
 const items=albums.filter(a => ['recently_played','rediscover'].includes(collection)?a.last_played_at:collection==='most_played'?a.personal_play_count>0:['favorites','liked_mix'].includes(collection)?a.is_favorite:collection==='top_rated'?a.rating>0:true);
 if(['random','liked_mix'].includes(collection)) {
  for(let i=items.length-1;i>0;i--){const j=Math.floor(random()*(i+1));[items[i],items[j]]=[items[j],items[i]];}
  return items;
 }
 const value=a=>collection==='recently_added'?Date.parse(a.added_at)||0:collection==='rediscover'?-(Date.parse(a.last_played_at)||0):collection==='recently_played'?Date.parse(a.last_played_at)||0:collection==='most_played'?a.personal_play_count:collection==='top_rated'?a.rating:0;
 const title=a=>a.title.toLowerCase().trim().replace(/\s+/gu,' ');
 const compare=(a,b)=>a<b?-1:a>b?1:0;
 return items.sort((a,b)=>value(b)-value(a)||compare(title(a),title(b))||compare(a.album_key,b.album_key));
}
// Preview actions use the same permission-aware collection resolver and queue actor.
export function mountAlbumDiscovery(heading, albums, current, openCollection, openAlbum, view, openPlaylist) {
 const panel=document.createElement('details'); panel.setAttribute('aria-label','Discover albums');
 panel.innerHTML='<summary class="btn">Music home · Discover albums</summary>';
 panel.open=Boolean(view.discoveryOpen);
 panel.addEventListener('toggle',()=>{if(current())view.discoveryOpen=panel.open;});
 panel.onkeydown=event=>{if(event.target.tagName==='SUMMARY' && ['Enter',' '].includes(event.key))event.stopPropagation();if(event.key==='Escape' && panel.open){event.preventDefault();event.stopPropagation();panel.open=false;panel.querySelector('summary').focus();}};
 mountMusicHome(panel,{...Object.fromEntries(Object.entries(albumCollections).map(([key,value])=>[key,value[0]])),pinned:'Pinned playlists'},(rows,key)=>{
  const [title,source,empty]=albumCollections[key];
  const items=discoverAlbums(albums,key), row=document.createElement('section');
  if(['random','liked_mix'].includes(key)) {
   view.discoveryOrders ||= {};
   const saved=view.discoveryOrders[key] || (key==='random'?view.discoveryRandomKeys:null);
   if(saved) {const ranks=new Map(saved.map((key,i)=>[key,i]));items.sort((a,b)=>(ranks.get(a.album_key) ?? ranks.size)-(ranks.get(b.album_key) ?? ranks.size));}
   view.discoveryOrders[key]=items.map(a=>a.album_key);
  }
  row.className='music-heading'; row.setAttribute('aria-label',title);
  row.innerHTML=`<h3>${title}</h3><p class="text-muted">${source}</p>${items.length?`<p>${items.length} albums</p><div class="playlist-actions">${items.slice(0,4).map(a=>`<button class="btn btn-ghost queue-track" data-preview="${esc(a.album_key)}">${esc(a.title)} · ${esc(a.artist)}</button>`).join('')}</div>`:`<p>${empty}</p>`}${collectionButtons(`Play all ${items.length} albums`)}<button class="btn" data-view-all>View all ${title.toLowerCase()}</button>`;
  const actions=row.querySelector('[data-collection-actions]');
  actions.querySelector('[data-collection-play]').disabled=!items.length;
  if(!items.length)actions.querySelectorAll('[data-collection-shuffle], [data-collection-menu]').forEach(button=>button.disabled=true);
  bindCollectionActions(actions,()=>items.map(a=>({kind:'album',key:a.album_key})),current,title);
  row.querySelector('[data-view-all]').onclick=()=>openCollection(key,items);
  row.querySelectorAll('[data-preview]').forEach(button=>button.onclick=()=>openAlbum(button.dataset.preview));
  rows.append(row);
 },current,openPlaylist);
 heading.append(panel);
}
