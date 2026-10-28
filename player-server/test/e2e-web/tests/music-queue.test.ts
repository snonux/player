import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, playCard, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, resetProgress , openSidebarMusic, openQueueMoreAction, openQueueRowMenuAction} from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let fixture: MusicFixture, evidence: Evidence;
const folder = 'Aurora Lane - Night Transit (2021)';
const panel = (page: Page) => page.locator('#queue-panel');
const row = (page: Page, index: number) => panel(page).locator(`[data-queue-index="${index}"]`).first();
const titles = (page: Page) => panel(page).locator('.queue-track').evaluateAll(nodes => nodes.map(n => n.childNodes[0].textContent));
const songs = () => ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac'].map(name => track(fixture, `Music/${folder}/${name}`));
test.beforeAll(async ({ browser }, info) => {
 fixture = loadFixture(); evidence = new Evidence(fixture.evidenceDir, info.project.name, {browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});
});
test.beforeEach(async ({page}) => { await page.addInitScript(installAudioTap); await keyboardLogin(page,fixture.user); });
async function album(page: Page) {
 if ((await page.locator('#player').getAttribute('class') || '').includes('open')) await page.keyboard.press('Escape');
 await openSidebarMusic(page,'Albums');
 await page.getByRole('button',{name:'Open album Night Transit by Aurora Lane',exact:true}).click();
 await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
 await expectPlaying(page,songs()[0].id);
 // Twelve-second fixtures must pause before slower keyboard/UIAutomator edits.
 await page.locator('#btn-play').click();
 await expect.poll(async () => (await audioState(page)).paused).toBe(true);
}
async function audible(page: Page, song: any, name: string) {
 await expectPlaying(page,song.id);
 const capture=await captureOutput(page,1300,evidence.path(name+'.wav'));
 assertValidCapture(capture); const segment=mainSegment(capture.analysis);
 expect(segment?.duration_s).toBeGreaterThan(.8);
 expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
 return {media_id:song.id,state:await audioState(page),audio:segment,wav:capture.wav};
}

