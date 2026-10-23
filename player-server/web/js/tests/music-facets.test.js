import assert from 'node:assert/strict';
import { facetParams, hasFacets, mountMusicFacets, editMusicFacets } from '../music-facets.js';
import { nextMusicSearch, musicQuery } from '../music-search.js';
import { parseQuery } from '../search.js';
import { libraryURL, readLibraryURL } from '../navigation-url.js';
const facets={genre:'Électronique',year:'2020-2029',role:'composer',label:'Label A; B',compilation:'false'};
const state={music:{album:'',facets},selectedSetIds:[],filters:{},mediaPage:0};
assert.deepEqual(readLibraryURL(libraryURL(state).slice(1)).music.facets,facets);
assert.equal(hasFacets({facets}),true);assert.deepEqual(facetParams({genre:'',year:'2024'}),{year:'2024'});
assert.deepEqual(musicQuery('genre:"Électronique" year:2020-2029',parseQuery('genre:"Électronique" year:2020-2029')),{query:'',artistQuery:'',albumQuery:'',facets:{genre:'Électronique',year:'2020-2029'}});
assert.equal(musicQuery('genre:Jazz tag:keep',parseQuery('genre:Jazz tag:keep')),null);
console.log('M08 facet URL, modifiers, literal Unicode and generic syntax checks passed.');

// Exercise actual chip/Clear callbacks: clearing a role must also reset the
// existing Artists role that renderArtists uses when no facet role remains.
const {state:liveState}=await import('../state.js');
const {API}=await import('../api.js');
const originalOptions=API.musicFacets;
API.musicFacets=async()=>({genre:['Ambient'],role:['composer'],year:[]});
const handlers={};
const detailHandlers={};
const summary={tagName:'SUMMARY',closest:()=>null,matches:()=>false,focus(){this.focused=true;}};
const details={open:false,querySelector:()=>summary,addEventListener(name,handler){detailHandlers[name]=handler;}};
const remove={dataset:{removeFacet:'role'}};
const clear={addEventListener:(event,handler)=>{handlers.clear=handler;}};
const panel={querySelector:selector=>selector==='details'?details:selector==='[data-clear-facets]'?clear:null,querySelectorAll:selector=>selector==='[data-remove-facet]'?[remove]:[]};
globalThis.document={createElement:()=>panel};
const initial={artists:true,artist:'',album:'',role:'composer',facets:{role:'composer',genre:'Ambient'}};
liveState.music={...initial};let reloads=0;
await mountMusicFacets({prepend(){}},initial,()=>true,()=>reloads++);
remove.onclick();
assert.equal(liveState.music.role,'artists');
assert.equal(liveState.music.facets.genre,'Ambient','removing role preserves other dimensions');
const requestRole=()=>liveState.music.facets?.role || liveState.music.role || 'artists';
assert.equal(requestRole(),'artists','actual Artists API role fallback resets');
let url=new URL(libraryURL({...state,music:liveState.music}),'http://fixture.test');
assert.equal(url.searchParams.get('mrole'),'artists');
assert.equal(url.searchParams.get('mgenre'),'Ambient');
liveState.music={...initial};handlers.clear();
assert.deepEqual(liveState.music.facets,{});assert.equal(requestRole(),'artists');
url=new URL(libraryURL({...state,music:liveState.music}),'http://fixture.test');
assert.equal(url.searchParams.get('mrole'),'artists');assert.equal(url.searchParams.has('mgenre'),false);
assert.equal(reloads,2);API.musicFacets=originalOptions;
console.log('Artists role chip removal/Clear reset API fallback and URL while preserving only intended facets.');

const parsedSearch=raw=>musicQuery(raw,parseQuery(raw),true);
let searchView=nextMusicSearch(null,parsedSearch('genre:Ambient year:2021'));
assert.deepEqual(searchView.facets,{genre:'Ambient',year:'2021'});
searchView=nextMusicSearch(searchView,parsedSearch(''));
assert.deepEqual(searchView.facets,{},'deleted modifiers stop filtering');
searchView=nextMusicSearch({facets:{genre:'Jazz',label:'Label A; B'}},parsedSearch('genre:Ambient year:2021'));
assert.equal(searchView.facets.genre,'Ambient');
searchView=nextMusicSearch(searchView,parsedSearch('Harbour'));
assert.deepEqual(searchView.facets,{genre:'Jazz',label:'Label A; B'},'explicit choices survive removed/shadowing modifiers');
const rawInput={value:'genre:Ambient year:2021 artist:"A B" Harbour'};
document.getElementById=()=>rawInput;
searchView=nextMusicSearch({facets:{label:'Label A; B'}},parsedSearch(rawInput.value));
searchView=editMusicFacets(searchView,'genre','');
assert.equal(searchView.facets.genre,undefined);
assert.equal(rawInput.value,'year:2021 artist:"A B" Harbour');
searchView=nextMusicSearch(searchView,parsedSearch(rawInput.value));
assert.deepEqual(searchView.facets,{label:'Label A; B',year:'2021'});
searchView=editMusicFacets(searchView);
assert.equal(rawInput.value,'artist:"A B" Harbour');
assert.deepEqual(nextMusicSearch(searchView,parsedSearch(rawInput.value)).facets,{},'Clear does not resurrect typed modifiers');
// Loading controls render applied chips/Clear without awaiting option requests.
API.musicFacets=()=>{throw new Error('loading must not request options');};
liveState.music={facets:{genre:'Ambient'}};
await mountMusicFacets({prepend(){}},liveState.music,()=>true,()=>{},true);
assert.match(panel.innerHTML,/Remove Genre: Ambient/);assert.match(panel.innerHTML,/data-clear-facets/);
API.musicFacets=originalOptions;
console.log('Typed modifier removal, explicit facet preservation, coherent chip/Clear and loading controls passed.');

const {initKeyboard}=await import('../keyboard.js');
let globalKey,opened=0,played=0;
document.addEventListener=(name,handler)=>{if(name==='keydown')globalKey=handler;};
initKeyboard({enter:()=>opened++,playPause:()=>played++});
function disclosureKey(key,target=summary){
 const event={key,target,currentTarget:details,stopped:false,defaultPrevented:false,stopPropagation(){this.stopped=true;},preventDefault(){this.defaultPrevented=true;}};
 detailHandlers.keydown(event);if(!event.stopped)globalKey(event);return event;
}
for(const key of ['Enter',' ']){const event=disclosureKey(key);assert(event.stopped);assert(!event.defaultPrevented);}
assert.equal(opened,0);assert.equal(played,0);
assert(!disclosureKey(' ',{tagName:'SELECT',closest:()=>null,matches:()=>false}).stopped);
assert(!disclosureKey('p').stopped);assert.equal(played,1);
details.open=true;assert(disclosureKey('Escape').defaultPrevented);assert(!details.open);assert(summary.focused);
console.log('More filters summary preserves native activation without opening selection or toggling playback.');
