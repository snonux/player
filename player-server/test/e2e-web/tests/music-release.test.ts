import { settleMusicViewport } from './helpers/music-viewport';
import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { Evidence, loadFixture, SUPPORT_DIR, track, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, openFolder, selectByKeys, playCard, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';
import { installAudioTap } from './helpers/audio-tap';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable --release-metadata fixture');
let fixture: MusicFixture;
let evidence: Evidence;
const path = 'Music/Release Metadata/release.flac';
const expected: Record<string,string> = {
 'Release date':'2025-04','Original date':'1979','Recording date':'1978-11-03',
 Version:'Expanded / Live','Release type':'album\nlive',Label:'Label A; B\nLabel 東京',
 'Catalog number':'CAT/01\nCAT,02',Media:'CD\nDigital',Mood:'Reflective\nBright',
 Grouping:'Suite / One',Producer:'Producer A / B\nProducer C','tape op':'Op A; B\nOp 東京 / C',
 Comment:'<b>Preserved text</b>\n第二行 · ' + 'Long release comment 東京 '.repeat(8).trimEnd(),
};
test.beforeAll(async ({browser},info)=>{
 fixture=loadFixture();
 evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});
});
test.beforeEach(async({page})=>{ test.skip(!fixture.mediaIds[path], 'Start fixture with --release-metadata'); await page.addInitScript(installAudioTap); await keyboardLogin(page,fixture.admin); });
async function info(page:any) {
 const item=track(fixture,path);
 await openFolder(page,fixture.setIds.Music,'Release Metadata');
 await selectByKeys(page,item.title); await page.keyboard.press('i');
 await expect(page.locator('#media-info-body')).toContainText('Filename');
 try { await expect(page.locator('#media-info-modal')).toHaveCSS('opacity','1'); }
 catch(error) { await renderingDiagnostic(page,'Info opacity did not settle'); throw error; }
}
async function renderingDiagnostic(page:any, reason:string) {
 const data=await page.evaluate(()=>{
  const modal=document.getElementById('media-info-modal');
  return {visibility:document.visibilityState,focused:document.hasFocus(),viewport:[innerWidth,innerHeight],
   modal:modal?{className:modal.className,opacity:getComputedStyle(modal).opacity,
    animations:modal.getAnimations().map(animation=>({state:animation.playState,time:animation.currentTime}))}:null};
 });
 evidence.record({feature:'M05',scenario:'browser rendering readiness diagnostic',result:'fail',detail:reason,data});
}
async function settleViewport(page:any,width:number) {
 await settleMusicViewport(page,width,reason=>renderingDiagnostic(page,reason));
}
async function tabTo(page:any, target:any) {
 for(let n=0;n<40;n++) { if(await target.evaluate((el:any)=>el===document.activeElement)) return; await page.keyboard.press('Tab'); }
 throw new Error('New disclosure was not reachable through real Tab navigation');
}
async function expand(page:any, title:string, key='Enter') {
 const summary=page.locator('.media-info-music summary').filter({hasText:title});
 await tabTo(page,summary); await expect(summary).toBeFocused(); await page.keyboard.press(key);
 await expect(summary.locator('..')).toHaveAttribute('open','');
}
async function row(page:any,label:string,value:string) {
 await expect(page.locator('#media-info-body tr').filter({has:page.getByRole('rowheader',{name:label,exact:true})}).locator('td')).toHaveText(value);
}

