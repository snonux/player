import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openSidebarMusic, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, resetProgress, playCard, openQueueMoreAction, openQueueRowMenuAction} from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let fixture:MusicFixture, evidence:Evidence;
const panel=(page:Page)=>page.locator('#queue-panel');
const rows=(page:Page)=>page.locator('#media-grid .media-row');
const order=(page:Page)=>panel(page).locator('.queue-track').evaluateAll(nodes=>nodes.map(n=>n.childNodes[0].textContent));
const songs=()=>['01 - First Light.mp3','02 - Long Road Home.mp3','03 - Harbour.flac'].map(name=>track(fixture,`Music/Aurora Lane - Night Transit (2021)/${name}`));
test.beforeAll(async({browser},info)=>{fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});});
test.beforeEach(async({page})=>{
 await page.addInitScript(installAudioTap);await keyboardLogin(page,fixture.user);
 await page.keyboard.press('Q');await expect(panel(page)).toHaveClass(/open/);
 await openQueueMoreAction(page,'Continue session');
 await expect.poll(async()=>page.evaluate(async()=>{const q=await import('/js/queue.js' as string);return q.queueState().status;})).toContain('Session continued');
 if(await panel(page).locator('.queue-row').count())await openQueueMoreAction(page,'Clear queue');
 await expect(panel(page).locator('.queue-row')).toHaveCount(0);
 await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();
});
async function album(page:Page, start=true) {
 await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
 await page.getByRole('button',{name:'Albums',exact:true}).click();
 if(await page.locator('#menu-close').isVisible())await page.locator('#menu-close').click();
 await page.getByRole('button',{name:'Open album Night Transit by Aurora Lane',exact:true}).click();
 if(start){await page.getByRole('button',{name:'▶ Play album',exact:true}).click();await expectPlaying(page,songs()[0].id);await page.locator('#btn-play').click();await page.keyboard.press('Escape');}
}
async function action(page:Page,title:string,name:string) {
 await page.getByRole('button',{name:`Actions for ${title}`,exact:true}).click();
 const menu=page.getByRole('group',{name:'Media actions',exact:true});await expect(menu).toBeVisible();
 await menu.getByRole('button',{name,exact:true}).click();
}
async function audible(page:Page,song:any,name:string){
 await expectPlaying(page,song.id);const capture=await captureOutput(page,1300,evidence.path(name+'.wav'));assertValidCapture(capture);const segment=mainSegment(capture.analysis);
 expect(segment!.duration_s).toBeGreaterThan(.8);expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
 return {media_id:song.id,state:await audioState(page),audio:segment,wav:capture.wav};
}

