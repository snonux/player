import assert from 'node:assert/strict';
import { API } from '../api.js';
import { mountMusicEnrichment } from '../music-enrichment.js';
class Element {
 constructor(){this.isConnected=true;this.children=[];this.attributes={};this.hidden=false;this.dataset={};this.innerHTML='';}
 append(...items){this.children.push(...items);}
 setAttribute(k,v){this.attributes[k]=v;}
 removeAttribute(k){delete this.attributes[k];}
 remove(){this.isConnected=false;}
 querySelector(){return null;}
}
globalThis.document={createElement:()=>new Element()};
let result={enabled:true,can_refresh:true,state:'ready',source:'Fixture',description:'<b>Literal biography</b>',links:[]},calls=[];
API.musicEnrichment=async(kind,key,refresh)=>{calls.push([kind,key,refresh]);return result;};
const host=new Element();
const section=mountMusicEnrichment(host,'artist','key',()=>true);
await new Promise(resolve=>setImmediate(resolve));
const [toggle,body]=section.children, [content,refresh]=body.children;
assert.equal(body.hidden,true,'secondary metadata starts collapsed');
assert.match(content.innerHTML,/&lt;b&gt;Literal biography&lt;\/b&gt;/,'provider text never becomes HTML');
toggle.onclick();assert.equal(body.hidden,false);assert.equal(toggle.attributes['aria-expanded'],'true');
result={enabled:true,can_refresh:true,state:'error',description:'Last good biography',source:'Fixture',message:'Refresh failed',stale:true};
refresh.onclick();await new Promise(resolve=>setImmediate(resolve));
assert.equal(calls.at(-1)[2],true);
assert.equal(body.children[1],refresh,'refresh preserves its button for focus');
assert.match(content.innerHTML,/Last good biography/);assert.match(content.innerHTML,/Refresh failed/);
assert.match(content.innerHTML,/May be out of date/);
result={enabled:false};
refresh.onclick();await new Promise(resolve=>setImmediate(resolve));assert.equal(section.isConnected,false);
console.log('Music About disclosure, literal biography, explicit refresh, retained data and disabled state pass.');
