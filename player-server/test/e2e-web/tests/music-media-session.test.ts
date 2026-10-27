import { test, expect, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable --session-artwork fixture and real MPRIS bus');
let f:MusicFixture,e:Evidence;
const command=(...args:string[])=>execFileSync(args[0],args.slice(1),{encoding:'utf8',timeout:10000}).trim();
function descendant(pid:number) {
 const visited=new Set<number>();
 while(pid>1&&!visited.has(pid)) {
  if(pid===process.pid)return true;visited.add(pid);
  const match=readFileSync(`/proc/${pid}/status`,'utf8').match(/^PPid:\s*(\d+)/m);if(!match)return false;pid=Number(match[1]);
 }
 return false;
}
function ownedPlayer() {
 const players=command('busctl','--user','list','--no-legend','--no-pager').split('\n').filter(l=>l.startsWith('org.mpris.MediaPlayer2.'));
 const owned=players.flatMap(line=>{
  const [bus,value]=line.split(/\s+/);const pid=Number(value);
  if(!pid||!existsSync(`/proc/${pid}/cmdline`)||!descendant(pid))return [];
  const cmd=readFileSync(`/proc/${pid}/cmdline`,'utf8');
  const profile=cmd.match(/(?:--user-data-dir=|--profile\0)([^\0]+)/)?.[1];
  if(!profile||!profile.includes('playwright'))return [];
  if(!cmd.startsWith('/opt/google/chrome/chrome\0')&&!cmd.startsWith('/usr/lib64/firefox/firefox\0'))return [];
  return [{bus,pid,profile,cmd,start:readFileSync(`/proc/${pid}/stat`,'utf8')}];
 });
 expect(owned.length,'exactly one launched browser root/profile owns this serial test session').toBe(1);
 return owned[0];
}
function property(owner:ReturnType<typeof ownedPlayer>,name:string) {
 // Revalidate identity before EVERY OS request; never target user sessions.
 expect(readFileSync(`/proc/${owner.pid}/cmdline`,'utf8')).toBe(owner.cmd);
 expect(readFileSync(`/proc/${owner.pid}/stat`,'utf8').split(' ').slice(21,22)).toEqual(owner.start.split(' ').slice(21,22));
 expect(descendant(owner.pid)).toBe(true);
 return command('busctl','--user','get-property',owner.bus,'/org/mpris/MediaPlayer2','org.mpris.MediaPlayer2.Player',name);
}
function action(owner:ReturnType<typeof ownedPlayer>,name:string) {
 property(owner,'PlaybackStatus');
 return command('busctl','--user','call',owner.bus,'/org/mpris/MediaPlayer2','org.mpris.MediaPlayer2.Player',name);
}
async function osMetadata(owner:ReturnType<typeof ownedPlayer>,song:any,name:string,color?:number[]) {
 let metadata='';
 await expect.poll(()=>{metadata=property(owner,'Metadata');return metadata;},{timeout:15000}).toContain(JSON.stringify(song.title));
 const tags=(song as any).tags||{};
 expect(metadata).toContain(JSON.stringify(tags.artist||'Unknown artist'));
 if(tags.album)expect(metadata).toContain(JSON.stringify(tags.album));
 else expect(metadata.match(/"xesam:album" s "([^"]*)"/)?.[1]||'').toBe('');
 let art:any=null;
 if(color) {
  await expect.poll(()=>{metadata=property(owner,'Metadata');return metadata.match(/"mpris:artUrl" s "(file:[^"]+)"/)?.[1]||'';},{timeout:15000}).not.toBe('');
  const uri=metadata.match(/"mpris:artUrl" s "(file:[^"]+)"/)![1];
  const bytes=readFileSync(new URL(uri));const path=e.path(name+'-os-art.png');writeFileSync(path,bytes);
  const decoded=JSON.parse(command('python3','-c',"import json,sys;from PIL import Image;i=Image.open(sys.argv[1]).convert('RGB');i.load();print(json.dumps({'size':i.size,'pixel':i.getpixel((i.width//2,i.height//2))}))",path));
  for(let i=0;i<3;i++)expect(Math.abs(decoded.pixel[i]-color[i])).toBeLessThan(8);
  art={uri,path,decoded};
 }
 const path=e.path(name+'-os-metadata.json');writeFileSync(path,JSON.stringify({owner,metadata,art},null,2));
 return {metadata,art,path};
}
async function fallbackArt(owner:ReturnType<typeof ownedPlayer>,name:string,colors:number[][]) {
 let result:any={};
 await expect.poll(()=>{
  const metadata=property(owner,'Metadata');const uri=metadata.match(/"mpris:artUrl" s "(file:[^"]+)"/)?.[1];
  if(!uri){result={metadata,art:null};return true;}
  try{
   const path=e.path(name+'-os-fallback.png');writeFileSync(path,readFileSync(new URL(uri)));
   const decoded=JSON.parse(command('python3','-c',"import json,sys;from PIL import Image;i=Image.open(sys.argv[1]).convert('RGB');i.load();print(json.dumps({'size':i.size,'pixel':i.getpixel((i.width//2,i.height//2))}))",path));
   result={metadata,uri,path,decoded};return colors.every(color=>decoded.pixel.some((v:number,i:number)=>Math.abs(v-color[i])>=8));
  }catch{return false;}
 },{timeout:15000}).toBe(true);
 return result;
}
async function readiness(page:Page,owner:ReturnType<typeof ownedPlayer>){
 const windowState=await page.evaluate(()=>{const a=document.querySelector('audio') as HTMLAudioElement;return {url:location.href,metadata:navigator.mediaSession.metadata?{title:navigator.mediaSession.metadata.title,artwork:navigator.mediaSession.metadata.artwork}:null,playbackState:navigator.mediaSession.playbackState,deliveries:(window as any).__m54OSDeliveries,audio:{src:a?.currentSrc,paused:a?.paused,time:a?.currentTime,readyState:a?.readyState,error:a?.error?.message}};});
 return {windowState,osMetadata:exportedMetadata(owner),osPlayback:property(owner,'PlaybackStatus')};
}
function trackId(metadata:string){return metadata.match(/"mpris:trackid" o "([^"]+)"/)?.[1]||'';}
async function resetSaved(page:Page,id:number) {
 expect((await page.request.post('/api/progress/status',{data:{media_id:id,status:'not_started'}})).ok()).toBe(true);
 const before=await(await page.request.get(`/api/media/${id}`)).json();
 expect(before.progress?.position_seconds||0).toBe(0);return before;
}
function exportedMetadata(owner:ReturnType<typeof ownedPlayer>) {
 try{return property(owner,'Metadata');}catch(error){
  const active=command('busctl','--user','list','--no-legend','--no-pager');
  if(!active.split('\n').some(line=>line.startsWith(owner.bus+' ')))return '';
  throw error;
 }
}
async function toneAndSaved(page:Page,owner:ReturnType<typeof ownedPlayer>,song:any,name:string) {
 await expectPlaying(page,song.id);
 const capture=await captureOutput(page,1500,e.path(name+'.wav'));assertValidCapture(capture);
 const segment=mainSegment(capture.analysis);expect(segment?.duration_s).toBeGreaterThan(.8);
 expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
 let saved:any;
 await expect.poll(async()=>{saved=await(await page.request.get(`/api/media/${song.id}`)).json();return saved.progress?.position_seconds||0;},{timeout:10000}).toBeGreaterThan(1);
 expect(saved.media.id).toBe(song.id);
 action(owner,'Pause');await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
 return {capture,saved,segment};
}
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(process.env.PLAYER_MUSIC_EVIDENCE||f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page},info)=>{
 await page.context().addInitScript(installAudioTap);
 if(info.project.name==='chrome')await page.context().addInitScript(()=>{
  const session=navigator.mediaSession;if(!session)return;
  const original=session.setActionHandler;
  (window as any).__m54OSDeliveries=[];
  session.setActionHandler=function(action,handler){
   const observed=handler?function(this:unknown,...args:any[]){
    (window as any).__m54OSDeliveries.push({action,time:Date.now(),url:location.href,audio:{paused:(document.querySelector('audio') as HTMLAudioElement)?.paused,time:(document.querySelector('audio') as HTMLAudioElement)?.currentTime}});
    return Reflect.apply(handler,this,args);
   }:null;
   return Reflect.apply(original,this,[action,observed]);
  };
 });
 await keyboardLogin(page,f.user);
});

