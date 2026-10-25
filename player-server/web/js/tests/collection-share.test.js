import assert from 'node:assert/strict';
import { shareMusicCollection } from '../collection-share.js';
import { state } from '../state.js';

const configurationSelector = 'input:not([readonly]),select,[data-reload]';
function fixture() {
 const expiry={value:'',disabled:false,focus(){}},uses={value:'',disabled:false};
 const quality={value:'',disabled:false},reload={disabled:false},profileStatus={};
 const close={disabled:false},copy={disabled:false,hidden:true},submit={disabled:false};
 const result={value:'',disabled:false,focus(){},select(){}},resultLabel={hidden:true},status={};
 const configuration=[expiry,uses,quality,reload];
 const form={elements:{expiry,uses,audio_quality:quality},querySelectorAll:selector=>{
  assert.equal(selector,configurationSelector,'only configuration controls are locked');return configuration;
 }};
 const panel={isConnected:true,querySelector:selector=>({'select':quality,'[role=status]':profileStatus,'[data-reload]':reload})[selector],addEventListener(){}};
 const elements={'form':form,'[data-status]':status,'[type=submit]':submit,'[data-result] input':result,'[data-result]':resultLabel,'[data-copy]':copy,'[data-close]':close,'[data-share-quality]':panel};
 const overlay={querySelector:selector=>elements[selector],addEventListener(){},remove(){panel.isConnected=false;}};
 globalThis.document={activeElement:{isConnected:true,focus(){}},createElement:()=>overlay,body:{append(){}}};
 return {form,quality,configuration,close,copy,submit,result,status};
}
const deferred=()=>{let resolve;const promise=new Promise(done=>{resolve=done;});return {promise,resolve};};
const tick=()=>new Promise(resolve=>setImmediate(resolve));
globalThis.location={origin:'http://local.test'};
for (const single of [false,true]) {
 for (const succeeds of [false,true]) {
  const controls=fixture(),profiles=deferred(),creation=deferred();
  let submitted;
  state.user={};
  globalThis.fetch=(url,options)=>{
   if(url==='/api/stream-profiles')return profiles.promise;
   assert.equal(url,single?'/api/media/7/shares':'/api/shares/collections');
   submitted=JSON.parse(options.body);return creation.promise;
  };
  shareMusicCollection([{id:7,type:'audio'}],'Song',document.activeElement,single);
  // Original is available before the asynchronous profile list arrives.
  controls.quality.value='original';
  const pending=controls.form.onsubmit({preventDefault(){}});
  assert.equal(submitted.audio_quality,'original');
  assert.ok(controls.configuration.every(control=>control.disabled));
  assert.equal(controls.submit.disabled,true);
  assert.equal(controls.close.disabled,false);
  assert.equal(controls.copy.disabled,false);
  assert.equal(controls.result.disabled,false,'manual link field remains usable');
  // A late list response must preserve the frozen policy and disabled state.
  if(!succeeds) {
   profiles.resolve({ok:true,json:async()=>[{id:'aac128',label:'Balanced · AAC 128 kbps'}]});await tick();
   assert.equal(controls.quality.value,'original');
   assert.ok(controls.configuration.every(control=>control.disabled));
  }
  creation.resolve({ok:succeeds,json:async()=>({token:'ready'})});await pending;
  if(succeeds) {
   profiles.resolve({ok:true,json:async()=>[{id:'aac128',label:'Balanced · AAC 128 kbps'}]});await tick();
   assert.ok(controls.configuration.every(control=>control.disabled),'created link retains locked configuration');
   assert.equal(controls.submit.hidden,true);
   assert.equal(controls.copy.hidden,false);
   assert.equal(controls.copy.disabled,false);
   assert.equal(controls.result.value,'http://local.test/s/ready');
  } else {
   assert.ok(controls.configuration.every(control=>!control.disabled),'failure restores editing and profile reload');
   assert.equal(controls.submit.disabled,false);
   assert.match(controls.status.textContent,/Could not create link/);
  }
  assert.equal(controls.quality.value,'original');
  assert.equal(controls.close.disabled,false);
 }
}
console.log('Collection and single-song share configuration stays locked during creation/after success; errors restore editing and late profile loads preserve policy.');
