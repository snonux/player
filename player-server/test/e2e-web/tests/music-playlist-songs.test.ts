import { test, expect, request } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let f:MusicFixture,e:Evidence;
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,f.user);});
const modal='#playlist-modal';

test('M27 selected additions long list filtered duplicate move remove Undo original queue and persistence',async({page,context})=>{
 const artifacts:string[]=[];
 let id='';
 try{
  const name='M27 long playlist '+Date.now();
  const created=await page.request.post('/api/v1/playlists',{data:{name,comment:'Occurrence editing acceptance'}});expect(created.ok()).toBe(true);id=(await created.json()).playlist_id;
  const album=(await (await page.request.get('/api/v1/music/albums')).json()).find((a:any)=>a.title==='Night Transit');
  const detail=await(await page.request.get('/api/v1/music/albums/'+album.album_key)).json();
  for(let block=0;block<15;block++)expect((await page.request.post('/api/v1/playlists/'+id+'/albums',{data:{album_key:album.album_key}})).ok()).toBe(true);
  await page.goto('/?music=playlist&id='+id);await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
  await expect(page.locator('.media-row')).toHaveCount(45);
  await page.getByRole('button',{name:'Add songs',exact:true}).click();
  const filter=page.getByRole('searchbox',{name:'Find songs to add',exact:true});await expect(filter).toBeFocused();
  await filter.fill('Harbour');await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();
  await filter.fill('First Light');await page.getByRole('checkbox',{name:'Select First Light',exact:true}).check();
  await expect(page.locator(modal)).toContainText('2 selected');
  // Native typing owns all normal music keys while this input is focused.
  await filter.fill('r/h/l/?');await expect(filter).toHaveValue('r/h/l/?');await expect(page.locator(modal)).toContainText('No matching songs.');
  await filter.fill('Harbour');await expect(page.getByRole('checkbox',{name:'Select Harbour',exact:true})).toBeChecked();
  await page.getByRole('button',{name:'Add selected songs',exact:true}).click();await expect(page.locator(modal)).not.toHaveClass(/open/);
  const saved=await(await page.request.get('/api/v1/playlists/'+id)).json();
  expect(saved.song_count).toBe(47);expect(saved.songs.slice(-2).map((s:any)=>s.id)).toEqual([detail.songs[0].id,detail.songs[2].id]);
  expect(new Set(saved.entries.map((entry:any)=>entry.entry_id)).size).toBe(47);
  const last=saved.entries[46].entry_id;
  const find=page.getByRole('searchbox',{name:'Find playlist songs',exact:true});
  await find.fill('Harbour');await expect(page.locator('[data-count]')).toHaveText('Showing 16 of 47 songs');
  const lastRow=()=>page.locator(`[data-entry="${last}"]`);
  await lastRow().focus();await page.keyboard.press('Enter');await expectPlaying(page,detail.songs[2].id);
  const capture=await captureOutput(page,1400,e.path('m27-filtered-later-source.wav'));assertValidCapture(capture);const segment=mainSegment(capture.analysis);
  const source=f.tracks.find(t=>f.mediaIds[t.path]===detail.songs[2].id)!;expect(Math.abs(segment!.tone_hz!-source.tone_hz[0])).toBeLessThan(5);artifacts.push(capture.wav);
  await page.keyboard.press('Q');await expect(page.locator('#queue-panel [aria-current=true]')).toHaveAttribute('data-queue-index','46');await page.getByRole('button',{name:'Close queue',exact:true}).click();await page.keyboard.press('q');
  // a/Enter expose real keyboard actions, with filtered rows retaining saved IDs.
  await lastRow().focus();await page.keyboard.press('a');
  let menu=page.getByRole('group',{name:'Media actions',exact:true});await expect(menu.getByRole('button',{name:'Move down',exact:true})).toBeDisabled();
  await menu.getByRole('button',{name:'Move up',exact:true}).focus();await page.keyboard.press('Enter');
  await expect(lastRow()).toHaveClass(/selected/);await expect(lastRow()).toBeFocused();
  const moved=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(moved.entries[45].entry_id).toBe(last);expect(moved.entries[46].entry_id).toBe(saved.entries[45].entry_id);
  // Pointer alternative removes just the chosen duplicate, then keyboard Undo.
  await lastRow().getByRole('button',{name:'Song actions for Harbour',exact:true}).click();
  menu=page.getByRole('group',{name:'Media actions',exact:true});await menu.getByRole('button',{name:'Remove from playlist',exact:true}).click();
  await expect(lastRow()).toHaveCount(0);const removed=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(removed.song_count).toBe(46);expect(removed.entries.some((entry:any)=>entry.entry_id===saved.entries[2].entry_id)).toBe(true);
  const undo=page.locator('[data-undo]').getByRole('button',{name:'Undo',exact:true});await undo.focus();await page.keyboard.press('Enter');
  await expect(page.locator('[data-count]')).toHaveText('Showing 16 of 47 songs');
  const restored=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(restored.songs.map((s:any)=>s.id)).toEqual(moved.songs.map((s:any)=>s.id));expect(restored.entries[45].entry_id).not.toBe(last);
  await expect(page.locator(`[data-entry="${restored.entries[45].entry_id}"]`)).toBeFocused();
  // Offline failed move keeps authoritative order and visible selected occurrence.
  const row=page.locator(`[data-entry="${restored.entries[45].entry_id}"]`);await row.focus();await page.keyboard.press('a');await context.setOffline(true);await page.getByRole('group',{name:'Media actions'}).getByRole('button',{name:'Move down',exact:true}).click();
  await expect(page.locator('[data-song-error]')).not.toBeEmpty();await expect(row).toHaveAttribute('data-index','45');await context.setOffline(false);
  for(const [theme,width]of [['dark',1366],['light',1366],['dark',390],['light',390]]as const){
   await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   await expect.poll(()=>page.locator('[data-add-songs]').evaluate(el=>getComputedStyle(el).backgroundColor)).toBe(theme==='light'?'rgb(255, 255, 255)':'rgb(30, 34, 44)');
   const shot=e.path(`m27-songs-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  }
  await page.getByRole('button',{name:'Clear',exact:true}).click();await expect(find).toHaveValue('');await expect(page.locator('.media-row')).toHaveCount(47);
  await page.reload();await expect(page.locator('.media-row')).toHaveCount(47);const persisted=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(persisted.entries).toEqual(restored.entries);
  e.record({feature:'M27',scenario:'long list selected add filtered later duplicate original queue audio move remove Undo offline failure restart',result:'pass',data:{saved,moved,removed,restored,persisted,audio:segment},artifacts});
 }catch(error){e.record({feature:'M27',scenario:'long playlist song editing',result:'fail',detail:String(error),data:{id},artifacts});throw error;}
 finally{await context.setOffline(false);}
});

test('M27 selected add offline and revoked permission retain selection and owner mutations stay isolated',async({page,context})=>{
 const admin=await request.newContext({baseURL:f.origin});let userId=0;let id='';
 try{
  await admin.post('/api/v1/auth/login',{data:f.admin});userId=(await(await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===f.user.username).id;
  const response=await page.request.post('/api/v1/playlists',{data:{name:'M27 error '+Date.now(),comment:''}});id=(await response.json()).playlist_id;
  await page.goto('/?music=playlist&id='+id);await page.getByRole('button',{name:'Add songs',exact:true}).click();
  const find=page.getByRole('searchbox',{name:'Find songs to add',exact:true});await find.fill('First Light');await page.getByRole('checkbox',{name:'Select First Light',exact:true}).check();
  await context.setOffline(true);await page.getByRole('button',{name:'Add selected songs',exact:true}).click();await expect(page.locator(modal).getByRole('alert')).not.toBeEmpty();await expect(find).toHaveValue('First Light');await expect(page.getByRole('checkbox',{name:'Select First Light',exact:true})).toBeChecked();await context.setOffline(false);
  expect((await admin.delete('/api/v1/admin/permissions',{data:{set_id:f.setIds.Music,user_id:userId}})).ok()).toBe(true);
  await page.getByRole('button',{name:'Add selected songs',exact:true}).click();await expect(page.locator(modal).getByRole('alert')).not.toBeEmpty();await expect(page.getByRole('checkbox',{name:'Select First Light',exact:true})).toBeChecked();
  expect((await(await page.request.get('/api/v1/playlists/'+id)).json()).song_count).toBe(0);
  expect((await admin.post('/api/v1/playlists/'+id+'/songs',{data:{action:'add',media_ids:[1]}})).status()).toBe(404);
  await admin.post('/api/v1/admin/permissions',{data:{set_id:f.setIds.Music,user_id:userId,role:'viewer'}});
  await page.getByRole('button',{name:'Add selected songs',exact:true}).click();await expect(page.locator(modal)).not.toHaveClass(/open/);await expect(page.locator('.media-row')).toHaveCount(1);
  await page.getByRole('button',{name:'Add songs',exact:true}).click();await expect(find).toBeVisible();await page.keyboard.press('Escape');await expect(page.locator(modal)).not.toHaveClass(/open/);await expect(page.locator('.media-row.selected')).toBeFocused();
  await page.getByRole('button',{name:'Add songs',exact:true}).click();await expect(find).toBeVisible();await page.locator(modal).getByRole('button',{name:'Cancel',exact:true}).click();await expect(page.locator(modal)).not.toHaveClass(/open/);await expect(page.getByRole('button',{name:'Add songs',exact:true})).toBeFocused();
  e.record({feature:'M27',scenario:'actual offline and revoked add fail without mutation retain checkbox/query retry and foreign owner isolated',result:'pass',data:{id}});
 }catch(error){e.record({feature:'M27',scenario:'song edit failure boundaries',result:'fail',detail:String(error),data:{id}});throw error;}
 finally{await context.setOffline(false);if(userId)await admin.post('/api/v1/admin/permissions',{data:{set_id:f.setIds.Music,user_id:userId,role:'viewer'}});await admin.dispose();}
});

test('M27 pending selected add cannot be submitted again by changing checkboxes',async({page},info)=>{
 const created=await page.request.post('/api/v1/playlists',{data:{name:'M27 pending '+Date.now(),comment:''}});const id=(await created.json()).playlist_id;
 await page.goto('/?music=playlist&id='+id);await page.getByRole('button',{name:'Add songs',exact:true}).click();
 const find=page.getByRole('searchbox',{name:'Find songs to add',exact:true});await find.fill('First Light');await page.getByRole('checkbox',{name:'Select First Light',exact:true}).check();await find.fill('Harbour');await page.getByRole('checkbox',{name:'Select Harbour',exact:true}).check();
 const firefox=info.project.name==='firefox';
 let release!:()=>void;const gate=new Promise<void>(resolve=>release=resolve);let entered!:()=>void;const hit=new Promise<void>(resolve=>entered=resolve);let requests=0;
 if(firefox){
  // Controlled client-response delay preserves untouched application fetch
  // arguments/body/cookies/network. Firefox BiDi omits intercepted postData.
  await page.evaluate(id=>{
   const original=window.fetch;
   let release!:()=>void;const gate=new Promise<void>(resolve=>release=resolve);
   const delay={original,release,requests:0,reached:false,status:0};
   (window as any).__m27Delay=delay;
   window.fetch=async(...args)=>{
    const [input,init]=args;const url=typeof input==='string'?input:input instanceof URL?input.href:input.url;
    if(new URL(url,location.href).pathname===`/api/playlists/${id}/songs`&&init?.method==='POST'){
     delay.requests++;const response=await original(...args);delay.status=response.status;delay.reached=true;await gate;return response;
    }
    return original(...args);
   };
  },id);
 }else{
  await page.route('**/api/playlists/'+id+'/songs',async route=>{requests++;const response=await route.fetch();entered();await gate;await route.fulfill({response});});
 }
 const count=async()=>firefox?await page.evaluate(()=>(window as any).__m27Delay.requests):requests;
 const resume=async()=>{if(firefox)await page.evaluate(()=>(window as any).__m27Delay.release());else release();};
 try{
  const submit=page.getByRole('button',{name:'Add selected songs',exact:true});await submit.click();
  if(firefox){await expect.poll(()=>page.evaluate(()=>(window as any).__m27Delay.reached)).toBe(true);expect(await page.evaluate(()=>(window as any).__m27Delay.status)).toBe(200);}else await hit;
  const choice=page.getByRole('checkbox',{name:'Select Harbour',exact:true});if(await choice.isEnabled())await choice.uncheck();
  await expect(submit).toBeDisabled();expect(await count()).toBe(1);
  await find.fill('First Light');await expect(page.getByRole('checkbox',{name:'Select First Light',exact:true})).toBeDisabled();await expect(submit).toBeDisabled();
  const pending=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(pending.song_count).toBe(2);
  await resume();await expect(page.locator(modal)).not.toHaveClass(/open/);expect(await count()).toBe(1);
  const saved=await(await page.request.get('/api/v1/playlists/'+id)).json();expect(saved.song_count).toBe(2);
  e.record({feature:'M27',scenario:'delayed actual add response cannot reenable submit or create duplicate insertion block',result:'pass',data:{requests:await count(),fault_injection:firefox?'client window.fetch response promise delay; untouched original arguments/body/network':'Playwright held real response',pending,saved}});
 }finally{
  await resume();
  if(firefox)await page.evaluate(()=>{window.fetch=(window as any).__m27Delay.original;});
  else await page.unroute('**/api/playlists/'+id+'/songs');
 }
});
