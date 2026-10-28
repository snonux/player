import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, openQueueMoreAction, openQueueRowMenuAction } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let f:MusicFixture,e:Evidence;
const panel=(p:Page)=>p.locator('#queue-panel');
const row=(p:Page,i:number)=>panel(p).locator(`[data-queue-index="${i}"]`).first();
const q=(p:Page)=>p.evaluate(async()=> (await import('/js/queue.js')).queueState());
async function saved(p:Page) {
 const cfg=await(await p.request.get('/api/config')).json();
 return p.evaluate(id=>JSON.parse(localStorage.getItem(`player.queue.${location.origin}.${id}`)||'null'),cfg.user_id);
}
const songs=()=>['01 - First Light.mp3','02 - Long Road Home.mp3'].map(name=>track(f,`Music/Aurora Lane - Night Transit (2021)/${name}`));
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.context().addInitScript(installAudioTap);await keyboardLogin(page,f.user);});
async function openQueue(page:Page) {if(!await panel(page).isVisible()) await page.keyboard.press('Q');await expect(panel(page)).toBeVisible();}
async function closeQueue(page:Page) {if(await panel(page).isVisible()) await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();}
async function mode(page:Page,value:string) {
 await openQueue(page);
 for(let i=0;(await q(page)).repeat!==value && i<3;i++) await panel(page).locator('[data-queue-action="repeat"]').click();
 await expect(panel(page).getByRole('button',{name:`Repeat: ${value[0].toUpperCase()+value.slice(1)}`,exact:true})).toBeVisible();
 expect((await q(page)).repeat).toBe(value);
}
async function toggle(page:Page) {
 if(!await page.locator('#btn-play').isVisible()) {
  const restore=page.getByRole('button',{name:'Restore player',exact:true});
  if(await restore.isVisible()) await restore.click();
 }
 await expect(page.locator('#btn-play')).toBeVisible();await page.locator('#btn-play').click();
}
async function logout(page:Page) {
 await closeQueue(page);await page.getByRole('button',{name:'Log out',exact:true}).click();
 await page.waitForURL(url=>url.pathname==='/login.html');await expect(page.locator('#username')).toBeVisible();
}
async function finished(page:Page,id:number) {
 let progress:any;
 await expect.poll(async()=>{progress=(await(await page.request.get(`/api/media/${id}`)).json()).progress;return progress?.finished;},{timeout:10000}).toBe(true);
 return progress;
}
async function seed(page:Page, index=2) {
 const [a,b]=songs();
 const created=await page.request.post('/api/v1/playlists',{data:{name:'M20 repeat '+Date.now()}});expect(created.ok()).toBe(true);
 const id=(await created.json()).playlist_id;
 expect((await page.request.post(`/api/v1/playlists/${id}/songs`,{data:{action:'add',media_ids:[a.id,a.id,b.id]}})).ok()).toBe(true);
 await page.goto('/?music=playlist&id='+id);await expect(page.locator('.media-row')).toHaveCount(3);
 await page.locator('.media-row').nth(index).click();await expectPlaying(page,index===2?b.id:a.id);
 await toggle(page);await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
 return {id,a,b};
}
async function playRow(page:Page,index:number) {
 await openQueue(page);await row(page,index).locator('[data-queue-action="play"]').click();
 await closeQueue(page);
}
async function tone(page:Page,song:any,name:string) {
 const capture=await captureOutput(page,1300,e.path(name+'.wav'));assertValidCapture(capture);
 const segment=mainSegment(capture.analysis);expect(segment?.duration_s).toBeGreaterThan(.8);
 expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
 return {runtime:await audioState(page),tone:segment,wav:capture.wav};
}
async function naturalFinish(page:Page) {
 await expect.poll(()=>page.locator('#media-audio').evaluate((m:HTMLAudioElement)=>m.ended),{timeout:18000}).toBe(true);
 await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
}

