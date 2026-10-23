import { state } from './state.js';
import { API } from './api.js';
import { escapeHtml as esc, toast } from './utils.js';

let pendingRatingFocus = null;

export function ratingControl(kind,key,item) {
 const value=Number(item.rating || 0);
 return `<label class="text-sm" data-rating-label>★ <select data-music-rating data-kind="${kind}" data-key="${esc(String(key))}" data-saved="${value}" aria-label="${esc(kind)} rating">${Array.from({length:6},(_,n)=>`<option value="${n}" ${n===value?'selected':''}>${n?`${n} ${n===1?'star':'stars'}`:'Unrated · Clear rating'}</option>`).join('')}</select></label>`;
}
export function bindRatings(grid,current) {
 grid.querySelectorAll('[data-rating-label]').forEach(label=>label.onclick=e=>e.stopPropagation());
 grid.querySelectorAll('[data-music-rating]').forEach(select=>{
  select.onkeydown=e=>{if(e.key!=='Escape')e.stopPropagation();};
  select.onchange=async()=>{
   const before=select.dataset.saved,value=Number(select.value);select.disabled=true;
   try {await API.musicRating(select.dataset.kind,select.dataset.key,value);if(!current()||!select.isConnected)return;select.dataset.saved=String(value);pendingRatingFocus={kind:select.dataset.kind,key:select.dataset.key};document.dispatchEvent?.(new Event('music:rating-refresh'));toast(value?`Rated ${value} of 5 stars.`:'Rating cleared.','success');}
   catch(error){if(current()){select.value=before;toast(error.message || 'Could not save rating. Check your connection and retry.','error');}}
   finally{if(current()&&select.isConnected){select.disabled=false;select.focus();}}
  };
 });
 if(pendingRatingFocus){const match=[...grid.querySelectorAll('[data-music-rating]')].find(select=>select.dataset.kind===pendingRatingFocus.kind && select.dataset.key===pendingRatingFocus.key);pendingRatingFocus=null;if(match)requestAnimationFrame(()=>{if(current()&&match.isConnected)match.focus();});}
}
export function ratingFilters(grid,view,reload) {
 const heading=grid.querySelector('.grid-full');if(!heading)return;
 heading.insertAdjacentHTML('beforeend',`<label><input type="checkbox" data-rated-only ${view.rated?'checked':''}> Rated only</label> <label>Rating order <select data-rating-order aria-label="Rating order"><option value="" ${!view.ratingSort?'selected':''}>Current order</option><option value="rating" ${view.ratingSort?'selected':''}>Highest rated</option></select></label>`);
 heading.querySelector('[data-rated-only]').onchange=e=>{state.music.rated=e.target.checked;reload();};
 heading.querySelector('[data-rating-order]').onchange=e=>{state.music.ratingSort=e.target.value;reload();};
}
export function ratingParams(view){return {rated:view.rated || false,...(view.ratingSort?{sort:'rating'}:{})};}
