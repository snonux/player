import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// Run the actual startup and logout bodies with transport/UI seams. This catches
// missing production wiring even when helpers pass independently in unit tests.
const source=readFileSync(new URL('../app.js',import.meta.url),'utf8');
assert.match(source,/import \{ state, setAuthenticatedUser \} from '\.\/state\.js'/);
const startupStart=source.indexOf('  try {\n    const [cfg, sets] = await Promise.all(');
const startupEnd=source.indexOf('\n}\n\nfunction applySearchSet',startupStart);
const logoutStart=source.indexOf("document.getElementById('logout-btn')?.addEventListener('click', async () => {");
const logoutBodyStart=source.indexOf('\n',logoutStart)+1;
const logoutEnd=source.indexOf('\n  });',logoutBodyStart);
assert.ok(startupStart>0 && startupEnd>startupStart && logoutEnd>logoutBodyStart);
const run=(body,env)=>new Function('env',`with(env){return(async()=>{${body}})();}`)(env);
const {state,setAuthenticatedUser}=await import('../state.js');
const noop=()=>{};
const presenceEvents=[], presenceHost={};
const trackedIdentity=id=>{presenceEvents.push(`identity:${id}`);setAuthenticatedUser(id);};
const mountMusicPresence=host=>{
 assert.equal(host,presenceHost);assert.ok(state.user?.id>0,'mount follows authenticated identity');
 const account=state.user.id;presenceEvents.push(`mount:${account}`);
 return ()=>presenceEvents.push(`cleanup:${account}:visible:${state.user?.id}`);
};
const env={state,setAuthenticatedUser:trackedIdentity,closeMusicPresence:null,mountMusicPresence,document:{querySelector:selector=>{assert.equal(selector,'.main-content');return presenceHost;}},loggingOut:false,API:{config:async()=>({user_id:17}),sets:async()=>[],users:async()=>[],logout:async()=>{}},setPlaybackCacheAccount:noop,setListeningProgressAccount:noop,initSongNotifications:noop,setMediaPageSize:noop,showAdmin:noop,renderSets:()=>assert.equal(state.user?.id,17,'identity must exist before library rendering'),initNavigation:()=>({restore:async()=>{}}),isShuffle:noop,setShuffle:noop,syncChromeVirtualSets:noop,restoreQueue:async()=>{},queueState:()=>({}),toast:noop,navigatedDuringStartup:false,initialCollection:'library',openCollection:noop,stopRenderer:async()=>{},clearSongNotifications:noop,releasePlaybackCache:noop,location:{origin:'http://fixture',search:'',hash:''}};
env.loadMedia=async()=>{};
env.setPodcastAccount=noop;
env.initGlobalSyncControls=noop;
env.toast=(message,kind)=>assert.notEqual(kind,'error',message);
const startup=source.slice(startupStart,startupEnd),logout=source.slice(logoutBodyStart,logoutEnd);
await run(startup,env);
assert.equal(state.user.id,17,'authenticated config initializes production identity');
assert.deepEqual(presenceEvents,['identity:17','mount:17'],'production startup mounts only after identity is established');

// A slow optional queue restore must not leave the root library blank.
let finishQueue;
let libraryShown=false;
env.loadMedia=async()=>{libraryShown=true;};
env.restoreQueue=()=>new Promise(resolve=>{finishQueue=resolve;});
const slowQueueStartup=run(startup,env);
await new Promise(resolve=>setImmediate(resolve));
assert.equal(libraryShown,true,'root library is visible before saved queue hydration finishes');
finishQueue();await slowQueueStartup;
// Deep links keep queue-position recovery ahead of route restoration.
libraryShown=false;env.location.search='?media=12';
env.restoreQueue=async()=>{};
await run(startup,env);
assert.equal(libraryShown,false,'do not render the root library over a deep link');
env.location.search='';

// Logout invalidates in-flight home actors before awaiting slow renderer/network I/O.
let finishRenderer;
env.stopRenderer=()=>new Promise(resolve=>{finishRenderer=resolve;});
const pendingLogout=run(logout,env);
assert.equal(state.user,null,'logout clears identity synchronously');
assert.equal(env.loggingOut,true);
assert.deepEqual(presenceEvents.slice(-2),['cleanup:17:visible:17','identity:null'],'logout cleans up the live panel before clearing identity or awaiting output/network');
assert.equal(env.closeMusicPresence,null);
finishRenderer();await pendingLogout;
assert.equal(env.location.href,'/login.html');