test('M13 queue keyboard and buttons preserve entry/focus, source edits and Undo while browsing',async({page})=>{
 const artifacts:string[]=[], proof:any[]=[];
 try {
  const [a,b,c]=songs(); for(const song of [a,b,c]) await resetProgress(page,song.id);
  await album(page); await page.keyboard.press('Q');
  await expect(row(page,0)).toBeFocused(); await expect(row(page,0)).toHaveAttribute('aria-current','true');
  const activeEntry=await row(page,0).getAttribute('data-entry-id');
  await page.keyboard.press('j');
  const movedEntry=await row(page,1).getAttribute('data-entry-id');
  await page.keyboard.press('J');
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  await expect(row(page,2)).toBeFocused();
  await expect(row(page,2)).toHaveAttribute('data-entry-id',movedEntry!);
  await page.keyboard.press('K'); await expect(row(page,1)).toBeFocused();
  expect(await titles(page)).toEqual(['First Light','Long Road Home','Harbour']);
  await expect(row(page,1)).toHaveAttribute('data-entry-id',movedEntry!);
  // Move the same entry down again for the remaining edited-order journey.
  await page.keyboard.press('J');
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  await expect(row(page,2)).toBeFocused();
  await expect(row(page,2)).toHaveAttribute('data-entry-id',movedEntry!);
  await row(page,1).getByRole('button',{name:/Actions for Harbour/}).focus();
  await page.keyboard.press('Space');
  await expect(page.getByRole('menu',{name:'Queue track actions'})).toBeVisible();
  await page.keyboard.press('Escape');
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  await expect(row(page,1)).toBeFocused();
  await page.keyboard.press('j');
  await expect(row(page,2)).toBeFocused();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu',{name:'Queue track actions'})).toBeVisible();
  await expect(page.getByRole('menu',{name:'Queue track actions'}).getByRole('button',{name:'Move Long Road Home down',exact:true})).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(row(page,2)).toBeFocused();
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  expect(await row(page,0).getAttribute('data-entry-id')).toBe(activeEntry);
  await row(page,2).getByRole('button',{name:/Actions for Long Road Home/}).focus();
  await page.keyboard.press('Escape'); await expect(panel(page)).not.toHaveClass(/open/);
  await expect(page.locator('#btn-play')).toBeFocused();
  await page.keyboard.press('Q');
  await openQueueRowMenuAction(page,2,/Remove Long Road Home/);
  expect(await titles(page)).toEqual(['First Light','Harbour']);
  await openQueueMoreAction(page,'Undo');
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  // Desktop library remains navigable beside the queue; typing must stay text.
  await page.keyboard.press('Escape'); await page.keyboard.press('Escape'); await page.keyboard.press('/');
  await page.getByRole('searchbox',{name:'Search media'}).fill('nothing J K Delete Q');
  await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('nothing J K Delete Q');
  expect((await audioState(page)).src).toContain(`/media/${a.id}/`);
  await page.keyboard.press('Escape'); await page.keyboard.press('Q');
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
   await page.setViewportSize({width,height:844});
   if(await page.locator('html').getAttribute('data-theme')!==theme) {
    if(width<700) await panel(page).getByRole('button',{name:'Close queue'}).click();
    await page.getByRole('button',{name:'More options',exact:true}).click();
    await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    if(width<700) await page.keyboard.press('Q');
   }
   await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
   const shot=evidence.path(`m13-queue-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   expect(await row(page,0).getByRole('button',{name:/Actions for First Light/}).isVisible()).toBe(true);
   expect(await row(page,0).locator('[data-queue-action="remove"]').isHidden()).toBe(true);
  }
  await row(page,1).getByRole('button',{name:'Play Harbour',exact:true}).click();proof.push(await audible(page,c,'m13-jump-harbour'));artifacts.push(proof.at(-1).wav);
  await openQueueRowMenuAction(page,1,/Remove Harbour/);
  await expect.poll(async()=> (await audioState(page)).paused).toBe(true);
  expect(await titles(page)).toEqual(['First Light','Long Road Home']);
  await openQueueMoreAction(page,'Undo');
  expect((await audioState(page)).paused).toBe(true);
  await openQueueMoreAction(page,'Clear queue');
  await expect(panel(page)).toContainText('Queue is empty');
  await openQueueMoreAction(page,'Undo');
  expect((await audioState(page)).paused).toBe(true);
  await row(page,2).getByRole('button',{name:'Play Long Road Home',exact:true}).click();proof.push(await audible(page,b,'m13-clear-undo-explicit-resume'));artifacts.push(proof.at(-1).wav);
  evidence.record({feature:'M13',scenario:'keyboard/pointer/focus/typing/themes/current removal/Clear Undo/explicit jump',result:'pass',data:{proof},artifacts});
 } catch(error) {evidence.record({feature:'M13',scenario:'editable queue UI/source journey',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
});

test('M13 edited natural source order is independent of browsing and the Next button uses edited order',async({page})=>{
 const proof:any[]=[], artifacts:string[]=[];
 try {
  const [a,b,c]=songs();for(const song of [a,b,c]) await resetProgress(page,song.id);
  await album(page);await page.keyboard.press('Q');await page.keyboard.press('j');await page.keyboard.press('J');
  expect(await titles(page)).toEqual(['First Light','Harbour','Long Road Home']);
  await page.keyboard.press('Escape');
  await page.locator('#btn-next').click();proof.push(await audible(page,c,'m13-edited-next'));artifacts.push(proof.at(-1).wav);
  await page.locator('#btn-play').click();await page.keyboard.press('Q');
  await row(page,0).getByRole('button',{name:'Play First Light',exact:true}).click();
  proof.push(await audible(page,a,'m13-natural-first'));artifacts.push(proof.at(-1).wav);
  await page.keyboard.press('Escape');await page.keyboard.press('Escape');
  await openSidebarMusic(page,'Albums');
  await page.getByRole('button',{name:'Open album Synth Compilation by Various Artists',exact:true}).click();
  proof.push(await audible(page,c,'m13-natural-second'));artifacts.push(proof.at(-1).wav);
  proof.push(await audible(page,b,'m13-natural-third'));artifacts.push(proof.at(-1).wav);
  await expect.poll(async()=>{const s=await audioState(page);return s.paused && s.time>10;},{timeout:20000}).toBe(true);
  for(const song of [a,b,c]) expect((await(await page.request.get(`/api/v1/media/${song.id}`)).json()).progress?.finished).toBe(true);
  evidence.record({feature:'M13',scenario:'edited Next and three natural tones/IDs/end while browsing elsewhere',result:'pass',data:{proof},artifacts});
 }catch(error){evidence.record({feature:'M13',scenario:'edited actual source order',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
});

test('M13 retained queue cannot edit unrelated Sets source; real ACL denial and offline edits recover',async({page})=>{
 const artifacts:string[]=[], proof:any[]=[];const admin=await request.newContext({baseURL:fixture.origin});let userId=0;
 try {
  const [a,b]=songs();await album(page);
  await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
  await page.locator('#set-list').getByRole('button',{name:'Set Music',exact:true}).click();
  await page.locator('#menu-close').click();
  await page.getByRole('button',{name:'Folder Various Artists - Synth Compilation',exact:true}).click();
  await page.getByRole('button',{name:'Folder Disc 1',exact:true}).click();
  const source=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a');
  const next=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus');
  await resetProgress(page,source.id);await resetProgress(page,next.id);await playCard(page,source.title,source.id);await expectPlaying(page,source.id);
  if((await page.locator('#player').getAttribute('class')||'').includes('minimized')) await page.getByRole('button',{name:'Restore player',exact:true}).click();
  await page.locator('#btn-play').click();
  await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue paused');
  await openQueueRowMenuAction(page,0,/Remove First Light/);await openQueueMoreAction(page,'Undo');
  await openQueueMoreAction(page,'Clear queue');await openQueueMoreAction(page,'Undo');
  expect((await audioState(page)).src).toContain(`/media/${source.id}/`);
  await page.keyboard.press('Escape');await page.locator('#btn-next').click();proof.push(await audible(page,next,'m13-retained-sets-next'));artifacts.push(proof.at(-1).wav);await page.locator('#btn-play').click();
  await page.keyboard.press('Q');await row(page,0).getByRole('button',{name:'Play First Light',exact:true}).click();proof.push(await audible(page,a,'m13-retained-queue-reclaim'));artifacts.push(proof.at(-1).wav);await page.locator('#btn-play').click();
  await admin.post('/api/v1/auth/login',{data:fixture.admin});userId=(await(await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===fixture.user.username).id;
  await admin.delete('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId}});
  const denied=page.waitForResponse(r=>r.url().includes(`/media/${b.id}/stream`) && r.status()===403);
  await row(page,1).getByRole('button',{name:'Play Long Road Home',exact:true}).click();await denied;
  await expect(page.locator('#toast')).toContainText('Cannot play');
  const shot=evidence.path('m13-real-acl-denied-jump.png');await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});
  await row(page,1).getByRole('button',{name:'Play Long Road Home',exact:true}).click();proof.push(await audible(page,b,'m13-permission-retry'));artifacts.push(proof.at(-1).wav);await page.locator('#btn-play').click();
  await page.context().setOffline(true);
  await openQueueRowMenuAction(page,2,/Remove Harbour/);await openQueueMoreAction(page,'Undo');
  expect(await titles(page)).toEqual(['First Light','Long Road Home','Harbour']);
  await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
  // History-only query URLs were never fetched; the existing PWA caches exact
  // URLs. Exercise its actual precached root shell and the new module imports.
  await page.goto('/');await page.keyboard.press('Q');await expect(panel(page)).toContainText('Queue is empty');
  await page.context().setOffline(false);
  evidence.record({feature:'M13',scenario:'retained inactive ownership/Sets Next/explicit reclaim/real ACL error retry/offline edits/reload empty',result:'pass',data:{proof},artifacts});
 }catch(error){evidence.record({feature:'M13',scenario:'retained ownership and error boundaries',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
 finally{await page.context().setOffline(false);if(userId)await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});await admin.dispose();}
});

test('M13 1001 actual library entries remain reachable with bounded queue rendering',async({page})=>{
 test.skip(!process.env.PLAYER_QUEUE_SCALE,'Run music_queue_scale_fixture.py and set PLAYER_QUEUE_SCALE=1');
 const artifacts:string[]=[];
 try {
  const catalog=await(await page.request.get('/api/v1/music/albums')).json();const scale=catalog.find((a:any)=>a.title==='Queue Scale');
  expect(scale.song_count).toBe(1001);const detail=await(await page.request.get(`/api/v1/music/albums/${scale.album_key}`)).json();
  await openSidebarMusic(page,'Albums');
  await page.getByRole('button',{name:'Open album Queue Scale by Queue Scale',exact:true}).click();await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
  await expectPlaying(page,detail.songs[0].id);await page.locator('#btn-play').click();await page.keyboard.press('Q');
  expect(await panel(page).locator('.queue-row').count()).toBe(200);
  for(let i=0;i<5;i++)await panel(page).getByRole('button',{name:'Next page',exact:true}).click();
  await expect(panel(page)).toContainText('Showing 1001–1001 of 1001');expect(await panel(page).locator('.queue-row').count()).toBe(1);
  await panel(page).getByRole('button',{name:'Play Queue 1001',exact:true}).click();
  const proof=await audible(page,{id:detail.songs.at(-1).id,tone_hz:[440]},'m13-large-last-entry');artifacts.push(proof.wav);
  const shot=evidence.path('m13-1001-entry-queue-last.png');await page.screenshot({path:shot,fullPage:false,animations:'disabled'});artifacts.push(shot);
  evidence.record({feature:'M13',scenario:'actual1001-entry album, bounded200-row pages, last-entry audible ID jump',result:'pass',data:{proof,count:1001},artifacts});
 }catch(error){evidence.record({feature:'M13',scenario:'actual1001-entry queue',result:'fail',detail:String(error),artifacts});throw error;}
});


test('M13 long title and artist remain usable in narrow queue themes',async({page})=>{
 const artifacts:string[]=[];
 try {
  const catalog=await(await page.request.get('/api/v1/music/albums')).json();const long=catalog.find((a:any)=>a.title.startsWith('Long Album '));
  const detail=await(await page.request.get(`/api/v1/music/albums/${long.album_key}`)).json();
  await openSidebarMusic(page,'Albums');
  await page.locator(`[data-album="${long.album_key}"]`).click();await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
  await expectPlaying(page,detail.songs[0].id);await page.locator('#btn-play').click();await page.setViewportSize({width:390,height:844});
  for(const theme of ['dark','light']) {
   if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await page.keyboard.press('Q');await expect(panel(page)).toContainText(long.title);
   expect(await row(page,0).getByRole('button',{name:'Remove '+detail.songs[0].music.title,exact:true}).isVisible()).toBe(true);
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const shot=evidence.path(`m13-long-queue-${theme}-390.png`);await page.screenshot({path:shot,fullPage:false,animations:'disabled'});artifacts.push(shot);
   await panel(page).getByRole('button',{name:'Close queue',exact:true}).click();
  }
  evidence.record({feature:'M13',scenario:'long title/artist narrow queue dark and light',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M13',scenario:'long queue content',result:'fail',detail:String(error),artifacts});throw error;}
});
