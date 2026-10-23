import assert from 'node:assert/strict';
import { showShareCollection } from '../share-collection-player.js';

class Element {
 constructor(tag){this.tagName=tag.toUpperCase();this.children=[];this.attrs={};this.handlers={};this.classList={add:()=>{}};this.paused=true;}
 append(...children){this.children.push(...children);}
 after(child){this.sibling=child;}
 setAttribute(key,value){this.attrs[key]=value;}
 removeAttribute(key){delete this[key];}
 addEventListener(name,handler){this.handlers[name]=handler;}
 load(){}
 pause(){this.paused=true;}
 async play(){this.paused=false;this.plays=(this.plays||0)+1;}
}
const player=new Element('section');
const documentHandlers={};
globalThis.document={getElementById:()=>player,createElement:tag=>new Element(tag),addEventListener(name,handler){documentHandlers[name]=handler;}};
globalThis.window={addEventListener(){}};
const item=(name,url,unavailable=false)=>({media:{id:1,type:'audio',file_name:name},playback_url:url,unavailable});
showShareCollection({items:[item('A','/s/t/items/0/stream'),item('Unavailable song','',true),item('A again','/s/t/items/2/stream')]});
const [audio,controls,status,list]=player.sibling.children;
assert.equal(audio.src,'/s/t/items/0/stream');assert.equal(audio.paused,true,'opening never autoplays');
assert.equal(list.children[1].children[0].disabled,true,'unavailable occurrence cannot play');
assert.equal(controls.children[0].disabled,true);
list.children[0].children[0].onclick();await Promise.resolve();
assert.equal(audio.plays,1);
audio.handlers.ended();await Promise.resolve();
assert.equal(audio.src,'/s/t/items/2/stream','natural end skips unavailable source and retains repeated occurrence');
assert.equal(audio.plays,2);assert.equal(controls.children[1].disabled,true);
audio.handlers.ended();assert.equal(status.textContent,'End of collection.');
controls.children[0].onclick();await Promise.resolve();assert.equal(audio.src,'/s/t/items/0/stream');
audio.handlers.error();assert.match(status.textContent,/expired or been revoked/);
// Exercise the registered production handler, including focus that remains
// on the actual track and transport controls after pointer/Enter activation.
function key(target, overrides={}) {
 const event={target,key:'p',code:'KeyP',prevented:false,preventDefault(){this.prevented=true;},...overrides};
 documentHandlers.keydown(event);return event;
}
audio.pause();
assert.equal(key(list.children[0].children[0]).prevented,true,'p works while a track button holds focus');
assert.equal(audio.paused,false);
assert.equal(key(controls.children[1]).prevented,true,'p works while Next holds focus');
assert.equal(audio.paused,true);
assert.equal(key(new Element('a')).prevented,true,'p works while a download link holds focus');
assert.equal(audio.paused,false);
for(const overrides of [{ctrlKey:true},{metaKey:true},{altKey:true},{shiftKey:true},{isComposing:true},{keyCode:229},{repeat:true},{defaultPrevented:true}]) {
 assert.equal(key(new Element('body'),overrides).prevented,false,`native chord/composition/repeat preserved: ${JSON.stringify(overrides)}`);
 assert.equal(audio.paused,false,'ignored key does not toggle playback');
}
for(const tag of ['input','textarea','select','audio']) {
 assert.equal(key(new Element(tag)).prevented,false,`${tag} keeps native keys`);
 assert.equal(key(new Element(tag),{key:' ',code:'Space'}).prevented,false,`${tag} keeps native Space`);
}
assert.equal(key({tagName:'SPAN',isContentEditable:true}).prevented,false,'editable content keeps typing');
const dialogButton={tagName:'BUTTON',closest:()=>({})};
assert.equal(key(dialogButton,{key:' ',code:'Space'}).prevented,false,'dialog button Space activates natively');
assert.equal(key(dialogButton).prevented,false,'dialog owns letter keys');
assert.equal(key(list.children[0].children[0],{key:' ',code:'Space'}).prevented,true,'outside-dialog button Space remains play/pause');
assert.equal(audio.paused,true);
assert.equal(key(new Element('a'),{key:' ',code:'Space'}).prevented,true,'outside-dialog link Space remains play/pause');
assert.equal(audio.paused,false);
assert.equal(key(controls.children[0],{key:'Enter',code:'Enter'}).prevented,false,'Enter keeps native button activation');
showShareCollection({items:[item('Unavailable song','',true)]});
assert.match(player.sibling.children[2].textContent,/No songs are currently available/);
console.log('Guest collection succession, occurrences, retry state and focused keyboard handler passed.');
