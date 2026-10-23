import assert from 'node:assert/strict';
// Exercise production openAlbum/renderMusic, with album detail objects rather
// than list-shaped substitutes. Optional sections use inert transport/DOM seams.
const node=()=>({isConnected:true,dataset:{},classList:{add(){},remove(){},toggle(){}},
 append(){},prepend(){},remove(){},focus(){},setAttribute(){},removeAttribute(){},addEventListener(){},
 querySelector(){return node();},querySelectorAll(){return [];},insertAdjacentHTML(){}});
globalThis.window={addEventListener(){},scrollTo(){}};
globalThis.document={addEventListener(){},dispatchEvent(){},createElement:node,
 getElementById:()=>node(),querySelector:()=>null,querySelectorAll:()=>[]};
globalThis.localStorage={getItem:()=>null};
globalThis.location={origin:'http://local'};
const { API }=await import('../api.js');
const { state }=await import('../state.js');
const { initMusicCatalog,openAlbum,renderMusic }=await import('../views/music-catalog.js');
const { readLibraryURL }=await import('../navigation-url.js');
state.user={id:1};
const detail={album_key:'al_a',title:'Actual detail object',artist:'Artist',song_count:0,duration:0,songs:[]};
let reads=0;
API.album=async key=>{assert.equal(key,'al_a');reads++;return detail;};
API.albums=async()=>{assert.fail('Detail must use API.album, not the collection list.');};
API.relatedMusic=async()=>[];
API.musicEnrichment=async()=>({enabled:false});
initMusicCatalog(()=>{});
async function assertDetail(label) {
 const grid=node();
 grid.querySelector=selector=>selector==='.media-row, .album-card'?null:node();
 await renderMusic(grid,()=>true);
 assert.match(grid.innerHTML,/Actual detail object/,label);
 assert.match(grid.innerHTML,/album-header/,label);
 assert.doesNotMatch(grid.innerHTML,/Could not load|data-retry/,label);
 assert.equal(state.media.length,0,'Detail owns its songs, independently of random list order.');
}
// This is the state produced by Random -> View all. openAlbum preserves its
// navigation context; it must never sort the API.album object as a list.
state.music={album:'',collection:'random',collectionKeys:['al_b','al_a']};
openAlbum('al_a');
assert.equal(state.music.collection,'random');
assert.deepEqual(state.music.collectionKeys,['al_b','al_a']);
await assertDetail('in-app Random -> View all -> album');
// Cold deep links have no random snapshot, exposing the separate data.map path.
state.music=readLibraryURL('?music=album&id=al_a&mcollection=random').music;
assert.equal(state.music.collectionKeys,undefined);
await assertDetail('cold random/detail URL');
assert.equal(state.music.collectionKeys,undefined,'Detail does not manufacture a random collection snapshot.');
assert.equal(reads,2);
console.log('Random collection in-app and cold-link album detail object regressions passed.');
