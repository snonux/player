import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, selectByKeys, openFolder, playCard, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, resetProgress, openSidebarMusic } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
const folder = 'Aurora Lane - Night Transit (2021)';
const albumName = 'Night Transit';
test.beforeAll(async ({ browser }, info) => {
  fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});
});
test.beforeEach(async ({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,fixture.user);});
async function restorePlayer(page:Page) {
  if((await page.locator('#player').getAttribute('class'))?.includes('minimized')) await page.getByRole('button',{name:'Restore player',exact:true}).click();
}
/** Collapse the floating player so album cards near the bottom stay clickable. */
async function minimizePlayer(page:Page) {
  const player=page.locator('#player');
  const cls=await player.getAttribute('class') || '';
  if(!cls.includes('open') || cls.includes('minimized')) return;
  // Shift+C is the documented minimize shortcut; the ▾ control can sit under
  // expanded loudness/visuals chrome and miss a plain click on Firefox.
  await page.keyboard.press('Shift+KeyC');
  await expect(player).toHaveClass(/minimized/,{timeout:5000});
}
async function albums(page:Page) {
  await minimizePlayer(page);
  if(await page.locator('.album-card').count() && !(await page.getByRole('heading',{name:albumName,exact:true}).isVisible().catch(()=>false))) {
    return;
  }
  await openSidebarMusic(page,'Albums');
  await expect(page.locator('.album-card').first()).toBeVisible();
}
async function openAlbum(page:Page,title=albumName,artist='Aurora Lane') {
  await albums(page);
  await minimizePlayer(page);
  // Nested favorite/rating flatten the card to role=group; the primary open
  // control is the labelled sibling/button (same label as the card).
  const label=`Open album ${title} by ${artist}`;
  const open=page.getByRole('button',{name:label,exact:true});
  await expect(open).toHaveCount(1);
  // DOM click bypasses the minimized restore bar that intercepts Playwright
  // pointer events on lower album cards in Firefox.
  await open.evaluate((el:HTMLElement)=>el.click());
  await expect(page.getByRole('heading',{name:title,exact:true})).toBeVisible({timeout:15_000});
}
async function audible(page:Page,song:any,name:string) {
  await expectPlaying(page,song.id);
  const capture=await captureOutput(page,1300,evidence.path(name+'.wav'));
  assertValidCapture(capture);const segment=mainSegment(capture.analysis);
  expect(segment?.duration_s).toBeGreaterThan(.8);
  expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
  return {wav:capture.wav,state:await audioState(page),audio:segment};
}

