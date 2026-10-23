import assert from 'node:assert/strict';
import { normalizeMusicLayout, readMusicLayout, sortMusic } from '../music-layout.js';
import { state } from '../state.js';

const data=[{album_key:'a',title:'Second',year:2020,artist:'Z'},{album_key:'b',title:'First',year:1990,artist:'A'},{album_key:'c',title:'Missing',artist:'A'}];
const ascending=normalizeMusicLayout({sort:'year'},'albums');
assert.deepEqual(sortMusic(data,ascending,'albums').map(item=>item.album_key),['b','a','c']);
assert.deepEqual(sortMusic(data,{...ascending,descending:true},'albums').map(item=>item.album_key),['a','b','c'],'unknown years stay last in either direction');
assert.deepEqual(data.map(item=>item.album_key),['a','b','c'],'browse sorting cannot mutate collection order');
const songs=[{id:2,file_name:'fallback',duration:9,music:{artist:'B'}},{id:1,file_name:'file',duration:2,music:{title:'Tagged',artist:'A'}}];
assert.deepEqual(sortMusic(songs,normalizeMusicLayout({sort:'artist'},'songs'),'songs').map(item=>item.id),[1,2]);
assert.deepEqual(normalizeMusicLayout({sort:'rating',columns:['artist','unsupported'],view:'bad'},'songs'),{sort:'title',descending:false,columns:['artist'],view:'grid'});
const storage=new Map();
globalThis.location={origin:'https://one.example'};
globalThis.localStorage={getItem:key=>storage.get(key) ?? null};
state.user={id:1};
storage.set('player.music-layout.v1:https://one.example:1:albums',JSON.stringify({view:'table',columns:[],sort:'year',descending:true}));
assert.equal(readMusicLayout('albums').view,'table');
assert.deepEqual(readMusicLayout('albums').columns,[],'hiding optional columns survives restart');
assert.equal(readMusicLayout('songs').sort,'title','song settings independent');
state.user={id:2};assert.equal(readMusicLayout('albums').view,'grid','account settings isolated');
state.user={id:1};globalThis.location.origin='https://two.example';assert.equal(readMusicLayout('albums').view,'grid','server settings isolated');
localStorage.getItem=()=>{throw Error('Storage blocked');};assert.equal(readMusicLayout('albums').sort,'title');
console.log('Music layout sorting, missing metadata, immutable sequences, normalization and server/account isolation passed.');

// Exercise production disclosure wiring before the real global library handler,
// just as a bubbling browser keydown travels from summary/details to document.
const { bindMusicLayout } = await import('../music-layout.js');
const { initKeyboard } = await import('../keyboard.js');
let globalKey, entered=0, played=0, focused=false;
globalThis.document={addEventListener:(name,listener)=>{if(name==='keydown')globalKey=listener;}};
initKeyboard({enter:()=>entered++,playPause:()=>played++});
const summary={tagName:'SUMMARY',closest:()=>null,matches:()=>false,focus:()=>{focused=true;}};
const checkbox={tagName:'INPUT',closest:()=>null,matches:()=>false};
const details={open:false,querySelector:()=>summary};
const sort={},direction={};
const toolbar={querySelector:selector=>selector==='details'?details:selector==='[data-layout-sort]'?sort:selector==='[data-layout-direction]'?direction:null,querySelectorAll:()=>[]};
bindMusicLayout({querySelector:()=>toolbar},'songs',normalizeMusicLayout(null,'songs'),()=>{});
function keydown(key,target=summary) {
 const event={key,target,currentTarget:details,stopped:false,defaultPrevented:false,stopPropagation(){this.stopped=true;},preventDefault(){this.defaultPrevented=true;}};
 details.onkeydown(event);
 if(!event.stopped)globalKey(event);
 return event;
}
for(const key of ['Enter',' ']) {
 const event=keydown(key);
 assert(event.stopped,'summary activation stays inside the disclosure');
 assert(!event.defaultPrevented,'browser native disclosure activation remains enabled');
}
assert.equal(entered,0,'Columns Enter cannot open the selected album/song');
assert.equal(played,0,'Columns Space cannot toggle playback');
assert(!keydown(' ',checkbox).stopped,'checkbox Space retains native input ownership');
assert(!keydown('p').stopped,'other existing playback shortcuts remain available');
assert.equal(played,1);
details.open=true;
assert(keydown('Escape').defaultPrevented);assert(!details.open);assert(focused,'Escape restores the Columns opener');
console.log('Music Columns production wiring preserves native summary/checkbox activation and isolates Enter/Space from library shortcuts.');
