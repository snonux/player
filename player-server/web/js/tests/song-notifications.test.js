import assert from 'node:assert/strict';
import { createSongNotifications, bindSongNotificationLifecycle } from '../song-notifications.js';
const data = new Map(), notices = [];
let requests = 0, hidden = true, owner = 1, view, resolveAuth;
class Notice {
  static permission = 'default';
  static async requestPermission() { requests++; return Notice.permission; }
  constructor(title, options) { Object.assign(this, { title, options }); notices.push(this); }
  close() { this.closed = true; }
}
const storage = { getItem: k => data.get(k), setItem: (k,v) => data.set(k,v), removeItem: k => data.delete(k) };
const controller = createSongNotifications({ Notification: Notice, storage, hidden: () => hidden,
  authorize: () => resolveAuth ? new Promise(resolve => { resolveAuth = resolve; }) : Promise.resolve(owner), render: v => { view = v; } });
const song = id => ({ id, type:'audio', file_name:`${id}.mp3`, music:{title:`Song ${id}`,artist:'Artist',album:'Album'} });
controller.setAccount(1); assert.equal(view.enabled,false); assert.equal(requests,0);
await controller.playing(song(1)); assert.equal(notices.length,0);
Notice.permission='denied'; await controller.toggle(); assert.equal(view.enabled,false); assert.equal(view.disabled,true);
Notice.permission='default'; await controller.toggle(); assert.match(view.message,/not granted/);
Notice.permission='granted'; await controller.toggle(); assert.equal(view.enabled,true);
await controller.playing(song(2)); assert.equal(notices.length,1); assert.equal(notices[0].title,'Song 2'); assert.equal(notices[0].options.body,'Artist · Album');
await controller.playing(song(2)); assert.equal(notices.length,1);
hidden=false; await controller.playing(song(3)); hidden=true; await controller.playing(song(3)); assert.equal(notices.length,1);
await controller.playing(song(4)); assert.equal(notices.length,2); assert.equal(notices[0].closed,true);
// Same source transferred to another window is consumed, not announced twice.
const popup = createSongNotifications({Notification:Notice,storage,hidden:()=>true,authorize:async()=>1});
popup.setAccount(1); await popup.playing(song(4)); assert.equal(notices.length,2);
await controller.toggle(); assert.equal(view.enabled,false); assert.equal(notices[1].closed,true);
await controller.toggle(); owner=2; await controller.playing(song(5)); assert.equal(notices.length,2,'old account text never exposed');
owner=1; resolveAuth=true; const pending=controller.playing(song(6)); controller.clear(); resolveAuth(1); await pending; assert.equal(notices.length,2);
controller.setAccount(2); assert.equal(view.enabled,false,'account preference isolated');
const unsupported=createSongNotifications({storage,hidden:()=>true,authorize:async()=>2,render:v=>{view=v;}}); unsupported.setAccount(2);assert.equal(view.disabled,true);
console.log('Song notifications opt-in, permission, background, dedupe, transfer and account races PASS');

// A track change invalidates announcement work, not an explicit permission gesture.
let grant;
class DelayedNotice extends Notice {
  static requestPermission() { return new Promise(resolve => { grant = resolve; }); }
}
const race = createSongNotifications({Notification:DelayedNotice,storage,hidden:()=>true,authorize:async()=>3,render:v=>{view=v;}});
race.setAccount(3);
const permission = race.toggle();
await race.playing(song(10)); race.suspend();
grant('granted'); await permission;
assert.equal(view.enabled,true,'Allow survives a track change while the prompt is pending');
let storageFails = false;
const failingStorage = { ...storage, removeItem: k => { if (storageFails) throw Error('storage unavailable'); storage.removeItem(k); } };
const disable = createSongNotifications({Notification:Notice,storage:failingStorage,hidden:()=>true,authorize:async()=>3,render:v=>{view=v;}});
disable.setAccount(3); assert.equal(view.enabled,true);
storageFails=true; await disable.toggle(); disable.refresh();
assert.equal(view.enabled,false,'Disable stays off in memory if its persistence fails');
assert.match(data.get('player-song-notifications:3'),/on/);
await disable.playing(song(11)); assert.equal(notices.length,2,'failed persistence does not allow announcements');
console.log('Permission gesture track-change race and fail-closed disable PASS');

// Exercise the production page lifecycle wiring, including retained BFCache state.
const windowHandlers = {}, documentHandlers = {};
let lifecycleOwner = 4, lifecycleView, pendingAccount;
const lifecycle = createSongNotifications({Notification:Notice,storage,hidden:()=>true,
  authorize:()=> pendingAccount ? new Promise(resolve=>{pendingAccount=resolve;}) : Promise.resolve(lifecycleOwner),
  render:v=>{lifecycleView=v;}});
const windowTarget = {addEventListener:(name,callback)=>{windowHandlers[name]=callback;}};
const documentTarget = {visibilityState:'hidden',addEventListener:(name,callback)=>{documentHandlers[name]=callback;}};
bindSongNotificationLifecycle(lifecycle,windowTarget,documentTarget);
lifecycle.setAccount(4); await lifecycle.toggle(); await lifecycle.playing(song(40));
const visibleNotice=notices.at(-1);
pendingAccount=true; const oldAnnouncement=lifecycle.playing(song(41));
windowHandlers.pagehide({persisted:true});
assert.equal(visibleNotice.closed,true,'BFCache entry closes the current notification');
pendingAccount(4); await oldAnnouncement; pendingAccount=null;
assert.equal(notices.at(-1),visibleNotice,'BFCache entry invalidates pending announcement');
windowHandlers.pageshow({persisted:true});
assert.equal(lifecycleView.enabled,true,'same-account BFCache restore keeps opt-in');
await lifecycle.playing(song(42)); assert.equal(notices.at(-1).title,'Song 42');
windowHandlers.pagehide({persisted:true}); lifecycleOwner=5;
windowHandlers.pageshow({persisted:true});
const countAfterRestore=notices.length;
await lifecycle.playing(song(43)); assert.equal(notices.length,countAfterRestore,'changed account cannot expose old document metadata');
lifecycle.clear(); windowHandlers.pageshow({persisted:true});
assert.equal(lifecycleView.enabled,false,'logout remains cleared after BFCache restore');
await lifecycle.playing(song(44)); assert.equal(notices.length,countAfterRestore);
console.log('Production BFCache lifecycle, pending cancellation and changed-account privacy PASS');
