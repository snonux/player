import { writeFileSync } from 'node:fs';
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, openQueueMoreAction} from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let f:MusicFixture,e:Evidence;
const panel=(p:Page)=>p.locator('#queue-panel');
const rows=(p:Page)=>panel(p).locator('[data-queue-index]');
const row=(p:Page,i:number)=>panel(p).locator(`[data-queue-index="${i}"]`).first();
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,f.user);});
async function stored(page:Page) {
 const cfg=await(await page.request.get('/api/v1/config')).json();
 return page.evaluate(id=>JSON.parse(localStorage.getItem(`player.queue.${location.origin}.${id}`)||'null'),cfg.user_id);
}
async function state(page:Page) { return page.evaluate(async()=>{const q=await import('/js/queue.js');return q.queueState();}); }
async function seedAndPlay(page:Page) {
 const a=track(f,'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
 const b=track(f,'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3');
 const create=await page.request.post('/api/v1/playlists',{data:{name:'M16 recovery '+Date.now()}});expect(create.ok()).toBe(true);
 const id=(await create.json()).playlist_id;
 expect((await page.request.post(`/api/v1/playlists/${id}/songs`,{data:{action:'add',media_ids:[a.id,b.id,a.id]}})).ok()).toBe(true);
 await page.goto('/?music=playlist&id='+id);
 await expect(page.locator('.media-row')).toHaveCount(3);
 // Play the later actual duplicate occurrence, not a media-ID lookup.
 await page.locator('.media-row').nth(2).click();
 await expectPlaying(page,a.id);await page.locator('#btn-play').click();
 await expect.poll(async()=> (await audioState(page)).paused).toBe(true);
 await page.keyboard.press('Q');await expect(row(page,2)).toBeFocused();
 await page.keyboard.press('K');await expect(row(page,1)).toBeFocused();
 await expect.poll(async()=> (await stored(page))?.index).toBe(1);
 // Seek through actual player control at a fraction independent of viewport width.
 await page.keyboard.press('Escape');
 const trackBox=await page.locator('#progress-track').boundingBox();expect(trackBox).not.toBeNull();
 await page.locator('#progress-track').click({position:{x:trackBox!.width*.42,y:Math.min(5,trackBox!.height/2)}});
 await expect.poll(async()=> (await stored(page))?.position).toBeGreaterThan(3);
 await expect.poll(async()=>Math.abs((await stored(page)).position-(await audioState(page)).time)).toBeLessThan(.05);
 await page.keyboard.press('Q');
 const snapshot=await stored(page);expect(snapshot.entries.map((v:any)=>v.mediaId)).toEqual([a.id,a.id,b.id]);
 expect(snapshot.entries[1].id).not.toBe(snapshot.entries[0].id);
 return {id,a,b,snapshot};
}
async function recovered(page:Page,snapshot:any) {
 await page.reload();await page.keyboard.press('Q');
 await expect(panel(page).getByRole('button',{name:'Resume queue',exact:true})).toBeVisible();
 const q=await state(page);expect(q.entries.map((v:any)=>v.id)).toEqual(snapshot.entries.map((v:any)=>v.id));
 expect(q.index).toBe(snapshot.index);expect(q.position).toBeCloseTo(snapshot.position,1);expect(q.active).toBe(false);
 await expect(row(page,q.index)).toContainText('Selected');
 await expect.poll(async()=>Math.abs((await audioState(page)).time-snapshot.position)).toBeLessThan(.15);
 const before=await audioState(page);expect(before.paused).toBe(true);await page.waitForTimeout(1000);
 const after=await audioState(page);expect(after.paused).toBe(true);expect(Math.abs(after.time-before.time)).toBeLessThan(.15);
 return q;
}

test('M16 repeated reordered queue reload exact short-song offset paused Resume and account isolation',async({page})=>{
 const artifacts:string[]=[];let pid='';
 try {
  const {id,a,snapshot}=await seedAndPlay(page);pid=id;
  writeFileSync(e.path('m16-saved-before-reload.json'),JSON.stringify(snapshot,null,2));
  await recovered(page,snapshot);
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
   await page.setViewportSize({width,height:844});
   if(await page.locator('html').getAttribute('data-theme')!==theme){await panel(page).getByRole('button',{name:'Close queue'}).click();await page.getByRole('button',{name:'Toggle theme',exact:true}).click();await page.keyboard.press('Q');}
   const path=e.path(`m16-recovered-${theme}-${width}.png`);await page.screenshot({path,animations:'disabled'});artifacts.push(path);
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
  const resumeStarted=Date.now();
  await panel(page).getByRole('button',{name:'Resume queue',exact:true}).click();const resumedRuntime=await expectPlaying(page,a.id);
  expect(resumedRuntime.time).toBeGreaterThanOrEqual(snapshot.position-.15);
  expect(resumedRuntime.time).toBeLessThan(snapshot.position+(Date.now()-resumeStarted)/1000+1);
  const output=await captureOutput(page,1300,e.path('m16-resumed-output.wav'));assertValidCapture(output);
  const tone=mainSegment(output.analysis);expect(tone!.duration_s).toBeGreaterThan(.8);expect(Math.abs(tone!.tone_hz!-a.tone_hz[0])).toBeLessThan(5);artifacts.push(output.wav);
  await page.keyboard.press('Escape');await page.keyboard.press('Escape');
  if((await page.locator('#player').getAttribute('class')||'').includes('minimized')) await page.getByRole('button',{name:'Restore player',exact:true}).click();
  await page.locator('#btn-play').click();
  const saved=await stored(page);expect(saved.index).toBe(snapshot.index);expect(saved.position).toBeGreaterThan(snapshot.position);
  const other=track(f,'Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac');
  // A completed saved position must not turn a cold paused preview into Next.
  expect((await page.request.post(`/api/v1/media/${other.id}/progress`,{data:{position_seconds:other.duration_s}})).ok()).toBe(true);
  await page.goto('/?media='+other.id);
  await expect.poll(async()=> (await audioState(page)).src).toContain(`/media/${other.id}/`);
  expect((await audioState(page)).paused).toBe(true);
  await page.keyboard.press('Q');
  expect((await state(page)).entries.map((v:any)=>v.id)).toEqual(saved.entries.map((v:any)=>v.id));
  expect((await state(page)).index).toBe(saved.index);
  await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Log out',exact:true}).click();await keyboardLogin(page,f.admin);
  await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue is empty');expect((await state(page)).label).toBe('');
  await page.keyboard.press('Escape');await page.getByRole('button',{name:'Log out',exact:true}).click();await keyboardLogin(page,f.user);
  await page.keyboard.press('Q');await expect(panel(page).getByRole('button',{name:'Resume queue',exact:true})).toBeVisible();
  expect((await state(page)).entries.map((v:any)=>v.id)).toEqual(saved.entries.map((v:any)=>v.id));
  await openQueueMoreAction(page,'Clear queue');await page.reload();await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue is empty');
  // Typing and native button focus remain owned by existing controls.
  await page.keyboard.press('Escape');await page.keyboard.press('/');await page.getByRole('searchbox',{name:'Search media'}).fill('r/h/l/? Q J K');await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('r/h/l/? Q J K');
  e.record({feature:'M16',scenario:'actual repeated queue reorder pause reload silent recovery explicit Resume source tone account isolation Clear reload typing themes',result:'pass',data:{snapshot,saved,tone,resumedRuntime},artifacts});
 }catch(error){e.record({feature:'M16',scenario:'reload recovery',result:'fail',detail:String(error),data:{pid},artifacts});throw error;}
 finally {if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});

test('M16 revalidation permission drop transient network failure Retry and corrupt storage',async({page,context})=>{
 let pid='';const artifacts:string[]=[];
 try {
  const seeded=await seedAndPlay(page);pid=seeded.id;
  const cfg=await(await page.request.get('/api/v1/config')).json();
  const queueKey=await page.evaluate(uid=>`player.queue.${location.origin}.${uid}`,cfg.user_id);
  // Fault injection is explicitly transport abortion, not a claimed full offline server run.
  await page.route('**/api/media/*',route=>route.abort('failed'));
  await page.reload();await page.keyboard.press('Q');await expect(panel(page).getByRole('button',{name:'Retry restore'})).toBeVisible();
  expect((await stored(page)).entries).toEqual(seeded.snapshot.entries);
  await page.unroute('**/api/media/*');
  await context.setOffline(true);
  await panel(page).getByRole('button',{name:'Retry restore'}).click();
  await expect(panel(page)).toContainText('Check your connection and retry');
  const offlineSnapshot=await page.evaluate(key=>localStorage.getItem(key),queueKey);
  expect(offlineSnapshot).not.toBeNull();
  expect(JSON.parse(offlineSnapshot!).entries).toEqual(seeded.snapshot.entries);
  expect((await audioState(page)).paused).toBe(true);
  await context.setOffline(false);
  await panel(page).getByRole('button',{name:'Retry restore'}).click();
  await expect(panel(page).getByRole('button',{name:'Resume queue'})).toBeVisible();
  expect((await state(page)).position).toBeCloseTo(seeded.snapshot.position,1);
  const admin=await request.newContext({baseURL:f.origin});await admin.post('/api/login',{data:f.admin});
  const users=await(await admin.get('/api/admin/users')).json();const uid=users.find((u:any)=>u.username===f.user.username).id;
  expect((await admin.delete('/api/v1/admin/permissions',{data:{set_id:f.setIds.Music,user_id:uid}})).ok()).toBe(true);
  try {await page.reload();await page.keyboard.press('Q');await expect(panel(page)).toContainText('3 songs are no longer available');expect((await state(page)).entries).toHaveLength(0);}
  finally {await admin.post('/api/v1/admin/permissions',{data:{set_id:f.setIds.Music,user_id:uid,role:'viewer'}});await admin.dispose();}
  await page.addInitScript(key=>localStorage.setItem(key,'broken JSON'),queueKey);
  await page.reload();await page.keyboard.press('Q');await expect(panel(page)).toContainText('unreadable');
  const screenshot=e.path('m16-corrupt.png');await page.screenshot({path:screenshot,animations:'disabled'});artifacts.push(screenshot);
  e.record({feature:'M16',scenario:'real permission drops; initial transport fault then actual offline Retry preserves snapshot and silence; online Retry and corrupt recovery',result:'pass',artifacts});
 }catch(error){e.record({feature:'M16',scenario:'recovery boundaries',result:'fail',detail:String(error),artifacts});throw error;}
 finally {if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});

test('M16 recovered later duplicate Resume then normal Next uses following occurrence',async({page})=>{
 let pid='';const artifacts:string[]=[];
 try {
  const {id,a,b,snapshot}=await seedAndPlay(page);pid=id;
  await recovered(page,snapshot);
  await panel(page).getByRole('button',{name:'Resume queue',exact:true}).click();await expectPlaying(page,a.id);
  await page.locator('#btn-next').click();const runtime=await expectPlaying(page,b.id);
  const q=await state(page);expect(q.index).toBe(2);expect(q.entries[q.index].id).toBe(snapshot.entries[2].id);
  const output=await captureOutput(page,1300,e.path('m16-recovered-normal-next.wav'));assertValidCapture(output);
  const tone=mainSegment(output.analysis);expect(tone!.duration_s).toBeGreaterThan(.8);expect(Math.abs(tone!.tone_hz!-b.tone_hz[0])).toBeLessThan(5);artifacts.push(output.wav);
  e.record({feature:'M16',scenario:'supplemental recovered later occurrence Resume then normal Next selects following Long Road Home with runtime and original output',result:'pass',data:{snapshot,index:q.index,occurrence:q.entries[q.index].id,runtime,tone},artifacts});
 }catch(error){e.record({feature:'M16',scenario:'supplemental recovered normal Next',result:'fail',detail:String(error),artifacts});throw error;}
 finally {if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});
