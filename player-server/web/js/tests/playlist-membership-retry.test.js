import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { runInNewContext } from 'node:vm';

// Execute the production picker and its handlers without importing playback or
// starting a browser. The DOM double implements native disabled checkbox clicks.
const control = (extra = {}) => ({disabled:false, checked:false, value:'', ...extra,
  focus(){}, click(){if(!this.disabled){this.checked=!this.checked;this.onchange?.();}}});
const search=control(), song=control({value:'7'}), target=control({dataset:{target:'pl_extra'}});
const policy=control({value:'skip'}), submit=control(), error={}, selected={};
const choices={set innerHTML(value){this.html=value;},querySelectorAll:()=>[song]};
const targets={};
const elements={'input':search,'[data-targets]':targets,'[data-choices]':choices,
 '[data-save]':submit,'[data-duplicates]':policy,'[data-error]':error,
 '[data-selected]':selected,'[data-cancel]':{}};
const body={set innerHTML(value){this.html=value;},querySelector:s=>elements[s],
 querySelectorAll:s=>s==='[data-target]'?[target]:s==='[data-target], [data-duplicates]'?[target,policy]:[]};
let rejectSave, pending, open=true;
const requests=[];
const api={albums:async()=>[{album_key:'a'}],album:async()=>({songs:[{id:7,file_name:'Song'}]}),
 playlistMembership:async()=>[{playlist_id:'pl_extra',name:'Extra',can_edit:true,entries:[]}],
 addToPlaylists:body=>{requests.push(body);return pending?Promise.resolve():new Promise((resolve,reject)=>{rejectSave=reject;});}};
const source=(await readFile(new URL('../views/music-playlists.js',import.meta.url),'utf8'))
 .replace(/^import .*;\n/gm,'').replace(/export /g,'');
const html=(strings,...values)=>String.raw({raw:strings},...values);
const context={API:api,html,t:s=>s,ui:s=>s,plural:()=>'',uiCount:()=>'',
 testDialog:()=>({modal:{querySelector:()=>body},alive:()=>open,close:()=>{open=false;}}),
 esc:String,mediaTitle:song=>song.file_name,matchesSong:()=>true,toast:()=>{},console};
runInNewContext(source+'\ndialog=testDialog; globalThis.picker=addSongs;',context);
await context.picker({playlist_id:'pl_main'},()=>true);
song.click();target.click();
assert.equal(target.checked,true);
const first=submit.onclick();
assert.equal(target.disabled,true,'targets freeze while the request is in flight');
assert.equal(policy.disabled,true,'duplicate policy freezes with the submitted target snapshot');
target.click();
assert.equal(target.checked,true,'native click cannot uncheck an in-flight target');
rejectSave(Error('offline'));await first;
assert.equal(target.disabled,false,'failure restores target editing');
assert.equal(target.checked,true,'failure preserves the submitted target choice');
assert.match(error.textContent,/offline/);
target.click();
assert.equal(target.checked,false);
pending=true;await submit.onclick();
assert.deepEqual(Array.from(requests[0].playlist_ids),['pl_main','pl_extra']);
assert.deepEqual(Array.from(requests[1].playlist_ids),['pl_main'],'retry writes exactly the visibly checked targets');
assert.equal(open,false);
console.log('Production playlist picker freezes targets and retries visible selection PASS');
