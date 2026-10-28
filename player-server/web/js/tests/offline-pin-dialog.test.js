import assert from 'node:assert/strict';
import { showOfflinePin } from '../offline-pin-dialog.js';
import { bindCollectionActions } from '../collection-actions.js';
import { state } from '../state.js';
import { API } from '../api.js';

globalThis.localStorage = {getItem:()=>null};
globalThis.fetch = async()=>({ok:true,json:async()=>[]});
const tick = ()=>new Promise(resolve=>setImmediate(resolve));
const button = (visible=true)=>({isConnected:true,disabled:false,getClientRects:()=>visible?[{}]:[],focus(){document.activeElement=this;}});
const album = {kind:'album',key:'album'};

function fixture() {
 const controls = Object.fromEntries(['h2','[data-status]','[data-pin]','[data-play]','[data-unpin]','[data-remove]','[data-close]','[data-quality-status]','[data-quality-reload]'].map(key=>[key,button()]));
 controls['[data-download-quality]']={disabled:false,replaceChildren(){},append(){}};
 const events = {};
 const overlay = {isConnected:true,querySelector:selector=>controls[selector],addEventListener:(type,handler)=>{events[type]=handler;},remove(){this.isConnected=false;if(Object.values(controls).includes(document.activeElement))document.activeElement=document.body;}};
 const selected = button(), opener = button();
 let otherModal = null;
 globalThis.document = {
  activeElement:opener,
  body:{append(){}},
  createElement:tag=>tag==='option'?{}:overlay,
  addEventListener(){},
  querySelector:()=>otherModal,
  querySelectorAll:()=>[{classList:{contains:()=>true},querySelector:()=>selected}],
 };
 state.user={};
 return {overlay,controls,events,selected,opener,otherModal(value){otherModal=value;}};
}

for(const method of ['Escape','Close','modalbeforeclose']) {
 const f=fixture();
 showOfflinePin(album,'Album',f.opener);
 await tick();
 assert.equal(document.activeElement,f.controls.h2,'dialog initially owns focus');
 let prevented=false,stopped=false;
 const event={key:'Escape',preventDefault(){prevented=true;},stopPropagation(){stopped=true;}};
 if(method==='Escape')f.overlay.onkeydown(event);
 else if(method==='Close')f.controls['[data-close]'].onclick();
 else f.events.modalbeforeclose(event);
 assert.equal(f.overlay.isConnected,false,method+' removes the dialog');
 assert.equal(document.activeElement,f.opener,method+' restores the visible opener');
 if(method==='Escape')assert.ok(prevented && stopped,'Escape is handled once by the dialog');
 // Closing twice must not steal focus after another control has been used.
 f.selected.focus(); f.controls['[data-close]'].onclick();
 assert.equal(document.activeElement,f.selected);
}

for(const reason of ['hidden','removed','disabled']) {
 const f=fixture();
 const opener=button(reason!=='hidden');
 if(reason==='removed')opener.isConnected=false;
 if(reason==='disabled')opener.disabled=true;
 showOfflinePin(album,'Album',opener);
 f.controls['[data-close]'].onclick();
 assert.equal(document.activeElement,f.selected,reason+' opener falls back to the selected card');
}
for(const reason of ['account changed','another dialog']) {
 const f=fixture(); showOfflinePin(album,'Album',f.opener);
 if(reason==='account changed')state.user={};
 else f.otherModal({});
 const otherControl=button(); otherControl.focus();
 f.controls['[data-close]'].onclick();
 assert.equal(document.activeElement,otherControl,reason+' keeps focus on the other current control');
}

// Closing while the album request is pending must leave the dialog removed and
// never restore focus again when the asynchronous action finally settles.
{
 const f=fixture(), oldAlbum=API.album;
 let complete;
 API.album=()=>new Promise(resolve=>{complete=resolve;});
 try {
  showOfflinePin(album,'Album',f.opener);
  const pending=f.controls['[data-pin]'].onclick();
  assert.equal(f.controls['[data-close]'].textContent,'Cancel and close');
  f.controls['[data-close]'].onclick();
  assert.equal(document.activeElement,f.opener);
  f.selected.focus(); complete({songs:[{id:7,type:'audio'}]}); await pending;
  assert.equal(f.overlay.isConnected,false,'late collection response cannot reopen the dialog');
  assert.equal(document.activeElement,f.selected,'late collection response cannot steal focus');
 } finally {API.album=oldAlbum;}
}

// Mount the production collection binder: hidden detail actions must pass the
// visible entity More as the dialog opener; catalog More remains its own target.
for(const nested of [false,true]) {
 const f=fixture(), more=button(!nested), entityMore=button();
 const controls = Object.fromEntries(['play','shuffle','share','download','pin','menu'].map(action=>[`[data-collection-${action}]`,action==='menu'?more:button()]));
 const root={isConnected:true,querySelector:selector=>controls[selector],closest:()=>({querySelector:()=>entityMore})};
 bindCollectionActions(root,()=>[album],()=>true,'Album');
 controls['[data-collection-pin]'].onclick();
 await tick();
 f.overlay.onkeydown({key:'Escape',preventDefault(){},stopPropagation(){}});
 assert.equal(document.activeElement,nested?entityMore:more,'pin returns focus to the visible originating More');
}
await tick();
console.log('Saved collection dialog close paths restore visible focus, respect other dialogs/accounts, and resolve nested entity More.');
