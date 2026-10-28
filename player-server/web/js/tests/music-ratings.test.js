import assert from 'node:assert/strict';
import { API } from '../api.js';
import { state } from '../state.js';
import { ratingControl, bindRatings, ratingFilters, ratingParams } from '../music-ratings.js';
import { sortMusic, normalizeMusicLayout } from '../music-layout.js';

globalThis.document={getElementById:()=>null};
const markup=ratingControl('song','7',{rating:3});
assert.match(markup,/aria-label="song rating"/);
assert.match(markup,/value="0"[^>]*>Unrated · Clear rating/);
assert.match(markup,/value="3" selected>3 stars/);
assert.equal((markup.match(/<option /g)||[]).length,6);
assert.match(ratingControl('album','x"<',{}),/data-key="x&quot;&lt;"/);
assert.deepEqual(ratingParams({rated:true,ratingSort:'rating'}),{rated:true,sort:'rating'});
const label={},select={dataset:{kind:'song',key:'7',saved:'3'},value:'5',disabled:false,isConnected:true,focus(){this.focused=true;}};
const grid={querySelectorAll:query=>query==='[data-rating-label]'?[label]:[select]};
let calls=[];
API.musicRating=async(...args)=>calls.push(args);
bindRatings(grid,()=>true);
await select.onchange();
assert.deepEqual(calls,[['song','7',5]]);assert.equal(select.dataset.saved,'5');assert.equal(select.disabled,false);assert.equal(select.focused,true);
select.value='0';await select.onchange();assert.equal(select.dataset.saved,'0');
API.musicRating=async()=>{throw Error('offline');};select.value='4';await select.onchange();assert.equal(select.value,'0');assert.equal(select.disabled,false);
let stopped=false;label.onclick({stopPropagation(){stopped=true;}});assert.equal(stopped,true);
stopped=false;select.onkeydown({key:'Enter',stopPropagation(){stopped=true;}});assert.equal(stopped,true);

// Albums Filters extras + layout Sort: Rated-only only (no Rating-order overlap).
let reloads=0;let albumHtml='';const checkbox={};const extras={insertAdjacentHTML(_,html){albumHtml=html;},querySelector:sel=>sel==='[data-rated-only]'?checkbox:null};
state.music={favorites:true,facets:{genre:'Jazz'}};
ratingFilters({querySelector:selector=>selector==='[data-music-filter-extras]'?extras:selector==='[data-layout-sort]'?{}:null},{...state.music},()=>reloads++);
assert.match(albumHtml,/data-rated-only/);
assert.doesNotMatch(albumHtml,/data-rating-order/);
checkbox.onchange({target:{checked:true}});
assert.deepEqual(state.music,{favorites:true,facets:{genre:'Jazz'},rated:true,filtersOpen:true});assert.equal(reloads,1);

// Artists .grid-full fallback without Sort: keep Rating-order until qs3 adds a toolbar.
reloads=0;let artistHtml='';
const orderHandlers={};
const orderSelect={addEventListener(name,handler){orderHandlers[name]=handler;}};
const ratedSelect={};
const artistHost={insertAdjacentHTML(_,html){artistHtml=html;},querySelector:query=>query==='[data-rated-only]'?ratedSelect:query==='[data-rating-order]'?orderSelect:null};
state.music={favorites:true,facets:{genre:'Jazz'}};
ratingFilters({querySelector:selector=>selector==='[data-music-filter-extras]'?null:selector==='.grid-full'?artistHost:null},{...state.music},()=>reloads++);
assert.match(artistHtml,/data-rating-order/);
ratedSelect.onchange({target:{checked:true}});
orderHandlers.change({target:{value:'rating'}});
assert.deepEqual(state.music,{favorites:true,facets:{genre:'Jazz'},rated:true,filtersOpen:true,ratingSort:'rating'});assert.equal(reloads,2);

assert.deepEqual(sortMusic([{id:1,rating:1},{id:2,rating:5}],normalizeMusicLayout({sort:'rating',descending:true},'songs'),'songs').map(s=>s.id),[2,1]);
console.log('Ratings controls: set/clear/error rollback, focus, native keyboard, Rated-only with Sort overlap removed and artist Rating-order kept.');