test('M20 Off stops and replays, All wraps distinct source, One repeats exact occurrence and manual transport',async({page})=>{
 const proof:any[]=[];let pid='';
 try {
  const {id,a,b}=await seed(page);pid=id;
  await mode(page,'off');await playRow(page,2);await expectPlaying(page,b.id);
  proof.push(await tone(page,b,'m20-off-last-playing'));
  await naturalFinish(page);const stopped=await q(page);expect(stopped.index).toBe(2);
  const endedTime=(await audioState(page)).time;await page.waitForTimeout(1000);expect((await audioState(page)).time).toBeCloseTo(endedTime,1);
  const complete=await finished(page,b.id);
  await toggle(page);await expectPlaying(page,b.id);await naturalFinish(page);expect((await q(page)).index).toBe(2);
  await mode(page,'all');await row(page,0).focus();await page.keyboard.press('J');const editedFirst=(await q(page)).entries[0].id;await playRow(page,2);await expectPlaying(page,b.id);
  await expect.poll(async()=>(await q(page)).index,{timeout:18000}).toBe(0);await expectPlaying(page,a.id);
  expect((await q(page)).entries[(await q(page)).index].id).toBe(editedFirst);
  proof.push(await tone(page,a,'m20-all-wrap-first'));
  expect((await audioState(page)).time).toBeLessThan(5);await toggle(page);
  await mode(page,'one');await playRow(page,1);await expectPlaying(page,a.id);
  const occurrence=(await q(page)).entries[1].id;
  await expect.poll(async()=>(await audioState(page)).time,{timeout:16000}).toBeGreaterThan(10);
  await expect.poll(async()=>(await audioState(page)).time,{timeout:6000}).toBeLessThan(2);
  const repeated=await q(page);expect(repeated.index).toBe(1);expect(repeated.entries[1].id).toBe(occurrence);
  proof.push(await tone(page,a,'m20-one-repeated-second-occurrence'));
  await page.keyboard.press('N');await expectPlaying(page,b.id);proof.push(await tone(page,b,'m20-one-manual-next'));
  await page.keyboard.press('P');await expectPlaying(page,a.id);expect((await q(page)).index).toBe(1);
  await toggle(page);
  e.record({feature:'M20',scenario:'actual Off natural stop and same-source Replay; All audible wrap; One exact occurrence zero restart and explicit next/previous',result:'pass',data:{proof,complete,occurrence,repeated}});
 }catch(error){e.record({feature:'M20',scenario:'repeat transport',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally {if(pid)await page.request.delete('/api/playlists/'+pid);}
});

test('M20 edited queue mode survives paused reload, account isolation and empty Clear; Y typing and themes',async({page})=>{
 let pid='';const artifacts:string[]=[];
 try {
  const {id,a}=await seed(page,1);pid=id;await mode(page,'one');
  await row(page,1).focus();await page.keyboard.press('J');await expect(row(page,2)).toBeFocused();
  const before=await saved(page);expect(before.repeat).toBe('one');expect(before.index).toBe(2);
  await page.reload();await openQueue(page);await expect(panel(page).getByRole('button',{name:'Resume queue',exact:true})).toBeVisible();
  const recovered=await saved(page);expect(recovered.entries).toEqual(before.entries);expect(recovered.index).toBe(2);expect(recovered.repeat).toBe('one');expect((await audioState(page)).paused).toBe(true);
  const repeat=panel(page).locator('[data-queue-action="repeat"]');await repeat.focus();await page.keyboard.press('Y');await expect(repeat).toBeFocused();expect((await q(page)).repeat).toBe('off');
  await page.keyboard.press('Y');await expect(repeat).toBeFocused();expect((await q(page)).repeat).toBe('all');
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
   await page.setViewportSize({width,height:844});
   if(await page.locator('html').getAttribute('data-theme')!==theme){await closeQueue(page);await page.getByRole('button',{name:'Toggle theme',exact:true}).click();await openQueue(page);}
   const path=e.path(`m20-repeat-${theme}-${width}.png`);await page.screenshot({path,animations:'disabled'});artifacts.push(path);
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
  await page.setViewportSize({width:1366,height:860});
  await panel(page).getByRole('button',{name:'Resume queue',exact:true}).click();await expectPlaying(page,a.id);await closeQueue(page);await toggle(page);
  await page.keyboard.press('/');const search=page.getByRole('searchbox',{name:'Search media'});await search.fill('Y j/k Q');await expect(search).toHaveValue('Y j/k Q');expect((await q(page)).repeat).toBe('all');await page.keyboard.press('Escape');
  await logout(page);await keyboardLogin(page,f.admin);await openQueue(page);expect((await q(page)).repeat).toBe('off');expect((await q(page)).entries).toHaveLength(0);
  await closeQueue(page);await logout(page);await keyboardLogin(page,f.user);await openQueue(page);expect((await q(page)).repeat).toBe('all');
  const selected=(await q(page)).index;await openQueueRowMenuAction(page,selected,/^Remove /);expect((await audioState(page)).paused).toBe(true);await openQueueMoreAction(page,'Undo');
  await openQueueMoreAction(page,'Clear queue');await openQueueMoreAction(page,'Undo');expect((await q(page)).repeat).toBe('all');
  await openQueueMoreAction(page,'Clear queue');await page.reload();await openQueue(page);expect((await q(page)).entries).toHaveLength(0);expect((await q(page)).repeat).toBe('all');
  e.record({feature:'M20',scenario:'edited occurrence mode recovery paused, explicit Resume, Y native focus/typing, theme/narrow screenshots, account isolation and Clear reload',result:'pass',data:{before,recovered},artifacts});
 }catch(error){e.record({feature:'M20',scenario:'repeat recovery and UI',result:'fail',detail:String(error),artifacts});throw error;}
 finally {if(pid)await page.request.delete('/api/playlists/'+pid);}
});

test('M20 offline restore Retry retains mode and resumed selection',async({page,context})=>{
 let pid='';
 try {
  const {id}=await seed(page,1);pid=id;await mode(page,'one');const before=await saved(page);
  await page.route('**/api/media/*',route=>route.abort('failed'));await page.reload();await openQueue(page);await expect(panel(page).getByRole('button',{name:'Retry restore',exact:true})).toBeVisible();
  await page.unroute('**/api/media/*');await context.setOffline(true);
  await panel(page).getByRole('button',{name:'Retry restore',exact:true}).click();await expect(panel(page)).toContainText('Check your connection and retry');
  expect((await saved(page)).repeat).toBe('one');expect((await audioState(page)).paused).toBe(true);
  await context.setOffline(false);await panel(page).getByRole('button',{name:'Retry restore',exact:true}).click();await expect(panel(page).getByRole('button',{name:'Resume queue',exact:true})).toBeVisible();
  expect((await q(page)).repeat).toBe('one');expect((await q(page)).index).toBe(before.index);
  e.record({feature:'M20',scenario:'initial transport abort then real browser offline Retry retains repeat mode, online restore paused',result:'pass',data:{before,after:await saved(page)}});
 }catch(error){e.record({feature:'M20',scenario:'repeat offline boundary',result:'fail',detail:String(error)});throw error;}
 finally {await context.setOffline(false);if(pid)await page.request.delete('/api/playlists/'+pid);}
});


test('M20 real detached player finishes old run before One repeat and All wrap; popup Y',async({page})=>{
 let pid='';let popup:Page|undefined;const proof:any[]=[];
 try {
  const {id,a,b}=await seed(page,1);pid=id;await mode(page,'one');await closeQueue(page);
  const opened=page.waitForEvent('popup');await page.keyboard.press('d');popup=await opened;
  await expect(popup.locator('#media-audio')).toBeAttached();
  await expect.poll(async()=>(await audioState(popup!)).src).toContain(`/media/${a.id}/`);
  await popup.locator('#btn-play').click();await expectPlaying(popup,a.id);
  const occurrence=(await q(page)).entries[1].id;
  await expect.poll(async()=>(await audioState(popup!)).time,{timeout:16000}).toBeGreaterThan(10);
  await expect.poll(async()=>(await audioState(popup!)).time,{timeout:6000}).toBeLessThan(2);
  expect((await q(page)).index).toBe(1);expect((await q(page)).entries[1].id).toBe(occurrence);
  const aFinished=await finished(page,a.id);
  proof.push(await tone(popup,a,'m20-detached-one'));
  await popup.keyboard.press('Y');await expect.poll(async()=>(await q(page)).repeat).toBe('off');
  await popup.keyboard.press('Y');await expect.poll(async()=>(await q(page)).repeat).toBe('all');
  await popup.keyboard.press('N');await expectPlaying(popup,b.id);expect((await q(page)).index).toBe(2);
  await expect.poll(async()=>(await q(page)).index,{timeout:18000}).toBe(0);await expectPlaying(popup,a.id);
  proof.push(await tone(popup,a,'m20-detached-all-wrap'));
  const bFinished=await finished(page,b.id);
  await popup.locator('#btn-play').click();const shot=e.path('m20-detached-repeat.png');await popup.screenshot({path:shot,animations:'disabled'});
  e.record({feature:'M20',scenario:'actual popup owns final position then finished before One same-occurrence reset, popup Y and All audible wrap',result:'pass',data:{proof,occurrence,finished:aFinished,bFinished},artifacts:[shot]});
 }catch(error){e.record({feature:'M20',scenario:'detached repeat',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally {await popup?.close();if(pid)await page.request.delete('/api/playlists/'+pid);}
});
