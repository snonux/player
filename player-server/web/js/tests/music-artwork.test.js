import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { mountMusicArtwork } from '../music-artwork.js';

// A small DOM double follows replacement semantics for the actual editor mount
// and its production event handlers (same convention as music-enrichment tests).
class Element {
 constructor(tag='div'){this.tag=tag;this.children=[];this.isConnected=true;this.attributes={};this.dataset={};this.hidden=false;this._html='';}
 set innerHTML(value){
  this._html=value;this.children=[];
  for(const match of value.matchAll(/<(div|p|button|img)\b([^>]*)>/g)){
   const child=new Element(match[1]);
   for(const attr of match[2].matchAll(/([\w-]+)(?:="([^"]*)")?/g))child.attributes[attr[1]]=attr[2]??'';
   this.children.push(child);
  }
 }
 get innerHTML(){return this._html;}
 append(...items){this.children.push(...items);}
 prepend(item){this.children.unshift(item);}
 setAttribute(k,v){this.attributes[k]=v;}
 removeAttribute(k){delete this.attributes[k];}
 addEventListener(){}
 focus(){document.activeElement=this;}
 click(){this.onclick?.();}
 querySelector(selector){return this.querySelectorAll(selector)[0]??null;}
 querySelectorAll(selector){
  const match=node=>selector==='img'?node.tag==='img':selector.startsWith('[')&&Object.hasOwn(node.attributes,selector.slice(1,-1));
  return this.children.flatMap(child=>[...(match(child)?[child]:[]),...child.querySelectorAll(selector)]);
 }
}
let picker;
globalThis.document={createElement:tag=>{const node=new Element(tag);if(tag==='input')picker=node;return node;}};
globalThis.confirm=()=>true;
const requests=[];
let reply={has_artwork:true,can_edit:true,revision:'original'},statusCode=200;
globalThis.fetch=async(url,options)=>{requests.push([url,options]);return {ok:statusCode===200,status:statusCode,json:async()=>reply};};
const settle=()=>new Promise(resolve=>setImmediate(resolve));

// Guard the integration point too: the cover slot must have one owner. It is
// intentionally empty before the editor mounts; the editor supplies its fallback.
const view=await readFile(new URL('../views/music-playlists.js',import.meta.url),'utf8');
assert.match(view,/<div data-playlist-cover><\/div>/,'playlist detail gives the editor an empty cover slot');
assert.doesNotMatch(view,/<div data-playlist-cover>\$\{cover\(p\)\}/,'no second saved image outside the editor');
const host=new Element();
const fallback='<img class="album-cover" src="/api/media/8/thumbnail" alt="Default cover">';
const section=mountMusicArtwork(host,'playlist','pl_one',()=>true,fallback);
await settle();
const preview=section.querySelector('[data-art-preview]');
assert.equal(host.querySelectorAll('img').length,1);
assert.match(preview.innerHTML,/revision=original/);

section.querySelector('[data-art-change]').click();
picker.files=[{size:12}];reply={has_artwork:true,can_edit:true,revision:'replacement'};
picker.onchange();await settle();
assert.equal(requests.at(-1)[1].method,'PUT');
assert.equal(host.querySelectorAll('img').length,1,'replacement keeps exactly one authoritative image');
assert.match(preview.innerHTML,/revision=replacement/);
assert.doesNotMatch(preview.innerHTML,/revision=original/);

reply={has_artwork:false,can_edit:true,revision:''};
section.querySelector('[data-art-remove]').click();await settle();
assert.equal(requests.at(-1)[1].method,'DELETE');
assert.equal(host.querySelectorAll('img').length,1,'remove restores one ordinary cover');
assert.equal(preview.innerHTML,fallback);
assert.doesNotMatch(preview.innerHTML,/revision=/);

statusCode=403;reply={error:'Permission removed.'};
section.querySelector('[data-art-change]').click();picker.files=[{size:12}];picker.onchange();await settle();
assert.equal(host.querySelectorAll('img').length,0,'permission loss clears the only image, including old custom art');
assert.equal(preview.innerHTML,'');
console.log('Artwork actual mount/change/remove uses one preview; permission loss clears it.');
