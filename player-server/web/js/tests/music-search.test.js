import assert from 'node:assert/strict';
import { musicQuery, captureSearchView, restoreSearchView, hasSearchView, renderMusicSearch } from '../music-search.js';
import { parseQuery, initSearch } from '../search.js';
import { libraryURL, readLibraryURL } from '../navigation-url.js';
import { state } from '../state.js';
import { API } from '../api.js';

for (const [raw,expected] of [
 ['東京 / A & B; C/D',{query:'東京 / A & B; C/D',artistQuery:'',albumQuery:''}],
 ['artist:"A & B; C/D" album:"Café 夜" song',{query:'song',artistQuery:'A & B; C/D',albumQuery:'Café 夜'}],
 ['ALBUM:Café',{query:'',artistQuery:'',albumQuery:'Café'}],
 ['genre:Jazz',{query:'',artistQuery:'',albumQuery:'',facets:{genre:'Jazz'}}],
 ['',null],
]) assert.deepEqual(musicQuery(raw,parseQuery(raw)),expected);
for(const raw of ['tag:a','set:"Yoga Flow" x','like:1','like:0','min:3','max:5','minsize:1','maxsize:3','type:video','sort:name']) assert.equal(musicQuery(raw,parseQuery(raw)),null,raw);
// Clear is mode-sensitive: generic Sets/file-only filters retain their scope;
// the Music lens/search still has its intentional empty prompt.
const scope=[1,3];state.selectedSetIds=scope;state.music=null;
assert.equal(musicQuery('',parseQuery(''),Boolean(state.music)),null);
assert.equal(musicQuery('  ',parseQuery('  '),Boolean(state.music)),null);
assert.equal(state.selectedSetIds,scope);
state.music={search:true};
assert.deepEqual(musicQuery('',parseQuery(''),Boolean(state.music)),{query:'',artistQuery:'',albumQuery:''});
state.music={album:'al_current'};
assert.deepEqual(musicQuery('',parseQuery(''),Boolean(state.music)),{query:'',artistQuery:'',albumQuery:''});
state.music=null;
const route={search:true,query:'ПАРТИЯ %_',artistQuery:'A & B; C/D',albumQuery:'Café 夜'};
const decoded=readLibraryURL(libraryURL({...state,music:route}).slice(1)).music;
for(const [key,value] of Object.entries(route))assert.equal(decoded[key],value);

const listeners={},events=[];
const input={value:'',addEventListener:(name,fn)=>listeners[name]=fn,blur(){this.blurred=true;},focus(){},closest(){return null;}};
const nodes={'search-overlay':{classList:{add(){},remove(){}}}};
globalThis.document={getElementById:id=>id==='search-input'?input:nodes[id],dispatchEvent:event=>events.push(event.type),querySelector:()=>null,querySelectorAll:()=>[]};
globalThis.CustomEvent=class extends Event {};
globalThis.scrollTo=()=>{};
globalThis.scrollY=16;
const calls=[];
initSearch({input,onChange:value=>calls.push(value)});
const key=(value,isComposing=false)=>listeners.keydown({key:value,isComposing,preventDefault(){},stopPropagation(){}});
input.value='last query';listeners.input({isComposing:false});key('Enter');
assert.deepEqual(calls,['last query']);assert(events.includes('search:navigate-results'));assert(input.blurred);
await new Promise(resolve=>setTimeout(resolve,330));assert.equal(calls.length,1,'Enter cancelled duplicate debounce');
input.value='cancelled';listeners.input({isComposing:false});key('Escape');
await new Promise(resolve=>setTimeout(resolve,330));assert.equal(calls.length,1,'Escape cancelled pending query');
assert(events.includes('search:restore'));
input.value='composition';key('Enter',true);assert.equal(calls.length,1);
listeners.input({isComposing:true});await new Promise(resolve=>setTimeout(resolve,330));assert.equal(calls.length,1);
// A previously scheduled normal input must never observe partial composition.
input.value='normal pending';listeners.input({isComposing:false});
input.value='未確定';listeners.compositionstart();listeners.input({isComposing:true});
key('Enter');key('Escape');
await new Promise(resolve=>setTimeout(resolve,340));
assert.deepEqual(calls,['last query'],'normal debounce was cancelled before partial IME input');
input.value='東京';listeners.compositionend();listeners.input({isComposing:false});
await new Promise(resolve=>setTimeout(resolve,340));
assert.deepEqual(calls,['last query','東京'],'composition commit searches exactly once');
// Some engines report composing input before compositionstart is observed.
input.value='pending again';listeners.input({isComposing:false});input.value='部分';listeners.input({isComposing:true});
await new Promise(resolve=>setTimeout(resolve,340));assert.deepEqual(calls,['last query','東京']);
state.music={album:'original'};state.filters.search='original filter';state.selectedSetIds=[3];
captureSearchView();assert(hasSearchView());state.music=route;state.filters={search:'changed'};
let loaded=0;await restoreSearchView(async()=>loaded++);
assert.equal(loaded,1);assert.deepEqual(state.music,{album:'original'});assert.equal(state.filters.search,'original filter');assert.deepEqual(state.selectedSetIds,[3]);assert(!hasSearchView());
console.log('Music search query, file syntax, URL, immediate Enter, Escape, IME and restoration checks passed.');

