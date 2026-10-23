import {test, expect, request, type Page} from '@playwright/test';
import {execFileSync} from 'node:child_process';
import {join} from 'node:path';
import {Evidence, loadFixture, track, SUPPORT_DIR, type MusicFixture} from './helpers/music-fixture';
import {keyboardLogin, openFolder, selectByKeys, playCard, audioState, captureOutput, assertValidCapture, mainSegment} from './helpers/music-ui';
import {installAudioTap} from './helpers/audio-tap';
import {settleMusicViewport} from './helpers/music-viewport';

const path='Music/M06 Information/technical.flac';
const expected:Record<string,string>={Tempo:'123.5 BPM (beats/min)','Sample rate':'48 kHz (48000 Hz)',
 'Bit depth':'24 bits',Channels:'2 (stereo)','Track ReplayGain':'0 dB','Album ReplayGain':'-6.25 dB',
 'Track peak':'0.89 (1 = full scale)','Album peak':'1 (1 = full scale)'};
let fixture:MusicFixture, evidence:Evidence;
test.skip(!process.env.PLAYER_MUSIC_FIXTURE,'Requires marked disposable fixture with music_info.py augmentation');
test.beforeAll(async({browser},info)=>{fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,
 {browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});});
test.beforeEach(async({page})=>{test.skip(!fixture.mediaIds[path],'Run music_info.py once at fixture setup');await page.addInitScript(installAudioTap);await keyboardLogin(page,fixture.admin);});
const modal=(page:Page)=>page.locator('#media-info-modal');
const group=(page:Page)=>page.locator('.media-info-music').filter({has:page.getByText('Advanced audio',{exact:true})});
async function openInfo(page:Page) {await page.keyboard.press('i');await expect(modal(page)).toHaveClass(/open/);await expect(page.locator('#media-info-body')).toContainText('Filename');}
async function tabExpand(page:Page) {
 const summary=group(page).locator('summary');
 for(let n=0;n<40;n++) {if(await summary.evaluate(el=>el===document.activeElement)) break;await page.keyboard.press('Tab');}
 await expect(summary).toBeFocused();await page.keyboard.press('Space');await expect(group(page)).toHaveAttribute('open','');
}
async function row(page:Page,label:string,value:string) {
 await expect(page.locator('#media-info-body tr').filter({has:page.getByRole('rowheader',{name:label,exact:true})}).locator('td')).toHaveText(value);
}
async function continues(page:Page,before:Awaited<ReturnType<typeof audioState>>) {
 await expect.poll(async()=>(await audioState(page)).time).toBeGreaterThan(before.time+.5);
 const after=await audioState(page);expect(after.src).toBe(before.src);expect(after.paused).toBe(false);return after;
}

