import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE,'Requires disposable local music fixture');
let f:MusicFixture,e:Evidence;
const panel=(p:Page)=>p.locator('#queue-panel');
const q=(p:Page)=>p.evaluate(async()=>(await import('/js/queue.js')).queueState());
const ids=(s:any)=>s.entries.map((v:any)=>v.id);
const media=(s:any)=>s.entries.map((v:any)=>v.media.id);
const sorted=(values:number[])=>[...values].sort((a,b)=>a-b);
async function saved(p:Page){const c=await(await p.request.get('/api/config')).json();return p.evaluate(id=>JSON.parse(localStorage.getItem(`player.queue.${location.origin}.${id}`)||'null'),c.user_id);}
async function open(p:Page){if(!await panel(p).isVisible()){await p.getByRole('button',{name:'Log out',exact:true}).focus();await p.keyboard.press('Q');}await expect(panel(p)).toBeVisible();}
async function close(p:Page){if(await panel(p).isVisible())await panel(p).getByRole('button',{name:'Close queue',exact:true}).click();}
async function pause(p:Page){if(!(await audioState(p)).paused){if(!await p.locator('#btn-play').isVisible())await p.getByRole('button',{name:'Restore player',exact:true}).click();await p.locator('#btn-play').click();}await expect.poll(async()=>(await audioState(p)).paused).toBe(true);const minimize=p.getByRole('button',{name:'Minimize player (Shift+C)',exact:true});if(await minimize.isVisible())await minimize.click();}
async function play(p:Page){await close(p);if(!await p.locator('#btn-play').isVisible())await p.getByRole('button',{name:'Restore player',exact:true}).click();await p.locator('#btn-play').click();await expect.poll(async()=>(await audioState(p)).paused).toBe(false);}
async function tone(p:Page,id:number,name:string){await expectPlaying(p,id);const capture=await captureOutput(p,1100,e.path(name+'.wav'));assertValidCapture(capture);const segment=mainSegment(capture.analysis),track=f.tracks.find(t=>f.mediaIds[t.path]===id)!;expect(segment!.duration_s).toBeGreaterThan(.6);expect(Math.abs(segment!.tone_hz!-track.tone_hz[0])).toBeLessThan(5);return {id,runtime:await audioState(p),segment,wav:capture.wav};}
async function fixture(p:Page){
 const albums=await(await p.request.get('/api/v1/music/albums')).json();const album=albums.find((a:any)=>a.title==='Night Transit');
 const detail=await(await p.request.get('/api/v1/music/albums/'+album.album_key)).json();
 const songs=[...detail.songs,detail.songs[0],detail.songs[1],detail.songs[2]];
 const name='M18 shuffle '+Date.now();const made=await p.request.post('/api/v1/playlists',{data:{name}});expect(made.ok()).toBe(true);const pid=(await made.json()).playlist_id;
 expect((await p.request.post(`/api/v1/playlists/${pid}/songs`,{data:{action:'add',media_ids:songs.map((s:any)=>s.id)}})).ok()).toBe(true);
 await p.goto('/?music=playlist&id='+pid);await expect(p.locator('.media-row')).toHaveCount(songs.length);
 return {pid,songs,album,detail,name};
}
async function options(p:Page){await close(p);await p.locator('#media-grid [data-collection-menu]').click();await p.getByRole('menu',{name:'More collection actions'}).getByRole('button',{name:'Shuffle options…',exact:true}).click();const dialog=p.locator('#shuffle-options-modal');await expect(dialog).toBeVisible();return dialog;}
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,f.user);});