const oldSearch=API.musicSearch,oldMedia=API.media;
const classes=new Set(),button={};
const row={dataset:{id:'19',index:'0',searchKind:'file'},classList:{add:name=>classes.add(name),remove:name=>classes.delete(name),contains:name=>classes.has(name)},scrollIntoView(){},querySelector:selector=>selector==='[data-action="search-file-open"]'?button:null};
const grid={innerHTML:'',classList:{add(){}},querySelector:()=>null,querySelectorAll:selector=>['[data-search-kind]','.media-row'].includes(selector)?[row]:[]};
const resultCount={textContent:''},oldGet=document.getElementById,oldRows=document.querySelectorAll;
try {
 API.musicSearch=async()=>({songs:[],albums:[],artists:[]});
 API.media=async()=>[{id:19,type:'audio',file_name:'source_001.flac',rel_path:'Music/source_001.flac'}];
 document.getElementById=id=>id==='result-count'?resultCount:oldGet(id);
 document.querySelectorAll=()=>[row];
 await renderMusicSearch(grid,{query:'source_001'},()=>true,{back(){},reload(){}});
 assert.match(grid.innerHTML,/button[^>]+data-action="search-file-open"[^>]+aria-label="Open file source_001.flac"/,'Files touch action is available to the existing shared Media actions menu');
 let stopped=false;
 const count=()=>events.filter(event=>event==='search:play-selected').length;
 const before=count();
 button.onclick({pointerType:'touch',stopPropagation(){stopped=true;}});
 assert.equal(count(),before+1,'explicit touch open plays selected file');assert(stopped);
 row.onclick({pointerType:'touch',target:{closest:()=>button}});
 assert.equal(count(),before+1,'bubbling action cannot play twice');
 row.onclick({pointerType:'mouse',target:{closest:()=>null}});
 assert.equal(count(),before+2,'existing pointer row activation still works');
 let fileLookups=0;
 API.media=async()=>{fileLookups++;return [{id:19,type:'audio',file_name:'source_001.flac',rel_path:'Music/source_001.flac'}];};
 const ratedGrid={innerHTML:'',classList:{add(){}},querySelector:()=>null,querySelectorAll:()=>[]};
 await renderMusicSearch(ratedGrid,{query:'source_001',rated:true},()=>true,{back(){},reload(){}});
 assert.equal(fileLookups,0,'Rated only never loads unfiltered Files fallback');
 assert.deepEqual(state.searchFiles,[]);assert.deepEqual(state.media,[]);
 assert.doesNotMatch(ratedGrid.innerHTML,/data-search-kind="file"/,'cleared or unrated matching music cannot reappear as Files');
 assert.match(ratedGrid.innerHTML,/Nothing matches these filters/);

} finally {API.musicSearch=oldSearch;API.media=oldMedia;document.getElementById=oldGet;document.querySelectorAll=oldRows;}
console.log('Pending IME debounce and explicit Files touch activation checks passed.');
