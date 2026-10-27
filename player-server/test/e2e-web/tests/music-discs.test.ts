import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, SUPPORT_DIR, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, captureOutput, assertValidCapture, mainSegment, expectPlaying, openSidebarMusic } from './helpers/music-ui';
import { installAudioTap } from './helpers/audio-tap';
import { settleMusicViewport } from './helpers/music-viewport';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE,'Requires augmented --multi-disc fixture');
let fixture:MusicFixture, evidence:Evidence;
const titles=['M04 Two Disc Album','M04 Compilation'];
test.beforeAll(async({browser},info)=>{
 fixture=loadFixture(); evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});
});
test.beforeEach(async({page})=>{
 test.skip(!fixture.mediaIds['Music/M04/Album/Disc 1/1.flac'],'Requires M04 augmentation');
 await page.addInitScript(installAudioTap); await keyboardLogin(page,fixture.user);
});
async function albums(page:Page) {
 const classes=await page.locator('#player').getAttribute('class') || '';
 if(classes.includes('open')&&!classes.includes('minimized')) await page.keyboard.press('Escape');
 await openSidebarMusic(page,'Albums');
 await expect(page.locator('.album-card').first()).toBeVisible();
}
async function open(page:Page,title:string) {
 await albums(page);
 const artist=title===titles[0]?'M04 Ensemble':title===titles[1]?'Various Artists':'M04 Solo';
 await page.getByRole('button',{name:`Open album ${title} by ${artist}`,exact:true}).click();
 await expect(page.getByRole('heading',{name:title,exact:true})).toBeVisible();
}
async function followCredit(page:Page,name:string,keyboard:boolean,rowIndex=2) {
 const row=page.locator('.media-row').nth(rowIndex);
 const button=row.getByRole('button',{name:`Track artist: ${name}`,exact:true});
 if(!keyboard) { await button.click();return; }
 // The previous album's ended Restore player remains visible. Native Tab
 // navigation reaches the link while preserving the existing player.
 await row.focus();
 for(let count=0;count<16;count++) {
  await page.keyboard.press('Tab');
  if(await button.evaluate(el=>el===document.activeElement)) {
   await expect(button).toBeFocused();await page.keyboard.press('Enter');return;
  }
 }
 throw new Error(`Artist link was not reachable through native Tab: ${name}`);
}
async function audible(page:Page,path:string,name:string) {
 const song=track(fixture,path); await expectPlaying(page,song.id);
 const out=await captureOutput(page,1600,evidence.path(name+'.wav')); assertValidCapture(out);
 expect(mainSegment(out.analysis)?.duration_s).toBeGreaterThan(1);
 expect(Math.abs(mainSegment(out.analysis)!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
 return out.wav;
}

test('M04 two discs and compilation: grouped artwork, structured artist links, global playback indices',async({page})=>{
 test.setTimeout(180000);
 const artifacts:string[]=[];
 try {
  for(const [index,title] of titles.entries()) {
   const folder=index?'Compilation':'Album';
   if(index) {
    const catalog=await (await page.request.get('/api/v1/music/albums')).json();
    const compilation=catalog.filter((album:any)=>album.title===title);
    expect(compilation).toHaveLength(1);expect(compilation[0].artist).toBe('Various Artists');expect(compilation[0].song_count).toBe(3);
    const detail=await (await page.request.get(`/api/v1/music/albums/${compilation[0].album_key}`)).json();
    expect(detail.songs.map((song:any)=>song.music.artists)).toEqual([['M04 A / B','M04 東京'],['M04 Guest'],['M04 A / B','M04 東京']]);
   }
   await open(page,title);
   if(index) await expect(page.locator('.media-row').nth(1).locator('.credit-link')).toHaveText(['M04 Guest']);
   await expect(page.locator('.song-open')).toHaveText([`M04 ${folder} 1-1`,`M04 ${folder} 1-2`,`M04 ${folder} 2-1`]);
   await expect(page.locator('.music-number')).toHaveText(['1-1','1-2','2-1']);
   await expect(page.locator('.music-disc')).toHaveCount(2);
   await expect(page.locator('.music-disc h2').first()).toHaveText('Disc 1 — Original');
   await expect(page.locator('.music-disc h2').last()).toContainText('Disc 2 — Encore 東京');
   const ids=await page.locator('.media-row').evaluateAll(rows=>rows.map(row=>Number((row as HTMLElement).dataset.id)));
   const covers=page.locator('.music-disc img');
   for(let i=0;i<2;i++) await expect.poll(()=>covers.nth(i).evaluate((img:HTMLImageElement)=>img.complete&&img.naturalWidth>0)).toBe(true);
   expect(await covers.nth(0).getAttribute('src')).not.toBe(await covers.nth(1).getAttribute('src'));
   const color=await covers.last().evaluate((img:HTMLImageElement)=>{const c=document.createElement('canvas');c.width=c.height=1;const x=c.getContext('2d')!;x.drawImage(img,0,0,1,1);return [...x.getImageData(0,0,1,1).data];});
   expect(color[0]).toBeGreaterThan(200);expect(color[2]).toBeLessThan(50); // red disc2 wins over blue ancestor/embedded
   if(index) {
    await followCredit(page,'M04 Guest',true,1);
    await expect(page.getByRole('heading',{name:'M04 Guest',exact:true})).toBeVisible();
    await expect(page.locator('.album-card').filter({hasText:title})).toBeVisible();
    await page.getByRole('button',{name:'← Back',exact:true}).click();
    await expect(page.locator('.media-row.selected')).toHaveAttribute('data-id',String(ids[1]));
   }
   for(const name of ['M04 A / B','M04 東京']) {
    await followCredit(page,name,index===1);
    await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
    await expect(page.locator('.album-card').filter({hasText:title})).toBeVisible();
    await page.getByRole('button',{name:'← Back',exact:true}).click();
    await expect(page.locator('.media-row.selected')).toHaveAttribute('data-id',String(ids[2]));
   }
   for(const [theme,width] of [['dark',1366],['light',390]] as const) {
    await settleMusicViewport(page,width);
    if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    await page.locator('.music-disc').last().scrollIntoViewIfNeeded();
    const shot=evidence.path(`m04-${folder}-${theme}-${width}.png`);await page.screenshot({path:shot,animations:'disabled'});artifacts.push(shot);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   }
   await settleMusicViewport(page,1366);
   // Enter the actual second disc row: heading count must never offset its index.
   await page.locator('.media-row').last().focus(); await page.keyboard.press('Enter');
   artifacts.push(await audible(page,`Music/M04/${folder}/Disc 2/1.flac`,`m04-${folder}-disc2-index`));
   await page.keyboard.press('Q');await expect(page.locator('#queue-panel')).toHaveClass(/open/);
   for(const [position,label] of [`M04 ${folder} 1-1`,`M04 ${folder} 1-2`,`M04 ${folder} 2-1`].entries()) {
    await expect(page.locator('#queue-panel .queue-track').nth(position)).toHaveAttribute('aria-label',`Play ${label}`);
   }
   await page.getByRole('button',{name:'Close queue',exact:true}).click();
   await page.keyboard.press('Escape');
   // Start from the last song of disc 1 and let natural playback cross discs.
   await page.locator('.media-row').nth(1).focus();await page.keyboard.press('Enter');
   artifacts.push(await audible(page,`Music/M04/${folder}/Disc 1/2.flac`,`m04-${folder}-disc1-last`));
   await expectPlaying(page,ids[2]);
   artifacts.push(await audible(page,`Music/M04/${folder}/Disc 2/1.flac`,`m04-${folder}-boundary`));
   await expect.poll(async()=>page.evaluate(async id=>{const {currentMediaId}=await import('/js/playback.js' as string);const audio=document.getElementById('media-audio') as HTMLAudioElement;return currentMediaId()===id&&audio.ended&&audio.paused;},ids[2]),{timeout:25000}).toBe(true);
   await page.keyboard.press('Escape');
   await page.locator('.media-row').last().focus();await page.keyboard.press('i');
   await expect(page.locator('#media-info-body')).toContainText('Disc subtitle');
   if(index) await expect(page.getByRole('rowheader',{name:'Compilation',exact:true})).toBeVisible();
   await page.keyboard.press('Escape'); await expect(page.locator('.media-row.selected')).toBeFocused();
  }
  await page.keyboard.press('/');await page.getByRole('searchbox',{name:'Search media'}).fill('r h l / M04');
  await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('r h l / M04');
  evidence.record({feature:'M04',scenario:'actual album/compilation groups, links, numbering, art and audible disc boundary',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M04',scenario:'album/compilation core',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M04 caller ACLs, real offline Retry and broken thumbnail rendering',async({page,context})=>{
 const admin=await request.newContext({baseURL:fixture.origin});let grant:any;
 try {
  await admin.post('/api/v1/auth/login',{data:fixture.admin});
  const visible=await (await page.request.get('/api/v1/music/albums')).json();
  expect(visible.filter((a:any)=>a.title===titles[1])).toHaveLength(1);
  const key=visible.find((a:any)=>a.title===titles[0]).album_key;
  const own=await (await page.request.get(`/api/v1/music/albums/${key}`)).json();
  const all=await (await admin.get(`/api/v1/music/albums/${key}`)).json();
  expect(own.discs).toHaveLength(2);expect(all.discs).toHaveLength(3);
  expect(JSON.stringify(own)).not.toContain('M04 Hidden');
  expect((await page.request.get(`/api/media/${fixture.mediaIds['Restricted/M04/hidden.flac']}/thumbnail`)).status()).toBe(403);
  await open(page,titles[0]);await page.getByRole('button',{name:'← Back to albums',exact:true}).click();
  await context.setOffline(true);
  try {
   await page.getByRole('button',{name:`Open album ${titles[0]} by M04 Ensemble`,exact:true}).click();
   await expect(page.getByRole('alert')).toContainText('Could not load albums');
  }finally{await context.setOffline(false);}
  await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(page.locator('.music-disc')).toHaveCount(2);
  await page.getByRole('button',{name:'← Back to albums',exact:true}).click();
  const users=await (await admin.get('/api/admin/users')).json();
  grant={set_id:fixture.setIds.Music,user_id:users.find((u:any)=>u.username===fixture.user.username).id,role:'viewer'};
  await admin.delete('/api/admin/permissions',{data:grant});
  await page.getByRole('button',{name:`Open album ${titles[0]} by M04 Ensemble`,exact:true}).click();
  await expect(page.getByRole('alert')).toContainText('unavailable');
  await admin.post('/api/admin/permissions',{data:grant});grant=null;
  await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(page.locator('.music-disc')).toHaveCount(2);
  await open(page,'M04 Missing Art');await expect(page.getByRole('img',{name:'No album artwork',exact:true})).toBeVisible();
  execFileSync('python3',[join(SUPPORT_DIR,'music_discs.py'),'art',fixture.dir,'broken']);
  try {
   await open(page,titles[0]);
   // Revalidate in a new document: the old document can retain the decoded
   // same-URL picture after a fixture file edit, without another image fetch.
   const coverId=fixture.mediaIds['Music/M04/Album/Disc 2/1.flac'];
   const invalidFetch=page.waitForResponse(response=>response.url().endsWith(`/api/media/${coverId}/thumbnail`) && response.request().resourceType()==='image');
   await page.reload();
   const response=await invalidFetch;expect(response.status()).toBe(200);
   expect(await response.text()).toBe('Not an image: M04 controlled broken cover');
   await expect(page.locator('.music-disc .album-placeholder')).toHaveCount(1);
  }finally{execFileSync('python3',[join(SUPPORT_DIR,'music_discs.py'),'art',fixture.dir,'initial']);}
  evidence.record({feature:'M04',scenario:'real offline/ACL Retry and actual missing/broken-image rendering boundary',result:'pass',data:{visible:own.discs,admin:all.discs}});
 }catch(error){evidence.record({feature:'M04',scenario:'boundaries',result:'fail',detail:String(error)});throw error;}
 finally{if(grant)await admin.post('/api/admin/permissions',{data:grant});await admin.dispose();}
});

test('M04 changed/cleared/failed-probe metadata and server restart preserve media/user state',async({page})=>{
 const admin=await request.newContext({baseURL:fixture.origin});
 const song=track(fixture,'Music/M04/Album/Disc 2/1.flac');
 const detail=async()=>{const r=await page.request.get(`/api/v1/media/${song.id}`);expect(r.ok()).toBe(true);return r.json();};
 const scan=async(state:string)=>{
  execFileSync('python3',[join(SUPPORT_DIR,'music_discs.py'),'retag',fixture.dir,state]);
  expect((await admin.post('/api/admin/rescan',{data:{}})).ok()).toBe(true);
  await expect.poll(async()=>{const p=await admin.get('/api/admin/scan-progress');expect(p.ok()).toBe(true);return (await p.json()).running;},{timeout:40000}).toBe(false);
 };
 try {
  await admin.post('/api/v1/auth/login',{data:fixture.admin});
  await page.request.post('/api/progress',{data:{media_id:song.id,position_seconds:4}});
  await page.request.post(`/api/media/${song.id}/favorite`);
  const before=await detail();
  await scan('changed');let changed=await detail();expect(changed.media.music.disc_subtitle).toBe('Changed disc 東京');expect(changed.media.music.compilation).toBe(true);
  await open(page,titles[0]);await expect(page.locator('.music-disc h2').last()).toHaveText('Disc 2 — Changed disc 東京');
  await scan('broken');changed=await detail();expect(changed.media.music.disc_subtitle).toBe('Changed disc 東京');expect(changed.media.music.compilation).toBe(true);
  await scan('cleared');const after=await detail();expect(after.media.music.disc_subtitle).toBeUndefined();expect(after.media.music.compilation).toBeUndefined();
  expect(after.media.id).toBe(before.media.id);expect(after.favorite).toBe(before.favorite);expect(after.progress).toEqual(before.progress);
  await open(page,titles[0]);await expect(page.locator('.music-disc h2').last()).toHaveText('Disc 2');
  await scan('initial');
  execFileSync('python3',[join(SUPPORT_DIR,'music_fixture.py'),'restart',fixture.dir]);
  await page.reload();await expect(page.locator('.music-disc h2').last()).toContainText('Disc 2 — Encore 東京');
  expect((await detail()).media.id).toBe(before.media.id);
  evidence.record({feature:'M04',scenario:'changed/cleared/failed-probe and same-DB server restart',result:'pass',data:{before,after}});
 }catch(error){evidence.record({feature:'M04',scenario:'state round trips',result:'fail',detail:String(error)});throw error;}
 finally{await scan('initial');await admin.dispose();}
});