test('M18 queue remainder preserves current source, canonical Off, reshuffle Undo, independent browse and actual full collection cycle',async({page})=>{
 let pid='';const proof:any[]=[];
 try{
  const setup=await fixture(page);pid=setup.pid;
  await page.locator('.media-row').nth(1).click();await expectPlaying(page,setup.songs[1].id);await pause(page);await open(page);
  const before=await q(page),runtime=await audioState(page),button=panel(page).locator('[data-queue-action="shuffle"]');
  await button.focus();await page.keyboard.press('Enter');await expect(button).toBeFocused();await expect(button).toHaveAttribute('aria-pressed','true');
  const mixed=await q(page);expect(ids(mixed).slice(0,2)).toEqual(ids(before).slice(0,2));expect(sorted(ids(mixed))).toEqual(sorted(ids(before)));expect((await audioState(page)).src).toBe(runtime.src);expect((await audioState(page)).time).toBeCloseTo(runtime.time,1);
  await panel(page).getByRole('button',{name:'Reshuffle remaining',exact:true}).click();await panel(page).getByRole('button',{name:'Undo',exact:true}).click();expect(ids(await q(page))).toEqual(ids(mixed));expect((await q(page)).shuffle).toBe(true);
  await button.click();expect(ids(await q(page))).toEqual(ids(before));await button.click();const retained=await q(page);
  await close(page);await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();await page.locator('#music-albums').click();await expect(page.locator('.album-card').first()).toBeVisible();expect(ids(await q(page))).toEqual(ids(retained));expect((await q(page)).index).toBe(retained.index);
  await page.getByRole('button',{name:'Log out',exact:true}).focus();await page.keyboard.press('r');expect((await q(page)).shuffle).toBe(true);expect(ids(await q(page))).toEqual(ids(retained));
  await page.goto('/?music=playlist&id='+pid);await page.locator('[data-collection-shuffle]').click();await expect.poll(async()=>(await q(page)).loading).toBe(false);await pause(page);await open(page);
  const collection=await q(page),stored=await saved(page);expect(collection.shuffle).toBe(true);expect(collection.index).toBe(0);expect(sorted(media(collection))).toEqual(sorted(setup.songs.map((s:any)=>s.id)));expect(stored.canonical).toEqual(sorted(ids(collection)));
  const visited:number[]=[];
  for(let at=0;at<collection.entries.length;at++){
   await play(page);await expectPlaying(page,collection.entries[at].media.id);proof.push(await tone(page,collection.entries[at].media.id,`m18-cycle-${at}`));visited.push((await q(page)).entries[(await q(page)).index].id);await pause(page);
   if(at+1<collection.entries.length){await page.getByRole('button',{name:'Log out',exact:true}).focus();await page.keyboard.press('N');await expectPlaying(page,collection.entries[at+1].media.id);await pause(page);}
  }
  expect(visited).toEqual(ids(collection));expect(new Set(visited).size).toBe(collection.entries.length);
  await open(page);expect(await panel(page).locator('[data-queue-action="reshuffle"]').isDisabled()).toBe(true);await panel(page).locator('[data-queue-action="shuffle"]').click();expect((await q(page)).index).toBe(collection.entries.length-1);expect(ids(await q(page))).toEqual(ids(collection));
  e.record({feature:'M18',scenario:'remainder toggle/current source/reshuffle Undo/library browse/r unchanged and complete audible occurrence cycle',result:'pass',data:{before,mixed,retained,collection,stored,visited,proof}});
 }catch(error){e.record({feature:'M18',scenario:'shuffle core',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally{if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});

test('M18 optional collection filters invalid/no-match/offline/Cancel preserve queue, successful limited shuffle and responsive themes',async({page,context})=>{
 let pid='';const artifacts:string[]=[];
 try{
  const setup=await fixture(page);pid=setup.pid;await page.locator('.media-row').first().click();await expectPlaying(page,setup.songs[0].id);await pause(page);const before=await saved(page);
  const escaped=await options(page);await escaped.getByLabel('Genre (optional)',{exact:true}).focus();await page.keyboard.press('Escape');await expect(escaped).not.toBeVisible();await expect(page.locator('#media-grid [data-collection-menu]')).toBeFocused();
  const dialog=await options(page),submit=dialog.getByRole('button',{name:'Shuffle',exact:true});
  await dialog.getByLabel('Maximum songs (optional)',{exact:true}).fill('0');await submit.click();await expect(dialog).toContainText('Maximum songs must be a whole number');expect((await saved(page)).entries).toEqual(before.entries);
  await dialog.getByLabel('Maximum songs (optional)',{exact:true}).fill('2');await dialog.getByLabel('Genre (optional)',{exact:true}).fill('No such genre');await submit.click();await expect(dialog).toContainText('No songs match these options.');expect((await saved(page)).entries).toEqual(before.entries);
  const metadata=setup.songs.find((s:any)=>s.music?.genres?.length&&s.music?.year);expect(metadata).toBeTruthy();const genre=metadata.music.genres[0],year=metadata.music.year;
  await dialog.getByLabel('Genre (optional)',{exact:true}).fill(genre);await dialog.getByLabel('From year (optional)',{exact:true}).fill(String(year));await dialog.getByLabel('To year (optional)',{exact:true}).fill(String(year));
  await context.setOffline(true);await submit.click();await expect(dialog).toContainText('Could not load songs.');expect((await saved(page)).entries).toEqual(before.entries);await context.setOffline(false);
  await submit.click();await expect(dialog).not.toBeVisible();await expect.poll(async()=>(await q(page)).loading).toBe(false);await pause(page);
  const selected=await q(page),eligible=setup.songs.filter((s:any)=>s.music?.year===year&&s.music?.genres?.some((g:string)=>g.trim().toLowerCase()===genre.trim().toLowerCase()));expect(selected.entries.length).toBe(Math.min(2,eligible.length));expect(media(selected).every((id:number)=>eligible.some((s:any)=>s.id===id))).toBe(true);expect(selected.shuffle).toBe(true);const proof=await (async()=>{await play(page);const t=await tone(page,selected.entries[0].media.id,'m18-filtered-source');await pause(page);return t;})();
  await open(page);const path=e.path('m18-queue-'+(await page.locator('html').getAttribute('data-theme')||'dark')+'-narrow.png');await page.setViewportSize({width:390,height:844});await page.screenshot({path});artifacts.push(path);await close(page);
  // Delay real responses and exercise both pending Escape and Cancel focus.
  const canceledBefore=await saved(page);
  for(const action of ['Escape','Cancel']){
   let release!:()=>void;const gate=new Promise<void>(resolve=>release=resolve);let requested!:()=>void;const started=new Promise<void>(resolve=>requested=resolve);
   let completed!:()=>void;const done=new Promise<void>(resolve=>completed=resolve);
   await page.route(`**/api/playlists/${pid}`,async route=>{const response=await route.fetch();requested();await gate;await route.fulfill({response});completed();});
   const pending=await options(page);await pending.getByRole('button',{name:'Shuffle',exact:true}).click();await started;
   if(action==='Escape'){await pending.getByLabel('Genre (optional)',{exact:true}).focus();await page.keyboard.press('Escape');}
   else await pending.getByRole('button',{name:'Cancel',exact:true}).click();
   await expect(pending).not.toBeVisible();await expect(page.locator('#media-grid [data-collection-menu]')).toBeEnabled();await expect(page.locator('#media-grid [data-collection-menu]')).toBeFocused();
   const received=page.waitForResponse(response=>new URL(response.url()).pathname===`/api/playlists/${pid}`&&response.status()===200);
   release();await done;await(await received).finished();await page.waitForTimeout(200);await page.unroute(`**/api/playlists/${pid}`);expect((await saved(page)).entries).toEqual(canceledBefore.entries);
  }
  e.record({feature:'M18',scenario:'optional genre/year/max validation no-match actual offline retry cancellation responsive originals and filtered source',result:'pass',data:{before,eligible:eligible.map((s:any)=>s.id),selected,proof,canceledBefore},artifacts});
 }catch(error){e.record({feature:'M18',scenario:'shuffle options/boundaries',result:'fail',detail:String(error),artifacts});throw error;}
 finally{await context.setOffline(false);if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});

test('M18 scoped shuffle snapshot survives reload paused; keyboard typing focus and real account isolation',async({page})=>{
 let pid='';try{
  const setup=await fixture(page);pid=setup.pid;await page.locator('[data-collection-shuffle]').click();await expect.poll(async()=>(await q(page)).loading).toBe(false);await pause(page);const before=await saved(page);await page.reload();await open(page);await expect(panel(page).getByRole('button',{name:'Resume queue',exact:true})).toBeVisible();const after=await saved(page);expect(after.entries).toEqual(before.entries);expect(after.canonical).toEqual(before.canonical);expect(after.shuffle).toBe(true);expect(after.index).toBe(before.index);expect((await audioState(page)).paused).toBe(true);
  await close(page);await page.keyboard.press('/');const search=page.getByRole('searchbox',{name:'Search media'});await search.fill('r Q j/k');await expect(search).toHaveValue('r Q j/k');expect((await q(page)).shuffle).toBe(true);await page.keyboard.press('Escape');
  const logout=async()=>{await close(page);await page.getByRole('button',{name:'Log out',exact:true}).click();await page.waitForURL(u=>u.pathname==='/login.html');};
  await logout();await keyboardLogin(page,f.admin);await open(page);expect((await q(page)).entries).toHaveLength(0);expect((await q(page)).shuffle).toBe(false);await logout();await keyboardLogin(page,f.user);await open(page);expect((await saved(page)).canonical).toEqual(before.canonical);expect((await q(page)).shuffle).toBe(true);
  await panel(page).getByRole('button',{name:'Resume queue',exact:true}).click();await close(page);const proof=await tone(page,before.entries[before.index].mediaId,'m18-recovered-source');await pause(page);
  e.record({feature:'M18',scenario:'scoped order/mode/canonical reload and account roundtrip paused then actual Resume source, native typing',result:'pass',data:{before,after,proof}});
 }catch(error){e.record({feature:'M18',scenario:'shuffle recovery',result:'fail',detail:String(error)});throw error;}finally{if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});


test('M18 settled actual theme controls show readable queue On and Shuffle options on desktop and narrow screens',async({page})=>{
 let pid='';const artifacts:string[]=[],computed:any[]=[];
 try{
  const setup=await fixture(page);pid=setup.pid;await page.locator('[data-collection-shuffle]').click();await expect.poll(async()=>(await q(page)).loading).toBe(false);await pause(page);
  for(const [theme,width] of [['light',1366],['dark',1366],['light',390],['dark',390]] as const){
   await page.setViewportSize({width,height:844});await close(page);
   if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
   expect(await page.evaluate(()=>localStorage.getItem('kiss-theme'))).toBe(theme);
   await page.waitForTimeout(250);
   await open(page);await expect(panel(page).locator('[data-queue-action="shuffle"]')).toHaveAttribute('aria-pressed','true');
   const queueShot=e.path(`m18-actual-queue-${theme}-${width}.png`);await page.screenshot({path:queueShot,animations:'disabled'});artifacts.push(queueShot);
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   await close(page);const dialog=await options(page);
   const cancel=dialog.getByRole('button',{name:'Cancel',exact:true});await expect(cancel).toBeVisible();
   await page.waitForTimeout(250);
   const colors=await cancel.evaluate(button=>{
    const style=getComputedStyle(button),modal=getComputedStyle(button.closest('.modal')!);
    return {theme:document.documentElement.getAttribute('data-theme'),foreground:style.color,background:style.backgroundColor,modalBackground:modal.backgroundColor};
   });
   const rgb=(text:string)=>(text.match(/[\d.]+/g)||[]).slice(0,3).map(Number);
   const luminance=(values:number[])=>values.map(v=>{const c=v/255;return c<=.04045?c/12.92:((c+.055)/1.055)**2.4;}).reduce((sum,c,i)=>sum+c*[.2126,.7152,.0722][i],0);
   const fg=luminance(rgb(colors.foreground)),bg=luminance(rgb(colors.background));const ratio=(Math.max(fg,bg)+.05)/(Math.min(fg,bg)+.05);
   computed.push({...colors,width,contrast:ratio});expect(ratio).toBeGreaterThanOrEqual(4.5);
   const optionShot=e.path(`m18-actual-options-${theme}-${width}.png`);await page.screenshot({path:optionShot,animations:'disabled'});artifacts.push(optionShot);
   await cancel.click();await expect(page.locator('#media-grid [data-collection-menu]')).toBeFocused();
  }
  e.record({feature:'M18',scenario:'actual persisted theme controls, settled Cancel contrast, selected queue On and options desktop/narrow originals',result:'pass',data:{computed},artifacts});
 }catch(error){e.record({feature:'M18',scenario:'actual settled theme inspection',result:'fail',detail:String(error),data:{computed},artifacts});throw error;}finally{if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});

test('M18 actual revoked Music grant preserves queue/source/position for direct Shuffle and options, restored grant retries audibly',async({page})=>{
 let pid='';const admin=await request.newContext({baseURL:f.origin});let grant:any;
 const artifacts:string[]=[],responses:any[]=[];
 try{
  const setup=await fixture(page);pid=setup.pid;
  await page.locator('.media-row').nth(1).click();await expectPlaying(page,setup.songs[1].id);await pause(page);
  await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();await page.locator('#music-albums').click();
  await page.getByRole('button',{name:'Open album Night Transit by Aurora Lane',exact:true}).click();
  await expect(page.locator('[data-collection-shuffle]')).toBeVisible();
  const before=await saved(page),runtime=await audioState(page);
  expect((await admin.post('/api/v1/auth/login',{data:f.admin})).ok()).toBe(true);
  const users=await(await admin.get('/api/v1/admin/users')).json();
  const user=users.find((u:any)=>u.username===f.user.username);
  const matrix=await(await admin.get('/api/v1/admin/permissions')).json();
  const prior=matrix.permissions.find((g:any)=>g.set_id===f.setIds.Music&&g.user_id===user.id);
  expect(prior).toBeTruthy();grant={set_id:prior.set_id,user_id:prior.user_id,role:prior.role};
  page.on('response',response=>{if(new URL(response.url()).pathname.includes('/music/albums/'))responses.push({url:new URL(response.url()).pathname,status:response.status()});});
  let deniedStatus=0;
  try{
   expect((await admin.delete('/api/v1/admin/permissions',{data:grant})).ok()).toBe(true);
   deniedStatus=(await page.request.get('/api/v1/music/albums/'+setup.album.album_key)).status();expect(deniedStatus).toBeGreaterThanOrEqual(400);
   await page.locator('[data-collection-shuffle]').click();await expect(page.locator('[data-collection-status]')).toContainText('Could not load songs.');
   expect((await saved(page)).entries).toEqual(before.entries);
   const direct=e.path('m18-permission-direct-retained.png');await page.screenshot({path:direct});artifacts.push(direct);
   const dialog=await options(page);await dialog.getByRole('button',{name:'Shuffle',exact:true}).click();await expect(dialog).toContainText('Could not load songs.');
   const retained=await saved(page),current=await audioState(page);
   expect(retained.entries).toEqual(before.entries);expect(retained.canonical).toEqual(before.canonical);expect(retained.index).toBe(before.index);expect(retained.shuffle).toBe(before.shuffle);expect(retained.position).toBeCloseTo(before.position,1);
   expect(current.src).toBe(runtime.src);expect(current.paused).toBe(true);expect(current.time).toBeCloseTo(runtime.time,1);
   const denied=e.path('m18-permission-options-retained.png');await page.screenshot({path:denied});artifacts.push(denied);
  }finally{
   expect((await admin.post('/api/v1/admin/permissions',{data:grant})).ok()).toBe(true);
   const restored=await(await admin.get('/api/v1/admin/permissions')).json();expect(restored.permissions.some((g:any)=>g.set_id===grant.set_id&&g.user_id===grant.user_id&&g.role===grant.role)).toBe(true);
  }
  const dialog=page.locator('#shuffle-options-modal');await dialog.getByRole('button',{name:'Shuffle',exact:true}).click();await expect(dialog).not.toBeVisible();
  await expect.poll(async()=>(await q(page)).loading).toBe(false);const selected=await q(page);expect(selected.shuffle).toBe(true);expect(sorted(media(selected))).toEqual(sorted(setup.detail.songs.map((song:any)=>song.id)));
  const proof=await tone(page,selected.entries[0].media.id,'m18-permission-restored-source');await pause(page);
  e.record({feature:'M18',scenario:'actual Music grant revocation direct/options preserves queue source position; exact prior grant restored then audible retry',result:'pass',data:{grant,deniedStatus,responses,before,selected,proof},artifacts});
 }catch(error){e.record({feature:'M18',scenario:'actual shuffle permission boundary',result:'fail',detail:String(error),data:{responses},artifacts});throw error;}
 finally{if(grant)await admin.post('/api/v1/admin/permissions',{data:grant});await admin.dispose();if(pid)await page.request.delete('/api/v1/playlists/'+pid);}
});