test('M54 actual MPRIS artwork metadata and OS previous next play pause follow real sources',async({page})=>{
 const a=track(f,'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
 test.skip(a.duration_s < 40, 'Requires --session-artwork fixture');
 const b=track(f,'Music/Tag Verification/source_001.flac');
 const c=track(f,'Music/Untagged/track_0042.mp3');
 const blue=[43,76,126],orange=[210,90,32];
 let pid='';const proof:any[]=[];
 try {
  const created=await page.request.post('/api/playlists',{data:{name:'M54 OS '+Date.now()}});expect(created.ok()).toBe(true);pid=(await created.json()).playlist_id;
  expect((await page.request.post(`/api/playlists/${pid}/songs`,{data:{action:'add',media_ids:[a.id,b.id,c.id]}})).ok()).toBe(true);
  for(const song of [a,b,c])expect((await page.request.post('/api/progress/status',{data:{media_id:song.id,status:'not_started'}})).ok()).toBe(true);
  await page.goto('/?music=playlist&id='+pid);await page.locator('.media-row').nth(0).click();await expectPlaying(page,a.id);
  let owner:ReturnType<typeof ownedPlayer>;
  await expect.poll(()=>{try{owner=ownedPlayer();return true;}catch{return false;}},{timeout:15000}).toBe(true);
  owner=ownedPlayer();
  proof.push(await osMetadata(owner,a,'m54-first',blue));proof.push(await toneAndSaved(page,owner,a,'m54-first'));
  const paused=await audioState(page);action(owner,'Pause');await page.waitForTimeout(600);expect(Math.abs((await audioState(page)).time-paused.time)).toBeLessThan(.15);
  action(owner,'Play');await expectPlaying(page,a.id);action(owner,'Pause');
  proof.push({beforeNext:await resetSaved(page,b.id)});
  action(owner,'Next');proof.push(await osMetadata(owner,b,'m54-next',orange));proof.push(await toneAndSaved(page,owner,b,'m54-next'));
  proof.push({beforePrevious:await resetSaved(page,a.id)});
  action(owner,'Previous');proof.push(await osMetadata(owner,a,'m54-previous',blue));proof.push(await toneAndSaved(page,owner,a,'m54-previous'));
  for(const [theme,width] of [['dark',1366],['light',390]] as const) {
   await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect(page.locator('#player-music-identity')).toContainText('Aurora Lane · Night Transit');await page.screenshot({path:e.path(`m54-full-${theme}-${width}.png`)});
   await page.locator('#btn-minimize').click();await expect(page.locator('#player-restore-title')).toContainText('Aurora Lane · Night Transit');await page.screenshot({path:e.path(`m54-mini-${theme}-${width}.png`)});
   await page.getByRole('button',{name:'Restore player',exact:true}).click();
  }
  // Native typing and keyboard traversal remain separate from system actions.
  await page.keyboard.press('/');await page.locator('#search-input').focus();await page.keyboard.type('Np/r');expect(await page.locator('#search-input').inputValue()).toBe('Np/r');await expect(page.locator('#search-input')).toBeFocused();expect((await audioState(page)).src).toContain(`/media/${a.id}/`);await page.locator('#search-input').fill('');await page.keyboard.press('Escape');
  action(owner,'Next');await expectPlaying(page,b.id);action(owner,'Next');await expectPlaying(page,c.id);action(owner,'Pause');
  await expect.poll(()=>property(owner,'Metadata')).toContain(JSON.stringify(c.title));
  const missing=property(owner,'Metadata');expect(missing).toContain('Unknown artist');
  const fallbackUri=missing.match(/"mpris:artUrl" s "(file:[^"]+)"/)?.[1];
  if(fallbackUri){
   const path=e.path('m54-actual-os-fallback-art.png');writeFileSync(path,readFileSync(new URL(fallbackUri)));
   const pixel=JSON.parse(command('python3','-c',"import json,sys;from PIL import Image;i=Image.open(sys.argv[1]).convert('RGB');i.load();print(json.dumps(i.getpixel((i.width//2,i.height//2))))",path));
   for(const color of [blue,orange])expect(pixel.some((v:number,i:number)=>Math.abs(v-color[i])>=8),'OS fallback cannot reuse previous cover pixels under a new URL').toBe(true);
   proof.push({fallbackUri,path,pixel});
  }
  await page.screenshot({path:e.path('m54-untagged-no-art.png')});
  // q is the documented actual stop-and-close action. An OS default app icon/title is allowed; the old music identity must disappear.
  await page.locator('#btn-minimize').focus();await page.keyboard.press('q');
  await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
  await expect.poll(()=>exportedMetadata(owner)).not.toContain(JSON.stringify(c.title));
  proof.push({closedExport:exportedMetadata(owner)});
  e.record({feature:'M54',scenario:'real OS MPRIS metadata/decoded artwork and all four transport commands with distinct source tones/per-ID saved progress; themed full/mini and untagged fallback',result:'pass',data:{owner,proof,missing}});
 }catch(error){e.record({feature:'M54',scenario:'real OS controls',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally {if(pid)await page.request.delete('/api/playlists/'+pid);}
});

test('M54 detached OS owner, denied artwork and offline retry keep the current track identity',async({page,context},info)=>{
 const a=track(f,'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
 test.skip(a.duration_s < 40, 'Requires --session-artwork fixture');
 const b=track(f,'Music/Tag Verification/source_001.flac');
 const d=track(f,'Music/Partial Tags/Lonely Single.mp3');
 let pid='';const proof:any[]=[];
 try {
  const created=await page.request.post('/api/playlists',{data:{name:'M54 ownership '+Date.now()}});expect(created.ok()).toBe(true);pid=(await created.json()).playlist_id;
  expect((await page.request.post(`/api/playlists/${pid}/songs`,{data:{action:'add',media_ids:[a.id,b.id,d.id]}})).ok()).toBe(true);
  await page.goto('/?music=playlist&id='+pid);await page.locator('.media-row').nth(0).click();await expectPlaying(page,a.id);
  const owner=ownedPlayer();action(owner,'Pause');
  proof.push(await osMetadata(owner,a,'m54-main-before-detach',[43,76,126]));
  const mainTrackId=trackId(property(owner,'Metadata'));expect(mainTrackId).not.toBe('');
  proof.push({mainBeforeDetach:await readiness(page,owner),mainTrackId});
  const opened=context.waitForEvent('page');await page.locator('#btn-minimize').focus();await page.keyboard.press('d');
  const popup=await opened;await popup.waitForLoadState('domcontentloaded');
  await expect.poll(()=>popup.locator('#player-music-identity').textContent()).toContain(a.title);
  expect(await page.evaluate(()=>navigator.mediaSession.metadata)).toBeNull();
  await popup.locator('#btn-play').click();await expectPlaying(popup,a.id);
  proof.push(await osMetadata(owner,a,'m54-detached-first',[43,76,126]));
  proof.push({beforeDetachedNext:await resetSaved(page,b.id)});action(owner,'Next');
  proof.push(await osMetadata(owner,b,'m54-detached-next',[210,90,32]));proof.push(await toneAndSaved(popup,owner,b,'m54-detached-next'));
  expect(await page.evaluate(()=>navigator.mediaSession.metadata)).toBeNull();
  proof.push({popupBeforeClose:await readiness(popup,owner),popupTrackId:trackId(property(owner,'Metadata'))});
  const popupPath=e.path('m54-detached-os-owner.png');await popup.screenshot({path:popupPath});
  let closeSeen=false;const closed=popup.waitForEvent('close').then(()=>{closeSeen=true;});
  try{await popup.keyboard.press('d');}catch(error){await closed;if(!closeSeen||!popup.isClosed()||!String(error).includes('has been closed'))throw error;}
  await closed;expect(popup.isClosed()).toBe(true);
  await expect(page.locator('#player')).not.toHaveClass(/hidden/);
  await expect(page.locator('#player-music-identity')).toContainText(b.title);
  try{
   proof.push({mainVisibleBeforeOSReady:await readiness(page,owner)});
   await expect(page.locator('#player-status')).toHaveText('Playback returned here. Press Play to resume.');
   expect((await audioState(page)).paused).toBe(true);
   await expect.poll(()=>page.locator('#media-audio').evaluate((audio:HTMLAudioElement)=>audio.readyState)).toBeGreaterThan(0);
   if(info.project.name==='chrome'){
    proof.push({observedChromePausedReturnRequiresVisiblePlay:await readiness(page,owner),osOnlyFailureRetained:'actual-attempt5/chrome/evidence.jsonl'});
    proof.push({beforeChromeVisibleResume:await resetSaved(page,b.id)});
    await page.locator('#btn-play').click();await expectPlaying(page,b.id);
    proof.push(await osMetadata(owner,b,'m54-reattached-after-visible-play',[210,90,32]));
    proof.push(await toneAndSaved(page,owner,b,'m54-reattached-after-visible-play'));
    action(owner,'Pause');await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
    action(owner,'Play');await expectPlaying(page,b.id);action(owner,'Pause');
   }else{
    proof.push(await osMetadata(owner,b,'m54-reattached',[210,90,32]));
    proof.push({mainBeforeOSPlay:await readiness(page,owner)});
    action(owner,'Play');await expectPlaying(page,b.id);action(owner,'Pause');
   }
   await expect(page.locator('#player-status')).toHaveText('');
   proof.push({mainAfterOSPlay:await readiness(page,owner)});
  }catch(error){proof.push({reattachFailure:await readiness(page,owner)});throw error;}
  // Keep the existing default visualization choice; use its visible Off
  // control to inspect the actual known cover on the full-player surface.
  await page.getByRole('combobox',{name:'Audio visualization'}).selectOption('off');
  await expect(page.locator('#cover-art')).toBeVisible();
  expect(await page.locator('#cover-art').evaluate((image:HTMLImageElement)=>image.naturalWidth)).toBeGreaterThan(0);
  for(const [theme,width] of [['dark',1366],['light',390]] as const){
   await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect(page.locator('#player-music-identity')).toContainText('A & B; C/D · Metadata Album');
   await page.screenshot({path:e.path(`m54-cover-full-${theme}-${width}.png`)});
   await page.locator('#btn-minimize').click();await expect(page.locator('.player-mini-art')).toBeVisible();
   expect(await page.locator('.player-mini-art').evaluate((image:HTMLImageElement)=>image.naturalWidth)).toBeGreaterThan(0);
   await page.screenshot({path:e.path(`m54-cover-mini-${theme}-${width}.png`)});await page.getByRole('button',{name:'Restore player',exact:true}).click();
  }
  // A real failed stream after the browser's offline transport cutoff must be
  // retryable through system Play; the old cover must not return as B's art.
  let deniedArtHits=0;
  await context.route(`**/api/media/${a.id}/thumbnail`,route=>{deniedArtHits++;return route.fulfill({status:403,body:'Denied artwork'});});
  action(owner,'Previous');await expectPlaying(page,a.id);action(owner,'Pause');
  await expect.poll(()=>property(owner,'Metadata')).toContain(JSON.stringify(a.title));
  await expect.poll(()=>deniedArtHits).toBeGreaterThan(0);
  expect(await page.evaluate(()=>navigator.mediaSession.metadata?.artwork.length)).toBe(0);
  const denied=await fallbackArt(owner,'m54-injected403',[[43,76,126],[210,90,32]]);
  proof.push({injected403Requests:deniedArtHits,denied});
  action(owner,'Next');await expectPlaying(page,b.id);action(owner,'Pause');
  proof.push({beforeUntouchedOfflineTarget:await resetSaved(page,d.id)});
  await context.setOffline(true);action(owner,'Next');
  await expect.poll(async()=>page.locator('#toast').textContent(),{timeout:15000}).toMatch(/Cannot play|could not|couldn't|failed|network/i);
  if(info.project.name==='chrome'){
   await expect(page.locator('#player-music-identity')).toContainText(d.title);
   proof.push({chromeFailedSourceVisiblePlayPrecondition:await readiness(page,owner),fallback:await fallbackArt(owner,'m54-offline-no-stale-art',[[43,76,126],[210,90,32]])});
   expect(property(owner,'Metadata')).not.toContain(JSON.stringify(b.title));
  }else await expect.poll(()=>property(owner,'Metadata')).toContain(JSON.stringify(d.title));
  expect(property(owner,'PlaybackStatus')).not.toContain('Playing');
  const offlinePath=e.path('m54-offline-current-source.png');await page.screenshot({path:offlinePath});
  await context.setOffline(false);
  if(info.project.name==='chrome')await page.locator('#btn-play').click();else action(owner,'Play');
  await expectPlaying(page,d.id);
  proof.push(await osMetadata(owner,d,'m54-offline-retry'));proof.push(await toneAndSaved(page,owner,d,'m54-offline-retry'));
  await page.getByRole('button',{name:'Log out',exact:true}).click();await page.waitForURL(url=>url.pathname==='/login.html');
  await expect.poll(()=>exportedMetadata(owner)).not.toContain(JSON.stringify(d.title));
  e.record({feature:'M54',scenario:'actual popup owns OS metadata/Next with real audio; paused return preserves intent (Chrome explicit visible Play activation, Firefox OS-only resume); actual OS controls then work; actual routed injected403 art response clears both previous cover colors; offline recovery (Chrome visible Play activation, Firefox OS retry) and logout clear session',result:'pass',data:{owner,proof},artifacts:[popupPath,offlinePath]});
 }catch(error){
  const windows=[];for(const target of context.pages()){try{windows.push({url:target.url(),state:await target.evaluate(()=>{const a=document.querySelector('audio') as HTMLAudioElement;return {metadata:navigator.mediaSession.metadata?{title:navigator.mediaSession.metadata.title,artist:navigator.mediaSession.metadata.artist,album:navigator.mediaSession.metadata.album,artwork:navigator.mediaSession.metadata.artwork}:null,playbackState:navigator.mediaSession.playbackState,audio:{src:a?.currentSrc,paused:a?.paused,time:a?.currentTime,readyState:a?.readyState,error:a?.error?.message},deliveries:(window as any).__m54OSDeliveries};})});}catch{}}
  e.record({feature:'M54',scenario:'OS ownership/error boundaries',result:'fail',detail:String(error),data:{proof,windows}});throw error;
 }
 finally {await context.setOffline(false);if(pid)await page.request.delete('/api/playlists/'+pid).catch(()=>{});}
});

test('M54 Chrome failed untouched source uses visible Play then actual OS controls and clears on logout',async({page,context},info)=>{
 test.skip(info.project.name!=='chrome','Observed Chrome 144 audio-focus preactivation boundary');
 const a=track(f,'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
 const d=track(f,'Music/Partial Tags/Lonely Single.mp3');
 let pid='';const proof:any[]=[];
 try{
  const created=await page.request.post('/api/playlists',{data:{name:'M54 Chrome offline '+Date.now()}});expect(created.ok()).toBe(true);pid=(await created.json()).playlist_id;
  expect((await page.request.post(`/api/playlists/${pid}/songs`,{data:{action:'add',media_ids:[a.id,d.id]}})).ok()).toBe(true);
  proof.push({beforeFreshD:await resetSaved(page,d.id)});
  await page.goto('/?music=playlist&id='+pid);await page.locator('.media-row').nth(0).click();await expectPlaying(page,a.id);
  const owner=ownedPlayer();proof.push(await osMetadata(owner,a,'m54-focused-before-offline',[43,76,126]));action(owner,'Pause');
  await context.setOffline(true);action(owner,'Next');
  await expect.poll(()=>page.locator('#toast').textContent(),{timeout:15000}).toMatch(/Cannot play/i);
  await expect(page.locator('#player-music-identity')).toContainText('Lonely Single');
  const failed=await readiness(page,owner);expect(failed.windowState.metadata?.title).toBe(d.title);
  expect(failed.windowState.audio.error).toBeTruthy();expect(failed.windowState.audio.paused).toBe(true);
  expect(failed.osPlayback).not.toContain('Playing');expect(failed.osMetadata).not.toContain(JSON.stringify(a.title));
  const fallback=await fallbackArt(owner,'m54-focused-offline-fallback',[[43,76,126],[210,90,32]]);
  const offlineShot=e.path('m54-focused-visible-error.png');await page.screenshot({path:offlineShot});proof.push({failed,fallback});
  await context.setOffline(false);await page.locator('#btn-play').click();await expectPlaying(page,d.id);
  proof.push(await osMetadata(owner,d,'m54-focused-after-visible-play'));proof.push(await toneAndSaved(page,owner,d,'m54-focused-after-visible-play'));
  action(owner,'Play');await expectPlaying(page,d.id);action(owner,'Pause');await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
  action(owner,'Previous');await expectPlaying(page,a.id);proof.push(await osMetadata(owner,a,'m54-focused-recovered-previous',[43,76,126]));
  action(owner,'Next');await expectPlaying(page,d.id);proof.push(await osMetadata(owner,d,'m54-focused-recovered-next'));action(owner,'Pause');
  const active=property(owner,'Metadata');
  await page.getByRole('button',{name:'Log out',exact:true}).click();await page.waitForURL(url=>url.pathname==='/login.html');
  await expect.poll(()=>exportedMetadata(owner)).not.toContain(JSON.stringify(d.title));
  e.record({feature:'M54',scenario:'Chrome144 failed untouched source loses OS focus without stale track/art; existing visible Play after reconnect activates real OS metadata/Play/Pause/Previous/Next, audible fresh D progress and logout clears',result:'pass',detail:'OS-only failed-source metadata/retry before visible Play is not supported in observed Chrome144; prior actual-attempt7 failure retained.',data:{owner,proof,active,cleared:exportedMetadata(owner)},artifacts:[offlineShot]});
 }catch(error){e.record({feature:'M54',scenario:'Chrome focused offline/clear',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally{await context.setOffline(false);if(pid)await page.request.delete('/api/playlists/'+pid).catch(()=>{});}
});