test('M02 library and song entry, album artwork/totals/discs, linked artist, typing and themes',async({page})=>{
 const artifacts:string[]=[];const contrastChecks:any[]=[];
 try {
  const catalog=await (await page.request.get('/api/v1/music/albums')).json();
  const album=catalog.find((a:any)=>a.title===albumName);
  expect(album.song_count).toBe(3);expect(album.duration).toBeGreaterThanOrEqual(36);expect(album.duration).toBeLessThan(37);
  expect(catalog.some((a:any)=>a.title==='Hidden Album')).toBe(false);
  const detail=await (await page.request.get(`/api/v1/music/albums/${album.album_key}`)).json();
  expect(detail.songs.map((m:any)=>m.music.title)).toEqual(['First Light','Long Road Home','Harbour']);
  expect(detail.songs.every((m:any)=>m.set_id===fixture.setIds.Music)).toBe(true);
  expect((await page.request.get(`/api/v1/media/${detail.cover_media_id}/thumbnail`)).status()).toBe(200);
  await albums(page);
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
    await page.setViewportSize({width,height:844});
    if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    const shot=evidence.path(`m02-catalog-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
  await openAlbum(page);
  await expect(page.locator('.album-header')).toContainText('3 songs');
  expect(await page.locator('.album-header img').evaluate((img:HTMLImageElement)=>img.naturalWidth)).toBeGreaterThan(0);
  await page.getByRole('button',{name:'Album artist: Aurora Lane',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Aurora Lane',exact:true})).toBeVisible();
  for(const theme of ['dark','light']) {
    if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    const shot=evidence.path(`m02-linked-artist-${theme}-390.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  }
  await page.getByRole('button',{name:'← Back',exact:true}).click();
  await expect(page.getByRole('heading',{name:albumName,exact:true})).toBeVisible();
  await expect(page.getByRole('heading',{name:albumName,exact:true})).toBeVisible();
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
    await page.setViewportSize({width,height:844});
    if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    await expect.poll(async()=>page.locator('.media-row').last().evaluate(row=>getComputedStyle(row).backgroundColor)).toBe(theme==='light' ? 'rgb(255, 255, 255)' : 'rgb(22, 25, 32)');
    const colors=await page.locator('.media-row .text-muted').last().evaluate(el=>({foreground:getComputedStyle(el).color,background:getComputedStyle(el.closest('.media-row')!).backgroundColor}));
    const luminance=(color:string)=>{const rgb=color.match(/[\d.]+/g)!.slice(0,3).map(Number).map(v=>v/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4);return rgb[0]*.2126+rgb[1]*.7152+rgb[2]*.0722;};
    const a=luminance(colors.foreground),b=luminance(colors.background),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);
    expect(ratio).toBeGreaterThanOrEqual(4.5);contrastChecks.push({theme,width,...colors,ratio});
    const shot=evidence.path(`m02-detail-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
  await page.keyboard.press('Backspace');
  await expect(page.locator('.album-card.selected')).toHaveAttribute('data-album',album.album_key);
  await expect(page.locator('.album-card.selected .collection-open')).toBeFocused();
  await selectByKeys(page,'Open album Synth Compilation by Various Artists',100);await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Synth Compilation',exact:true})).toBeVisible();
  await expect(page.locator('.music-number')).toHaveCount(3);
  expect(await page.locator('.music-number').allTextContents()).toEqual(['1-1','1-2','2-1']);
  expect(await page.locator('.song-open').allTextContents()).toEqual(['Neon Rain','Glass City','Afterglow']);
  await page.getByRole('button',{name:'← Back to albums',exact:true}).click();
  const long=catalog.find((a:any)=>a.title.startsWith('Long Album '));
  await openAlbum(page,long.title,long.artist);
  for(const theme of ['dark','light']) {
    if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    await expect(page.getByRole('heading',{name:long.title,exact:true})).toBeVisible();
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    const shot=evidence.path(`m02-long-${theme}-390.png`);await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  }
  // The song entry follows the actual Sets info dialog and returns to this lens.
  await openFolder(page,fixture.setIds.Music,folder);
  const first=track(fixture,`Music/${folder}/01 - First Light.mp3`);
  const songCard=page.locator(`#media-grid [data-id="${first.id}"]`);
  const openControl=songCard.locator('.media-open');
  if(await openControl.count()) await openControl.focus();
  else await songCard.focus();
  await page.keyboard.press('i');
  await expect(page.getByRole('button',{name:'Open album',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Open album',exact:true}).click();
  await expect(page.getByRole('heading',{name:albumName,exact:true})).toBeVisible();
  await page.keyboard.press('/');await page.keyboard.type('Np');
  await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('Np');
  // Enter commits the query and navigates to search results (Escape only
  // cancels a pending debounce and restores the prior album lens).
  await page.keyboard.press('Enter');
  await expect(page.locator('#media-grid')).toContainText(/No results\.|Nothing matches these filters/,{timeout:5_000});
  // An intentional search works; a subsequent still-debounced search must
  // not override the user's explicit Albums navigation.
  await page.keyboard.press('/');
  await page.getByRole('searchbox',{name:'Search media'}).fill('pending');
  await page.keyboard.press('Escape');
  await albums(page);
  await page.waitForTimeout(400); // exceed the production 300 ms debounce
  await expect(page.locator('.album-card').first()).toBeVisible();
  // Exercise a real browser offline state through an in-app request, while
  // retaining the already loaded shell and its Back/Retry controls.
  await page.context().setOffline(true);
  try {
    await openSidebarMusic(page,'Albums');
    await expect(page.getByRole('alert')).toContainText('Could not load albums');
    await expect(page.getByRole('button',{name:'Retry',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'← Back',exact:true})).toBeVisible();
    const shot=evidence.path('m02-offline-catalog.png');await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  } finally { await page.context().setOffline(false); }
  await page.getByRole('button',{name:'Retry',exact:true}).click();
  await expect(page.locator('.album-card').first()).toBeVisible();
  // Empty/permission behavior uses actual server ACLs, not route interception.
  const admin=await request.newContext({baseURL:fixture.origin});
  try {
    await admin.post('/api/v1/auth/login',{data:fixture.admin});
    const privateAlbums=await (await admin.get('/api/v1/music/albums')).json();
    expect(privateAlbums.find((a:any)=>a.title===albumName).song_count).toBe(4);
    const hidden=privateAlbums.find((a:any)=>a.title==='Hidden Album');
    const userId=(await (await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===fixture.user.username).id;
    try {
      expect((await admin.delete('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId}})).status()).toBe(200);
      await openSidebarMusic(page,'Albums');
      await expect(page.locator('#media-grid')).toContainText('No albums available.');
      const shot=evidence.path('m02-real-acl-empty-catalog.png');await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
    } finally {
      expect((await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}})).status()).toBe(200);
      expect((await (await page.request.get('/api/v1/sets')).json()).some((set:any)=>set.id===fixture.setIds.Music)).toBe(true);
    }
    expect((await page.request.get(`/api/v1/music/albums/${hidden.album_key}`)).status()).toBe(404);
    await page.goto(`/?music=album&id=${hidden.album_key}`);
    await expect(page.getByRole('alert')).toContainText('unavailable');
    await expect(page.getByRole('button',{name:'Retry',exact:true})).toBeVisible();
    const denied=evidence.path('m02-real-acl-unavailable.png');await page.screenshot({path:denied,fullPage:true});artifacts.push(denied);
  } finally {await admin.dispose();}
  evidence.record({feature:'M02',scenario:'catalog/detail/song route, ACL aggregates/artwork, linked artist, discs, keyboard and themes',result:'pass',data:{album,detail,contrastChecks},artifacts});
 } catch(error) {evidence.record({feature:'M02',scenario:'catalog/detail/song route and design',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M02 ordered collection starts at zero, survives browsing, stops at end and retries actual permission error',async({page})=>{
 const artifacts:string[]=[];const proof:any[]=[];
 const songs=['01 - First Light.mp3','02 - Long Road Home.mp3','03 - Harbour.flac'].map(name=>track(fixture,`Music/${folder}/${name}`));
 const admin=await request.newContext({baseURL:fixture.origin});let userId=0,revoked=false;
 try {
  await admin.post('/api/v1/auth/login',{data:fixture.admin});
  userId=(await (await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===fixture.user.username).id;
  for(const song of songs) await resetProgress(page,song.id);
  await page.request.post('/api/v1/progress',{data:{media_id:songs[0].id,position_seconds:8}});
  await openAlbum(page);await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
  await expectPlaying(page,songs[0].id);expect((await audioState(page)).time).toBeLessThan(4);
  // Deny the next source before it has ever been loaded in this browser context.
  revoked=true;
  const revoke=await admin.delete('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId}});
  expect(revoke.status()).toBe(200);
  await restorePlayer(page);
  expect((await page.request.get(`/api/v1/media/${songs[1].id}/stream`)).status()).toBe(403);
  const deniedStream=page.waitForResponse(response=>response.url().includes(`/media/${songs[1].id}/stream`) && response.status()===403);
  await page.getByRole('button',{name:'Next track',exact:true}).click();
  expect((await deniedStream).status()).toBe(403);
  await expect(page.locator('#toast')).toContainText('Cannot play');
  const failed=evidence.path('m02-actual-stream-permission.png');await page.screenshot({path:failed,fullPage:true});artifacts.push(failed);
  expect((await page.request.get(`/api/v1/media/${songs[1].id}/stream`)).status()).toBe(403);
  await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});revoked=false;
  await page.locator('#btn-play').click();
  const retry=await audible(page,songs[1],'m02-restored-permission-retry');artifacts.push(retry.wav);
  await openAlbum(page);await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
  await expectPlaying(page,songs[0].id);expect((await audioState(page)).time).toBeLessThan(4);
  proof.push(await audible(page,songs[0],'m02-first'));artifacts.push(proof.at(-1).wav);
  // Browse another album while the handler retains the original ordered list.
  await openAlbum(page,'Synth Compilation','Various Artists');
  proof.push(await audible(page,songs[1],'m02-second'));artifacts.push(proof.at(-1).wav);
  proof.push(await audible(page,songs[2],'m02-third'));artifacts.push(proof.at(-1).wav);
  await expect.poll(async()=>{const s=await audioState(page);return s.paused && s.time>10;},{timeout:20000}).toBe(true);
  expect((await audioState(page)).src).toMatch(new RegExp(`/media/${songs[2].id}/`));
  await page.waitForTimeout(1200);expect((await audioState(page)).src).toMatch(new RegExp(`/media/${songs[2].id}/`));
  for(const song of songs) expect((await (await page.request.get(`/api/v1/media/${song.id}`)).json()).progress?.finished).toBe(true);
  evidence.record({feature:'M02',scenario:'ordered audible snapshot/start0/independent browse/stopend/real ACL failure+Retry',result:'pass',data:{proof,retry},artifacts});
 }catch(error){evidence.record({feature:'M02',scenario:'ordered collection playback and real permission retry',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
 finally {
  if(userId) {
    const restored=await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}});
    expect(restored.status()).toBe(200);
    const visible=await (await page.request.get('/api/v1/sets')).json();
    expect(visible.some((set:any)=>set.id===fixture.setIds.Music)).toBe(true);
  }
  await admin.dispose();
 }
});


test('M02 browser Back restores Sets source and retires the previous album snapshot',async({page})=>{
 const artifacts:string[]=[];
 try {
  const source=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a');
  const following=track(fixture,'Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus');
  for(const song of [source,following]) await resetProgress(page,song.id);
  await openFolder(page,fixture.setIds.Music,'Various Artists - Synth Compilation/Disc 1');
  await playCard(page,source.title,source.id);
  await expectPlaying(page,source.id);
  const setsURL=page.url();
  await openAlbum(page);await page.getByRole('button',{name:'▶ Play album',exact:true}).click();
  const first=track(fixture,`Music/${folder}/01 - First Light.mp3`);
  await expectPlaying(page,first.id);
  // Use real browser history, not a reload or synthetic navigation event.
  for(let step=0;step<6 && page.url()!==setsURL;step++) {
    await page.goBack();
    await expect(page.locator('#media-grid')).not.toContainText('Loading...');
  }
  expect(page.url()).toBe(setsURL);
  await expect.poll(async()=>(await audioState(page)).src).toMatch(new RegExp(`(/media/${source.id}/|^blob:)`));
  await restorePlayer(page);
  await page.locator('#btn-play').click();await expectPlaying(page,source.id);
  await restorePlayer(page);
  await page.getByRole('button',{name:'Next track',exact:true}).click();
  const proof=await audible(page,following,'m02-history-restored-sets-next');artifacts.push(proof.wav);
  const shot=evidence.path('m02-history-restored-sets.png');await page.screenshot({path:shot,fullPage:true,animations:'disabled'});artifacts.push(shot);
  evidence.record({feature:'M02',scenario:'actual browser history restores different Sets source; Next follows Sets rather than stale album',result:'pass',data:{setsURL,proof},artifacts});
 }catch(error){evidence.record({feature:'M02',scenario:'actual browser history snapshot lifetime',result:'fail',detail:String(error),artifacts});throw error;}
});