test('M06 source/tag units and disclosures preserve actual playback, keyboard focus and themes',async({page})=>{
 const artifacts:string[]=[];
 try {
  const item=track(fixture,path);
  await openFolder(page,fixture.setIds.Music,'M06 Information');await selectByKeys(page,item.title);
  await playCard(page,item.title,item.id);
  for(const [theme,width] of [['dark',1366],['light',390]] as const) {
   await settleMusicViewport(page,width);
   if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   // Actual card focus is the dialog opener, independently of ongoing playback.
   await page.getByRole('button',{name:item.title,exact:true}).focus();
   const before=await audioState(page);await openInfo(page);
   await expect(group(page)).not.toHaveAttribute('open','');await row(page,'Tempo',expected.Tempo);
   await tabExpand(page);for(const [label,value] of Object.entries(expected)) await row(page,label,value);
   await expect(group(page)).toContainText('Indexed original file');
   await page.keyboard.press('j');await page.keyboard.press('k');
   await group(page).scrollIntoViewIfNeeded();const shot=evidence.path(`m06-${theme}-${width}.png`);await page.screenshot({path:shot});artifacts.push(shot);
   const out=await captureOutput(page,2000,evidence.path(`m06-${theme}-info-source.wav`));assertValidCapture(out);
   expect(Math.abs(mainSegment(out.analysis)!.tone_hz!-item.tone_hz[0])).toBeLessThan(5);artifacts.push(out.wav);
   await continues(page,before);await page.keyboard.press('Escape');await expect(modal(page)).not.toHaveClass(/open/);
   await expect(page.getByRole('button',{name:item.title,exact:true})).toBeFocused();await continues(page,before);
  }
  // Pointer disclosure and long credits use the same player; no second source.
  await openInfo(page);await group(page).locator('summary').click();
  const credits=page.locator('.media-info-music').filter({has:page.getByText('Additional music details',{exact:true})});await credits.locator('summary').click();
  await expect(credits).toContainText('Long technical credit participant');await credits.scrollIntoViewIfNeeded();
  const shot=evidence.path('m06-long-credits.png');await page.screenshot({path:shot});artifacts.push(shot);await page.keyboard.press('Escape');
  await page.keyboard.press('/');await page.getByRole('searchbox',{name:'Search media'}).fill('M06 Information');
  await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('M06 Information');
  evidence.record({feature:'M06',scenario:'source/tag units, live playback, keyboard/pointer/focus/themes',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M06',scenario:'live information',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M06 missing lossy depth, actual offline Info/retry, restart and permission boundaries',async({page,context})=>{
 try {
  const item=track(fixture,path), lossy=track(fixture,'Music/M06 Information/lossy.mp3');
  await openFolder(page,fixture.setIds.Music,'M06 Information');await selectByKeys(page,lossy.title);await openInfo(page);await tabExpand(page);
  await expect(group(page).getByRole('rowheader',{name:'Bit depth',exact:true})).toHaveCount(0);await row(page,'Sample rate','48 kHz (48000 Hz)');await page.keyboard.press('Escape');
  await selectByKeys(page,item.title);await playCard(page,item.title,item.id);await page.getByRole('button',{name:item.title,exact:true}).focus();
  const before=await audioState(page);
  await context.setOffline(true);
  try {await page.keyboard.press('i');await expect(page.locator('#media-info-body .error-message')).toBeVisible();await continues(page,before);await page.keyboard.press('Escape');await expect(page.getByRole('button',{name:item.title,exact:true})).toBeFocused();}
  finally {await context.setOffline(false);}
  await openInfo(page);await tabExpand(page);await row(page,'Track ReplayGain','0 dB');await continues(page,before);await page.keyboard.press('Escape');
  const snapshot=await (await page.request.get(`/api/v1/media/${item.id}`)).json();
  execFileSync('python3',[join(SUPPORT_DIR,'music_fixture.py'),'restart',fixture.dir]);
  await page.reload();await selectByKeys(page,item.title);await openInfo(page);await tabExpand(page);await row(page,'Bit depth','24 bits');await page.keyboard.press('Escape');
  const after=await (await page.request.get(`/api/v1/media/${item.id}`)).json();expect(after.media.id).toBe(snapshot.media.id);expect(after.media.music).toEqual(snapshot.media.music);
  await page.getByRole('button',{name:'Log out',exact:true}).click();await keyboardLogin(page,fixture.user);
  const hidden=track(fixture,'Restricted/M06 Information/hidden.flac');const denied=await page.request.get(`/api/v1/media/${hidden.id}`);expect(denied.status()).toBe(403);expect(await denied.text()).not.toContain('replaygain');
  const admin=await request.newContext({baseURL:fixture.origin});let permission:any;
  try {
   expect((await admin.post('/api/v1/auth/login',{data:fixture.admin})).ok()).toBe(true);
   const users=await(await admin.get('/api/v1/admin/users')).json();
   permission={set_id:fixture.setIds.Music,user_id:users.find((user:any)=>user.username===fixture.user.username).id};
   await openFolder(page,fixture.setIds.Music,'M06 Information');await selectByKeys(page,item.title);await playCard(page,item.title,item.id);await page.getByRole('button',{name:item.title,exact:true}).focus();
   const playing=await audioState(page);
   expect((await admin.delete('/api/v1/admin/permissions',{data:permission})).ok()).toBe(true);
   await page.keyboard.press('i');await expect(page.locator('#media-info-body .error-message')).toBeVisible();await expect(group(page)).toHaveCount(0);await continues(page,playing);await page.keyboard.press('Escape');
   await expect(page.getByRole('button',{name:item.title,exact:true})).toBeFocused();
  } finally {
   if(permission) expect((await admin.post('/api/v1/admin/permissions',{data:{...permission,role:'viewer'}})).ok()).toBe(true);
   await admin.dispose();
  }
  await openInfo(page);await tabExpand(page);await row(page,'Bit depth','24 bits');await page.keyboard.press('Escape');
  evidence.record({feature:'M06',scenario:'lossy omission/offline recovery/restart/actual denied Info',result:'pass'});
 }catch(error){evidence.record({feature:'M06',scenario:'information boundaries',result:'fail',detail:String(error)});throw error;}
});