// A delayed config response from startup cannot resurrect a logged-out account.
let finishConfig;
env.loggingOut=false;env.API.config=()=>new Promise(resolve=>{finishConfig=resolve;});
const mountsBefore=presenceEvents.filter(event=>event.startsWith('mount:')).length;
const pendingStartup=run(startup,env);
env.stopRenderer=async()=>{};
await run(logout,env);
finishConfig({user_id:17});await pendingStartup;
assert.equal(state.user,null);
assert.equal(presenceEvents.filter(event=>event.startsWith('mount:')).length,mountsBefore,'delayed config cannot remount a logged-out listener panel');

// Exercise production home storage using identity populated by that startup body.
globalThis.window={addEventListener(){}};
globalThis.location=env.location;
const reads=[],writes=[];
globalThis.localStorage={getItem:key=>{reads.push(key);return null;},setItem:(key,value)=>writes.push([key,value])};
class Node {
 constructor(){this.defaults={focus(){}};this.attributes=[];this.shows=[];}
 set innerHTML(value){this.html=value;this.shows=[...value.matchAll(/<input type="checkbox" data-home-show="([^"]+)" ([^>]*)>/g)].map(match=>({dataset:{homeShow:match[1]},checked:match[2].includes('checked'),focus(){document.activeElement=this;}}));}
 get innerHTML(){return this.html || '';}
 append(){} replaceChildren(){} contains(){return false;} focus(){}
 querySelectorAll(selector){return selector==='[data-home-show]'?this.shows:[];}
 querySelector(selector){if(selector==='[data-home-editor]')return this;if(selector==='[data-home-defaults]')return this.defaults;if(selector==='summary')return this.defaults;return this.shows.find(input=>selector===`[data-home-show="${input.dataset.homeShow}"]`) || null;}
}
globalThis.document={activeElement:null,createElement:()=>new Node(),getElementById:()=>null};
const {API}=await import('../api.js');API.playlists=async()=>[];
const {mountMusicHome}=await import('../music-home.js');
function mount(labels={}){const panel=new Node();let editor;panel.append=(node)=>{editor=node;};mountMusicHome(panel,labels,()=>{},()=>true,()=>{});return editor;}
for(const invalid of [undefined,null,0,-1,'17',NaN,Infinity]){
 setAuthenticatedUser(invalid);assert.equal(state.user,null);
 const editor=mount();editor.defaults.onclick();
}
assert.deepEqual(reads,[],'unknown identity never reads shared undefined/null account preferences');
assert.deepEqual(writes,[],'unknown identity remains in memory');
env.loggingOut=false;env.API.config=async()=>({user_id:17});await run(startup,env);
mount().defaults.onclick();
assert.equal(reads.at(-1),'player.music-home.v1:http://fixture:17');
assert.equal(writes.at(-1)[0],reads.at(-1));
const oldEditor=mount();
env.API.config=async()=>({user_id:29});env.renderSets=()=>{};await run(startup,env);
const count=writes.length;oldEditor.defaults.onclick();
assert.equal(writes.length,count,'old home actors cannot write after account changes');
mount().defaults.onclick();assert.equal(writes.at(-1)[0],'player.music-home.v1:http://fixture:29');
// Storage rejection redraws the persisted state rather than leaving a false checked value.
const failedEditor=mount({recently_added:'Recently added'});
const checkbox=failedEditor.querySelector('[data-home-show="recently_added"]');
assert.equal(checkbox.checked,true);
localStorage.setItem=()=>{throw Error('Storage blocked');};
checkbox.checked=false;checkbox.onchange();
const restored=failedEditor.querySelector('[data-home-show="recently_added"]');
assert.equal(restored.checked,true,'failed hide restores the previous visible setting');
assert.equal(document.activeElement,restored,'failed save restores intended checkbox focus');
env.API.config=async()=>({});await run(startup,env);
assert.equal(env.closeMusicPresence,null,'unknown identity never mounts presence');
console.log('Production config/logout/presence lifecycle, delayed startup invalidation and home account/unknown identity persistence passed.');
