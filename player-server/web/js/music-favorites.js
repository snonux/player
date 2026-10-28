import { API } from './api.js';
import { escapeHtml as esc, toast } from './utils.js';

function entityFavoriteLabel(favorite) {
 return `${favorite ? '♥' : '♡'} ${favorite ? 'Favorited' : 'Favorite'}`;
}
export function favoriteButton(kind,key,item) {
 const favorite = Boolean(item.is_favorite);
 return `<button class="btn btn-ghost" data-entity-favorite data-kind="${kind}" data-key="${esc(key)}" aria-pressed="${favorite}" aria-label="${favorite?'Unfavorite':'Favorite'} ${kind}">${entityFavoriteLabel(favorite)}</button>`;
}
export function bindFavorites(grid,current) {
 grid.querySelectorAll('[data-entity-favorite]').forEach(button=>button.onclick=async event=>{
  event.stopPropagation();if(button.disabled)return;
  const next=button.getAttribute('aria-pressed')!=='true';button.disabled=true;
  try {
   await API.musicFavorite(button.dataset.kind,button.dataset.key,next);
   if(!current()||!button.isConnected)return;
   button.setAttribute('aria-pressed',String(next));
   button.setAttribute('aria-label',`${next?'Unfavorite':'Favorite'} ${button.dataset.kind}`);
   button.textContent=entityFavoriteLabel(next);
   toast(next?'Added to favorites.':'Removed from favorites.','success');
   if(!next && grid.querySelector('[data-favorites-only]')?.checked)document.dispatchEvent(new Event('music:favorite-refresh'));
  }catch(error){if(current())toast(error.message||'Could not save favorite. Check your connection and retry.','error');}
  finally{if(current() && button.isConnected){button.disabled=false;button.focus();}}
 });
}
export function favoriteFilter(grid,view,reload) {
 const heading=grid.querySelector('.grid-full');if(!heading)return;
 heading.insertAdjacentHTML('beforeend',`<label><input type="checkbox" data-favorites-only ${view.favorites?'checked':''}> Favorites only</label>`);
 heading.querySelector('[data-favorites-only]').onchange=event=>{view.favorites=event.target.checked;document.dispatchEvent(new CustomEvent('music:favorite-filter',{detail:view.favorites}));reload();};
}