test('M05 organized rich release details, real disclosure keys, themes and source',async({page,context})=>{
 const artifacts:string[]=[];
 try {
  for(const [theme,width] of [['dark',1366],['light',390]] as const) {
   await settleViewport(page,width);
   await info(page);
   await row(page,'Tagged date','2024'); await row(page,'Year','2024');
   await expand(page,'Release details'); await expand(page,'Additional music details','Space');
   for(const [label,value] of Object.entries(expected)) await row(page,label,value);
   expect(await page.locator('#media-info-body td b').count()).toBe(0);
   await page.keyboard.press('Escape');
   await expect(page.getByRole('button',{name:track(fixture,path).title,exact:true})).toBeFocused();
   if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
   await expect(page.locator('body')).toHaveCSS('background-color',theme==='light'?'rgb(244, 245, 248)':'rgb(15, 17, 23)');
   await info(page);
   await page.getByText('Release details',{exact:true}).click();
   await page.getByText('Additional music details',{exact:true}).click();
   for(const [section,label] of [['release','Original date'],['comment','Comment'],['credits','tape op']]) {
    await page.locator('#media-info-body tr').filter({has:page.getByRole('rowheader',{name:label,exact:true})}).scrollIntoViewIfNeeded();
    const shot=evidence.path(`m05-${theme}-${width}-${section}.png`);await page.screenshot({path:shot,animations:'disabled'});artifacts.push(shot);
   }
   await page.keyboard.press('Escape');
  }
  // Keep the already selected online card; offline Info must fetch and fail
  // visibly rather than retain an earlier release snapshot or navigate away.
  await info(page);
  await page.keyboard.press('Escape');
  await context.setOffline(true);
  try {
   await page.keyboard.press('i');
   await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
   await expect(page.locator('#media-info-body .error-message')).toBeVisible();
   await expect(page.locator('#media-info-body .error-message')).not.toHaveText('');
   await expect(page.locator('#media-info-body .media-info-music')).toHaveCount(0);
   const offline=evidence.path('m05-offline-info-error.png');
   await page.screenshot({path:offline,animations:'disabled'});artifacts.push(offline);
   await page.keyboard.press('Escape');
   await expect(page.getByRole('button',{name:track(fixture,path).title,exact:true})).toBeFocused();
  } finally { await context.setOffline(false); }
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-body')).toContainText('Filename');
  await expect(page.locator('#media-info-body .error-message')).toHaveCount(0);
  await expand(page,'Release details');
  await row(page,'Release date','2025-04');await row(page,'Original date','1979');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button',{name:track(fixture,path).title,exact:true})).toBeFocused();
  await page.keyboard.press('/'); await page.getByRole('searchbox',{name:'Search media'}).fill('Release Metadata 東京');
  await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('Release Metadata 東京');
  const item=track(fixture,path); await openFolder(page,fixture.setIds.Music,'Release Metadata');await playCard(page,item.title,item.id);
  const out=await captureOutput(page,2000,evidence.path('m05-source.wav'));assertValidCapture(out);
  expect(mainSegment(out.analysis)?.duration_s).toBeGreaterThan(1);expect(Math.abs(mainSegment(out.analysis)!.tone_hz!-item.tone_hz[0])).toBeLessThan(5);artifacts.push(out.wav);
  evidence.record({feature:'M05',scenario:'organized rich release details, keyboard/pointer/themes/source',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M05',scenario:'organized rich release details, keyboard/pointer/themes/source',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M05 scan/removal/failed probe, permission and restart round trips',async({page})=>{
 const item=track(fixture,path);
 const detail=async()=>{const response=await page.request.get(`/api/v1/media/${item.id}`);expect(response.ok()).toBe(true);return response.json();};
 let restored=false;
 const rescan=async(state:string)=>{
  execFileSync('python3',[join(SUPPORT_DIR,'music_release.py'),fixture.dir,state]);
  expect((await page.request.post('/api/admin/rescan',{data:{}})).ok()).toBe(true);
  await expect.poll(async()=>{const progress=await page.request.get('/api/admin/scan-progress');expect(progress.ok()).toBe(true);return (await progress.json()).running;},{timeout:30000}).toBe(false);
 };
 try{
  await page.request.post(`/api/media/${item.id}/favorite`);await page.request.post('/api/progress',{data:{media_id:item.id,position_seconds:4}});
  const before=await detail();
  await rescan('changed');expect((await detail()).media.music.release_date).toBe('2026-06');
  await info(page);await expand(page,'Release details');await row(page,'Label','Changed Label');await page.keyboard.press('Escape');
  await rescan('broken');expect((await detail()).media.music.release_date).toBe('2026-06');
  await rescan('cleared');const after=await detail();expect(after.media.music.release_date).toBeUndefined();expect(after.media.music.credits).toBeUndefined();
  expect(after.media.id).toBe(before.media.id);expect(after.favorite).toBe(before.favorite);expect(after.progress).toEqual(before.progress);
  await info(page);await expect(page.locator('.media-info-music summary').filter({hasText:/^(Release details|Additional music details)$/})).toHaveCount(0);
  await expand(page,'Advanced audio');await row(page,'Sample rate','44.1 kHz (44100 Hz)');await row(page,'Bit depth','16 bits');await page.keyboard.press('Escape');
  await rescan('initial');restored=true;
  execFileSync('python3',[join(SUPPORT_DIR,'music_fixture.py'),'restart',fixture.dir]);
  await page.reload();await info(page);await expand(page,'Release details');await row(page,'Original date','1979');await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Log out',exact:true}).click();await keyboardLogin(page,fixture.user);
  const hidden=track(fixture,'Restricted/Release Metadata/hidden.flac');const denied=await page.request.get(`/api/v1/media/${hidden.id}`);expect(denied.status()).toBe(403);expect(await denied.text()).not.toContain('1979');
  evidence.record({feature:'M05',scenario:'rescan/removal/failed probe/restart/ACL round trips',result:'pass',data:{before,after}});
 }catch(error){evidence.record({feature:'M05',scenario:'rescan/removal/failed probe/restart/ACL round trips',result:'fail',detail:String(error)});throw error;}
 finally{if(!restored) await rescan('initial');}
});