test('M14 single and selected insertion preserves FIFO, duplicates, position, Undo and keyboard focus',async({page})=>{
 const proof:any[]=[];const artifacts:string[]=[];
 try{
  const [a,b,c]=songs();for(const song of [a,b,c])await resetProgress(page,song.id);
  await album(page);const before=await audioState(page);
  // Choose in reverse click order; insertion uses displayed order.
  await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();await page.getByRole('checkbox',{name:'Select Long Road Home',exact:true}).check();
  await page.getByRole('button',{name:'Selected song actions',exact:true}).click();
  await page.getByRole('group',{name:'Selected song actions'}).getByRole('button',{name:'Play next',exact:true}).click();
  await expect(page.locator('[data-selection-count]')).toHaveText('2 selected');
  await page.getByRole('button',{name:'Clear selection',exact:true}).click();
  await rows(page).nth(2).focus();await page.keyboard.press('e');await expect(rows(page).nth(2)).toBeFocused();
  await rows(page).nth(0).focus();await page.keyboard.press('E');
  const after=await audioState(page);expect(after.src).toBe(before.src);expect(after.time).toBeCloseTo(before.time,1);expect(after.paused).toBe(true);
  await page.keyboard.press('Q');
  expect(await order(page)).toEqual(['First Light','Long Road Home','Harbour','Harbour','Long Road Home','Harbour','First Light']);
  const ids=await panel(page).locator('[data-entry-id]').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-entry-id')));expect(new Set(ids).size).toBe(7);
  expect(await panel(page).locator('.queue-row').filter({hasText:'Up next (you added)'}).count()).toBe(3);
  await page.keyboard.press('Escape');
  // The toast's real native Space activates Undo, preserving the active song.
  await page.locator('#toast').getByRole('button',{name:'Undo',exact:true}).focus();await page.keyboard.press('Space');
  expect((await audioState(page)).paused).toBe(true);await page.keyboard.press('Q');expect((await order(page)).length).toBe(6);await page.keyboard.press('Escape');
  await action(page,'Harbour','Play now');proof.push(await audible(page,c,'m14-play-now'));
  await page.locator('#toast').getByRole('button',{name:'Undo',exact:true}).click();
  await expect.poll(()=>audioState(page).then(s=>s.paused)).toBe(true);
  await page.keyboard.press('Q');expect(await order(page)).toEqual(['First Light','Long Road Home','Harbour','Harbour','Long Road Home','Harbour']);await page.keyboard.press('Escape');
  // Existing a menu and native typing remain available.
  await rows(page).nth(1).focus();await page.keyboard.press('a');await expect(page.getByRole('group',{name:'Media actions'})).toBeVisible();await page.keyboard.press('Escape');await expect(rows(page).nth(1)).toBeFocused();
  await page.keyboard.press('/');await page.getByRole('searchbox',{name:'Search media'}).fill('e E Q j k');await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('e E Q j k');await page.getByRole('searchbox',{name:'Search media'}).fill('');await page.keyboard.press('Escape');
  await page.keyboard.press('Q');expect((await order(page)).length).toBe(6);await page.keyboard.press('Escape');
  if((await panel(page).getAttribute('class')||'').includes('open'))await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();
  await album(page,false);await expect(page.locator('[data-selection-toolbar]')).toBeHidden();
  await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();
  await expect(page.locator('[data-selection-toolbar]')).toBeVisible();
  await expect(page.getByRole('button',{name:'Selected song actions',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Clear selection',exact:true}).click();
  await expect(page.locator('[data-selection-toolbar]')).toBeHidden();
  await albumScreens(page,artifacts);
  evidence.record({feature:'M14',scenario:'selected/single FIFO duplicate semantics and playback/Undo/focus/typing',result:'pass',data:{before,after,proof,ids},artifacts});
 }catch(error){evidence.record({feature:'M14',scenario:'insertion UI',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
});
async function albumScreens(page:Page,artifacts:string[]) {
 for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const){
  await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
  await expect(rows(page).first().getByRole('checkbox')).toBeVisible();await expect(rows(page).first().getByRole('button',{name:'Actions for First Light',exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  const shot=evidence.path(`m14-album-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:false});artifacts.push(shot);
 }
}

test('M14 repeated Play next actually advances FIFO with audible sources and finished IDs',async({page})=>{
 const proof:any[]=[];
 try{
  const [a,b,c]=songs();for(const song of [a,b,c])await resetProgress(page,song.id);
  await album(page);await action(page,'Harbour','Play next');
  await expect.poll(()=>page.evaluate(async()=>{const {queueState}=await import('/js/queue.js' as string);return queueState().entries.map((entry:any)=>entry.media.id);})).toEqual([a.id,c.id,b.id,c.id]);
  await action(page,'Long Road Home','Play next');
  await page.keyboard.press('Q');expect(await order(page)).toEqual(['First Light','Harbour','Long Road Home','Long Road Home','Harbour']);await page.keyboard.press('Escape');
  await rows(page).first().focus();await page.keyboard.press('p');proof.push(await audible(page,a,'m14-natural-a'));
  proof.push(await audible(page,c,'m14-natural-c'));proof.push(await audible(page,b,'m14-natural-b'));
  for(const song of [a,c]){const detail=await(await page.request.get(`/api/v1/media/${song.id}`)).json();expect(detail.progress.media_id).toBe(song.id);expect(detail.progress.finished).toBe(true);}
  await page.keyboard.press('Q');const firstB=await panel(page).locator('[aria-current="true"]').getAttribute('data-entry-id');await expect(panel(page).locator('[aria-current="true"]')).toHaveAttribute('data-queue-index','2');await page.keyboard.press('Escape');
  await page.keyboard.press('N');await expect.poll(()=>audioState(page).then(s=>s.time)).toBeLessThan(1.2);proof.push(await audible(page,b,'m14-duplicate-b'));
  await page.keyboard.press('Q');await expect(panel(page).locator('[aria-current="true"]')).toHaveAttribute('data-queue-index','3');expect(await panel(page).locator('[aria-current="true"]').getAttribute('data-entry-id')).not.toBe(firstB);
  evidence.record({feature:'M14',scenario:'natural FIFO A440 C659 B523, distinct duplicateB playback and authoritative finished IDs',result:'pass',data:{proof}});
 }catch(error){evidence.record({feature:'M14',scenario:'natural FIFO',result:'fail',detail:String(error),data:{proof}});throw error;}
});

test('M14 empty enqueue, durable queue, offline modules and reload',async({page})=>{
 const proof:any[]=[];
 try{
  await album(page,false);await action(page,'Harbour','Add to queue');await page.keyboard.press('Q');expect(await order(page)).toEqual(['Harbour']);expect((await audioState(page)).paused).toBe(true);await page.keyboard.press('Escape');
  await action(page,'First Light','Play next');await page.keyboard.press('Q');expect(await order(page)).toEqual(['Harbour','First Light']);await page.keyboard.press('Escape');
  await action(page,'Long Road Home','Play now');proof.push(await audible(page,songs()[1],'m14-empty-play-now'));
  await page.goto('/');await expect(page.getByRole('button',{name:'Toggle sets sidebar',exact:true})).toBeVisible();await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue restored · paused');expect(await order(page)).toEqual(['Harbour','Long Road Home','First Light']);await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();
  await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
  await expect.poll(()=>page.evaluate(async()=>!!(await caches.match('/js/queue-actions.js')))).toBe(true);
  await page.context().setOffline(true);await page.reload();await expect(page.getByRole('button',{name:'Toggle sets sidebar',exact:true})).toBeVisible();await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue is empty');
  evidence.record({feature:'M14',scenario:'empty enqueue paused, Play now audible, durable online queue and actual offline cached module import',result:'pass',data:{proof},artifacts:[evidence.path('m14-offline.png')]});await page.screenshot({path:evidence.path('m14-offline.png')});
 }catch(error){evidence.record({feature:'M14',scenario:'empty/offline',result:'fail',detail:String(error),data:{proof}});throw error;}finally{await page.context().setOffline(false);}
});


test('M14 Sets audio actions retain current source; real permission denial recovers explicit inserted occurrence',async({page})=>{
 const proof:any[]=[];const admin=await request.newContext({baseURL:fixture.origin});let userId=0;
 try{
  await album(page);
  const set=page.locator('#set-list').getByRole('button',{name:'Set Music',exact:true});
  if(!await set.isVisible())await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
  await set.click();if(await page.locator('#menu-close').isVisible())await page.locator('#menu-close').click();
  await page.getByRole('button',{name:'Folder Various Artists - Synth Compilation',exact:true}).click();await page.getByRole('button',{name:'Folder Disc 1',exact:true}).click();
  const source=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a');
  await resetProgress(page,source.id);await playCard(page,source.title,source.id);
  if((await page.locator('#player').getAttribute('class')||'').includes('minimized'))await page.getByRole('button',{name:'Restore player',exact:true}).click();
  await page.locator('#btn-play').click();await page.keyboard.press('Escape');
  const card=page.locator(`#media-grid [data-id="${source.id}"]`);await card.focus();const before=await audioState(page);
  await page.keyboard.press('e');await page.keyboard.press('E');
  expect((await audioState(page)).src).toBe(before.src);expect((await audioState(page)).time).toBeCloseTo(before.time,1);expect((await audioState(page)).paused).toBe(true);
  await page.keyboard.press('a');const menu=page.getByRole('group',{name:'Media actions'});await expect(menu.getByRole('button',{name:'Play now',exact:true})).toBeVisible();await page.keyboard.press('Escape');
  if(!(await panel(page).getAttribute('class')||'').includes('open'))await page.getByRole('button',{name:'Queue',exact:true}).first().click();await expect(panel(page)).toHaveClass(/open/);expect((await order(page)).filter(title=>title===source.title).length).toBe(2);await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();await expect(panel(page)).not.toHaveClass(/open/);
  await admin.post('/api/v1/auth/login',{data:fixture.admin});userId=(await(await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===fixture.user.username).id;
  const target=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus');
  const targetCard=page.locator(`#media-grid [data-id="${target.id}"]`);
  await expect.poll(async()=>page.evaluate(async()=> (await import('/js/queue.js' as string)).queueState().loading)).toBe(false);
  await targetCard.getByRole('button',{name:target.title,exact:true}).focus();
  await expect(targetCard).toHaveClass(/selected/);
  await page.keyboard.press('a');await page.getByRole('group',{name:'Media actions'}).getByRole('button',{name:'Play next',exact:true}).click();
  await expect.poll(async()=>page.evaluate(async id=>(await import('/js/queue.js' as string)).queueState().entries.some((entry:any)=>entry.media.id===id),target.id)).toBe(true);
  expect((await admin.delete('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId}})).ok()).toBe(true);
  expect((await page.request.get(`/api/v1/media/${target.id}`)).status()).toBe(403);
  const denied=page.waitForResponse(r=>r.url().includes(`/media/${target.id}`)&&r.status()===403);
  if(!(await panel(page).getAttribute('class')||'').includes('open'))await page.getByRole('button',{name:'Queue',exact:true}).first().click();
  await panel(page).getByRole('button',{name:`Play ${target.title}`,exact:true}).click();await denied;
  await expect(page.locator('#toast')).toContainText('Cannot play');await page.screenshot({path:evidence.path('m14-real-denied.png')});
  await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});
  if(!(await panel(page).getAttribute('class')||'').includes('open'))await page.getByRole('button',{name:'Queue',exact:true}).first().click();await expect(panel(page)).toHaveClass(/open/);const index=await panel(page).locator('.queue-row[aria-current="true"]').getAttribute('data-queue-index');expect(index).not.toBeNull();
  await panel(page).locator(`[data-queue-index="${index}"]`).getByRole('button',{name:`Play ${target.title}`,exact:true}).click();proof.push(await audible(page,target,'m14-real-retry'));
  evidence.record({feature:'M14',scenario:'Sets e/E/a retain source and explicit inserted real ACL denial/retry',result:'pass',data:{before,proof},artifacts:[evidence.path('m14-real-denied.png')]});
 }catch(error){evidence.record({feature:'M14',scenario:'Sets and permission',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally{if(userId)await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});await admin.dispose();}
});

test('M14 long song action labels and selection remain usable in narrow themes',async({page})=>{
 const artifacts:string[]=[];
 try{
  const catalog=await(await page.request.get('/api/v1/music/albums')).json();const long=catalog.find((a:any)=>a.title.startsWith('Long Album '));
  await openSidebarMusic(page,'Albums');await page.locator(`[data-album="${long.album_key}"]`).click();
  await page.setViewportSize({width:390,height:844});
  for(const theme of ['dark','light']){
   if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await rows(page).first().getByRole('checkbox').check();await rows(page).first().getByRole('button',{name:/Actions for/}).click();
   const menu=page.getByRole('group',{name:'Media actions'});await expect(menu.getByRole('button',{name:'Play next',exact:true})).toBeVisible();
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);const shot=evidence.path(`m14-long-${theme}-390.png`);await page.screenshot({path:shot,fullPage:false});artifacts.push(shot);
   await menu.getByRole('button',{name:'Play next',exact:true}).click();await expect(rows(page).first()).toBeFocused();
  }
  evidence.record({feature:'M14',scenario:'long title/artist action menu and checked selection narrow light/dark',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M14',scenario:'long labels',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M14 batch Append and Play now preserve source, displayed order and retained remainder',async({page})=>{
 const proof:any[]=[];
 const show=async()=>{if(!(await panel(page).getAttribute('class')||'').includes('open'))await page.getByRole('button',{name:'Queue',exact:true}).first().click();await expect(panel(page)).toHaveClass(/open/);};
 const close=async()=>{await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();await expect(panel(page)).not.toHaveClass(/open/);};
 try{
  const [a,b,c]=songs();for(const song of [a,b,c])await resetProgress(page,song.id);
  await album(page);await show();await openQueueRowMenuAction(page,2,/Remove Harbour/);
  expect(await order(page)).toEqual(['First Light','Long Road Home']);const original=await panel(page).locator('[data-entry-id]').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-entry-id')));await close();
  await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();await page.getByRole('checkbox',{name:'Select First Light',exact:true}).check();
  await rows(page).first().focus();await page.keyboard.press('p');await expectPlaying(page,a.id);const before=await audioState(page);
  await page.getByRole('button',{name:'Selected song actions',exact:true}).click();await page.getByRole('group',{name:'Selected song actions'}).getByRole('button',{name:'Add to queue',exact:true}).click();
  const afterAppend=await audioState(page);expect(afterAppend.src).toBe(before.src);expect(afterAppend.time).toBeGreaterThanOrEqual(before.time-.05);expect(afterAppend.time).toBeLessThan(before.time+2);expect(afterAppend.paused).toBe(false);
  const appendAudio=await captureOutput(page,900,evidence.path('m14-batch-append-current-a.wav'));assertValidCapture(appendAudio);const appendSegment=mainSegment(appendAudio.analysis);expect(appendSegment!.duration_s).toBeGreaterThan(.5);expect(Math.abs(appendSegment!.tone_hz!-a.tone_hz[0])).toBeLessThan(5);const continued=await audioState(page);expect(continued.src).toBe(before.src);expect(continued.paused).toBe(false);expect(continued.time).toBeGreaterThan(before.time+.5);await page.keyboard.press('p');await expect.poll(()=>audioState(page).then(s=>s.paused)).toBe(true);
  await show();expect(await order(page)).toEqual(['First Light','Long Road Home','First Light','Harbour']);
  const appended=await panel(page).locator('[data-entry-id]').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-entry-id')));expect(appended.slice(0,2)).toEqual(original);await close();
  await page.getByRole('button',{name:'Clear selection',exact:true}).click();await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();await page.getByRole('checkbox',{name:'Select Long Road Home',exact:true}).check();
  await page.getByRole('button',{name:'Selected song actions',exact:true}).click();await page.getByRole('group',{name:'Selected song actions'}).getByRole('button',{name:'Play now',exact:true}).click();
  proof.push(await audible(page,b,'m14-batch-play-now-first-b'));
  await show();expect(await order(page)).toEqual(['First Light','Long Road Home','Harbour','Long Road Home','First Light','Harbour']);await expect(panel(page).locator('[aria-current="true"]')).toHaveAttribute('data-queue-index','1');
  const inserted=await panel(page).locator('[data-entry-id]').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-entry-id')));expect([inserted[0],...inserted.slice(3)]).toEqual(appended);expect(new Set(inserted).size).toBe(6);
  const firstShot=evidence.path('m14-batch-play-now-first.png');await page.screenshot({path:firstShot});await close();
  await page.keyboard.press('N');proof.push(await audible(page,c,'m14-batch-play-now-second-c'));
  await show();await expect(panel(page).locator('[aria-current="true"]')).toHaveAttribute('data-queue-index','2');expect(await order(page)).toEqual(['First Light','Long Road Home','Harbour','Long Road Home','First Light','Harbour']);const secondShot=evidence.path('m14-batch-play-now-second.png');await page.screenshot({path:secondShot});await close();await expect.poll(()=>audioState(page).then(s=>s.time)).toBeGreaterThan(3.5);await page.keyboard.press('p');
  const saved=[];for(const song of [a,b,c]){await expect.poll(async()=>{const d=await(await page.request.get(`/api/v1/media/${song.id}`)).json();return d.progress?.position_seconds||0;}).toBeGreaterThan(1);const d=await(await page.request.get(`/api/v1/media/${song.id}`)).json();expect(d.progress.user_id).toBe(2);expect(d.progress.media_id).toBe(song.id);saved.push(d.progress);}
  evidence.record({feature:'M14',scenario:'batch Append preserves playing source/order; batch Play now inserts contiguous B,C and retains remainder with actual B523/C659 and viewer IDs',result:'pass',data:{before,afterAppend,continued,appendAudio:appendSegment,appendWav:appendAudio.wav,original,appended,inserted,proof,saved},artifacts:[firstShot,secondShot]});
 }catch(error){evidence.record({feature:'M14',scenario:'batch Append/Play now',result:'fail',detail:String(error),data:{proof}});throw error;}
});
