import { test, expect, request } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, selectByKeys, expectPlaying, captureOutput, assertValidCapture, mainSegment , openSidebarMusic} from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let f:MusicFixture,e:Evidence;
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,f.user);await page.goto('/?music=playlists');await expect(page.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();});
const modal='#playlist-modal';

test('M26 create add album rename comment cold client persistence account scope artwork audio delete',async({page,browser})=>{
 const artifacts:string[]=[];
 try{
  const name='Road Trip '+Date.now();
  await page.locator('#media-grid [data-create]').click();
  await page.getByRole('textbox',{name:'Name',exact:true}).fill(name);
  await page.getByRole('textbox',{name:'Comment',exact:true}).fill('r/h/l/? plain <text> 東京');
  await page.locator(modal).getByRole('button',{name:'Create playlist',exact:true}).click();
  await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
  await expect(page.getByText('This playlist is empty.',{exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'▶ Play playlist',exact:true})).toBeDisabled();
  const url=page.url(),id=new URL(url).searchParams.get('id')!;
  await page.getByRole('button',{name:'Add music',exact:true}).click();
  const album=(await (await page.request.get('/api/v1/music/albums')).json()).find((a:any)=>a.title==='Night Transit');
  expect(album.cover_media_id).toBeGreaterThan(0);
  await page.locator(modal).getByRole('button',{name:'Night Transit — Aurora Lane',exact:true}).click();
  const saved=await (await page.request.get('/api/v1/playlists/'+id)).json();
  expect(saved.song_count).toBe(album.song_count);expect(saved.duration).toBeCloseTo(album.duration,4);expect(saved.cover_media_id).toBe(album.cover_media_id);
  await expect(page.locator('.album-header img')).toBeVisible();
  await expect.poll(()=>page.locator('.album-header img').evaluate((img:HTMLImageElement)=>img.complete&&img.naturalWidth>0)).toBe(true);
  await expect(page.locator('.album-header')).toContainText(`${album.song_count} songs`);
  await page.getByRole('button',{name:'Edit playlist',exact:true}).click();
  const renamed=name+' renamed';
  await page.getByRole('textbox',{name:'Name',exact:true}).fill(renamed);
  await page.getByRole('textbox',{name:'Comment',exact:true}).fill('Saved across restart\n東京 <plain text>');
  await page.getByRole('textbox',{name:'Comment',exact:true}).press('r');
  await expect(page.getByRole('textbox',{name:'Comment',exact:true})).toHaveValue('Saved across restart\n東京 <plain text>r');
  await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(page.getByRole('heading',{name:renamed,exact:true})).toBeVisible();
  const state=await page.context().storageState();
  const cold=await browser.newContext({storageState:state});const restored=await cold.newPage();
  await restored.goto(url);await expect(restored.getByRole('heading',{name:renamed,exact:true})).toBeVisible();
  await expect(restored.locator('.playlist-comment')).toHaveText('Saved across restart\n東京 <plain text>r');
  await expect.poll(()=>restored.locator('.album-header img').evaluate((img:HTMLImageElement)=>img.complete&&img.naturalWidth>0)).toBe(true);
  await cold.close();
  const admin=await request.newContext({baseURL:f.origin});await admin.post('/api/v1/auth/login',{data:f.admin});
  const outsider={username:`m26-outsider-${Date.now()}`,password:randomBytes(18).toString('hex')};
  const created=await admin.post('/api/v1/admin/users',{data:{...outsider,is_admin:false}});expect(created.ok()).toBe(true);
  const other=await browser.newContext();
  try{const otherPage=await other.newPage();await keyboardLogin(otherPage,outsider);await otherPage.goto('/?music=playlists');await expect(otherPage.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();await expect(otherPage.getByRole('button',{name:'Open playlist '+renamed,exact:true})).toHaveCount(0);
   const otherApi=await request.newContext({baseURL:f.origin});try{await otherApi.post('/api/v1/auth/login',{data:outsider});expect((await otherApi.get('/api/v1/playlists/'+id)).status()).toBe(404);expect((await otherApi.patch('/api/v1/playlists/'+id,{data:{name:'stolen',comment:''}})).status()).toBe(404);expect((await(await otherApi.get('/api/v1/playlists')).json()).some((p:any)=>p.playlist_id===id)).toBe(false);}finally{await otherApi.dispose();}
  }finally{await other.close();await admin.delete('/api/v1/admin/users/'+(await created.json()).id);await admin.dispose();}
  await page.reload();await expect(page.getByRole('heading',{name:renamed,exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');await expect(page.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();await selectByKeys(page,'Open playlist '+renamed,100);await page.keyboard.press('Enter');await expect(page.getByRole('heading',{name:renamed,exact:true})).toBeVisible();
  for(const [theme,width]of [['dark',1366],['light',1366],['dark',390],['light',390]]as const){
   await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   await expect.poll(()=>page.locator('#media-grid [data-edit]').evaluate(el=>getComputedStyle(el).backgroundColor)).toBe(theme==='light'?'rgb(255, 255, 255)':'rgb(30, 34, 44)');
   const shot=e.path(`m26-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  }
  await page.locator('.media-row').first().focus();await page.keyboard.press('j');await page.keyboard.press('k');await expect(page.locator('.media-row').first()).toHaveClass(/selected/);await page.keyboard.press('Enter');await expectPlaying(page,saved.songs[0].id);
  const capture=await captureOutput(page,1400,e.path('m26-source.wav'));assertValidCapture(capture);const segment=mainSegment(capture.analysis);
  const expected=f.tracks.find(t=>f.mediaIds[t.path]===saved.songs[0].id)!;expect(Math.abs(segment!.tone_hz!-expected.tone_hz[0])).toBeLessThan(5);artifacts.push(capture.wav);
  await page.keyboard.press('q');await expect(page.locator('#player')).not.toHaveClass(/open/);
  await page.getByRole('button',{name:'Add music',exact:true}).click();await page.locator(modal).getByRole('button',{name:'Night Transit — Aurora Lane',exact:true}).click();
  await expect.poll(async()=>(await(await page.request.get('/api/v1/playlists/'+id)).json()).song_count).toBe(2*album.song_count);
  const duplicated=await (await page.request.get('/api/v1/playlists/'+id)).json();expect(new Set(duplicated.entries.map((entry:any)=>entry.entry_id)).size).toBe(duplicated.song_count);
  await page.locator('.media-row').nth(3).locator('.song-open').click();await expectPlaying(page,saved.songs[0].id);await page.keyboard.press('Q');
  await expect(page.locator('#queue-panel [aria-current=true]')).toHaveAttribute('data-queue-index','3');
  await page.getByRole('button',{name:'Close queue',exact:true}).click();await page.keyboard.press('N');await expectPlaying(page,saved.songs[1].id);await page.keyboard.press('Q');
  await expect(page.locator('#queue-panel [aria-current=true]')).toHaveAttribute('data-queue-index','4');await page.getByRole('button',{name:'Close queue',exact:true}).click();
  await page.keyboard.press('q');
  await page.locator('#media-grid [data-delete]').click();await page.getByRole('button',{name:'Cancel',exact:true}).click();
  await expect(page.locator('#media-grid [data-delete]')).toBeFocused();
  expect((await page.request.get('/api/v1/playlists/'+id)).status()).toBe(200);
  await page.locator('#media-grid [data-delete]').click();await page.locator(modal).getByRole('button',{name:'Delete playlist',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();expect((await page.request.get('/api/v1/playlists/'+id)).status()).toBe(404);
  e.record({feature:'M26',scenario:'persistent CRUD comment account scope visible totals decoded artwork original source and client restart',result:'pass',data:{album,saved,duplicated,audio:segment},artifacts});
 }catch(error){e.record({feature:'M26',scenario:'persistent playlist journey',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M26 native typing offline retained form and delayed old dialog completions cannot affect new dialog',async({page,context})=>{
 await page.locator('#media-grid [data-create]').click();
 await page.getByRole('textbox',{name:'Name',exact:true}).fill('Offline retained');
 await page.getByRole('textbox',{name:'Comment',exact:true}).fill('r/h/l/?');
 await context.setOffline(true);await page.locator(modal).getByRole('button',{name:'Create playlist',exact:true}).click();
 await expect(page.locator(modal).getByRole('alert')).not.toBeEmpty();await expect(page.getByRole('textbox',{name:'Comment',exact:true})).toHaveValue('r/h/l/?');
 await context.setOffline(false);await page.getByRole('button',{name:'Cancel',exact:true}).click();
 // Delay both success and failure responses after a real save request. Closing
 // and reopening on the same route must leave the new form's input/error intact.
 for(const success of [true,false]){
  let release!:()=>void;const pending=new Promise<void>(resolve=>release=resolve);let entered!:()=>void;const hit=new Promise<void>(resolve=>entered=resolve);
  await page.route('**/api/playlists',async route=>{if(route.request().method()!=='POST'){await route.continue();return;}entered();await pending;await route.fulfill({status:success?201:500,contentType:'application/json',body:JSON.stringify(success?{playlist_id:'pl_delayed'}:{error:'old failure'})});});
  await page.locator('#media-grid [data-create]').click();await page.getByRole('textbox',{name:'Name',exact:true}).fill('Old request');
  await page.locator(modal).getByRole('button',{name:'Create playlist',exact:true}).click();await hit;
  await page.getByRole('button',{name:'Close playlist dialog',exact:true}).click();
  await page.locator('#media-grid [data-create]').click();await page.getByRole('textbox',{name:'Name',exact:true}).fill('New independent form');
  release();await page.waitForTimeout(200);await expect(page.getByRole('textbox',{name:'Name',exact:true})).toHaveValue('New independent form');await expect(page.locator(modal).getByRole('alert')).toBeEmpty();await expect(page.locator(modal)).toHaveClass(/open/);
  await page.unroute('**/api/playlists');await page.getByRole('button',{name:'Cancel',exact:true}).click();
 }
 e.record({feature:'M26',scenario:'offline input retention and old dialog success/failure ownership isolation',result:'pass'});
});

test('M26 cached app shell imports playlist module and offers actual offline error Retry',async({page,context})=>{
 const errors:string[]=[];page.on('pageerror',error=>errors.push(String(error)));
 await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
 await expect.poll(()=>page.evaluate(()=>Boolean(navigator.serviceWorker.controller))).toBe(true);
 await page.reload();await expect(page.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();
 await expect.poll(()=>page.evaluate(async()=>Boolean(await caches.match('/js/views/music-playlists.js')))).toBe(true);
 await expect.poll(()=>page.evaluate(async()=>Boolean(await caches.match('/?music=playlists')))).toBe(true);
 await context.setOffline(true);await page.reload();
 await expect(page.locator('#toast')).toHaveClass(/error/);
 await openSidebarMusic(page,'Playlists');
 await expect(page.locator('#media-grid [role=alert]')).toContainText('Could not load playlists');
 const shot=e.path('m26-cached-offline.png');await page.screenshot({path:shot,fullPage:true,animations:'disabled'});
 await context.setOffline(false);await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(page.getByRole('heading',{name:'Playlists',exact:true})).toBeVisible();
 expect(errors).toEqual([]);e.record({feature:'M26',scenario:'cached shell offline module loads and actual playlist error recovers with Retry',result:'pass',artifacts:[shot]});
});
