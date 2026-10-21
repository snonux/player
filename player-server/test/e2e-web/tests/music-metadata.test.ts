import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { Evidence, loadFixture, SUPPORT_DIR, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, playCard, captureOutput, assertValidCapture, mainSegment, selectByKeys } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => { await page.addInitScript(installAudioTap); await keyboardLogin(page, fixture.admin); });

async function info(page: any, title: string) {
  await selectByKeys(page, title);
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
  await expect(page.locator('#media-info-modal')).toHaveCSS('opacity','1');
  await expect(page.locator('#media-info-body')).toContainText('Filename');
}
async function row(page: any, label: string, value: string) {
  await expect(page.locator('#media-info-body tr').filter({has: page.getByRole('rowheader', {name: label, exact: true})}).locator('td')).toHaveText(value);
}

test('M01 tags, filename fallback, Unicode and real structured multivalues in Info and cards', async ({ page }, testInfo) => {
  const artifacts: string[] = [];
  const entries = [
    {path:'Music/Tag Verification/source_001.flac', fields: {Title:'A Different Music Title',Filename:'source_001.flac',Artist:'A & B; C/D','Album artist':'Album Ensemble',Album:'Metadata Album',Track:'4 / 9',Disc:'2 / 3',Date:'2024-02-29',Year:'2024',Genres:'Jazz'}},
    {path:'Music/Ünïcødé Artïst - 東京 Nights/02 - Партия для двоих.flac', fields:{Title:'Партия для двоих',Artist:'Ünïcødé Artïst, Mika Sørensen',Genres:'Électronique, Ambient'}},
    {path:'Music/Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg', fields:{Title:'Afterglow',Artist:'Dusk Choir',Disc:'2 / 2'}},
    {path:'Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a', fields:{Title:'Neon Rain',Artist:'Neon Coast',Disc:'1 / 2'}},
    {path:'Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus', fields:{Title:'Glass City',Artist:'Glass Avenue',Disc:'1 / 2'}},
    {path:'Music/Untagged/track_0042.mp3', fields:{Title:'track_0042',Filename:'track_0042.mp3'}},
    {path:'Music/Tag Verification/blank_title.mp3', fields:{Title:'blank_title',Filename:'blank_title.mp3',Album:'Partial Metadata'}},
  ];
  try {
    await page.locator('#media-grid').getByRole('button',{name:'Set Music',exact:true}).click();
    await page.getByRole('button',{name:'Folder Tag Verification',exact:true}).click();
    await expect(page.getByRole('button',{name:'A Different Music Title',exact:true})).toBeVisible();
    for (const entry of entries) {
      const item = track(fixture,entry.path);
      const folder = entry.path.slice('Music/'.length, entry.path.lastIndexOf('/'));
      await openFolder(page,fixture.setIds.Music,folder);
      await expect(page.getByRole('button',{name:item.title,exact:true})).toBeVisible();
      await info(page,item.title);
      for (const [label,value] of Object.entries(entry.fields)) await row(page,label,value);
      if (entry.path.includes('blank_title')) {
        for (const absent of ['Artist','Track','Disc','Year']) await expect(page.getByRole('rowheader',{name:absent,exact:true})).toHaveCount(0);
      }
      await page.keyboard.press('Escape');
      await expect(page.getByRole('button',{name:item.title,exact:true})).toBeFocused();
    }
    const long = track(fixture,'Music/Tag Verification/long_name.flac');
    for (const [theme,width] of [['dark',1366],['light',390]] as const) {
      await page.setViewportSize({width,height:860});
      await openFolder(page,fixture.setIds.Music,'Tag Verification');
      if (await page.locator('html').getAttribute('data-theme') !== theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
      await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
      const background = await page.evaluate(()=>getComputedStyle(document.body).backgroundColor);
      expect(background).toBe(theme === 'light' ? 'rgb(244, 245, 248)' : 'rgb(15, 17, 23)');
      const card=page.getByRole('button',{name:long.title,exact:true});
      await expect(card).toBeVisible();
      await expect(card.locator('.title')).toHaveAttribute('title',long.title);
      const path=evidence.path(`m01-${theme}-${width}.png`);await page.screenshot({path,fullPage:true,animations:'disabled'});artifacts.push(path);
      await info(page,long.title);
      await row(page,'Title',long.title);
      const details=evidence.path(`m01-info-${theme}-${width}.png`);await page.screenshot({path:details,animations:'disabled'});artifacts.push(details);
      await page.keyboard.press('Escape');
    }
    const item=track(fixture,'Music/Tag Verification/source_001.flac');
    await playCard(page,item.title,item.id);
    const out=await captureOutput(page,2000,evidence.path('m01-different-title-audio.wav'));
    assertValidCapture(out);const segment=mainSegment(out.analysis);
    expect(segment?.duration_s).toBeGreaterThan(1);
    expect(Math.abs(segment!.tone_hz!-item.tone_hz[0])).toBeLessThan(5);
    artifacts.push(out.wav);
    evidence.record({feature:'M01',scenario:'titles, tags, fallbacks, Unicode, multivalues, themes, focus and audible source',result:'pass',artifacts});
  } catch(error) {
    evidence.record({feature:'M01',scenario:'titles, tags, fallbacks, Unicode, multivalues, themes, focus and audible source',result:'fail',detail:String(error),artifacts});throw error;
  }
});

test('M01 rescan changes and removes tags in place while preserving saved state and filename', async ({page})=>{
 const item=track(fixture,'Music/Tag Verification/source_001.flac');
 const detail=async()=> (await (await page.request.get(`/api/v1/media/${item.id}`)).json());
 const retag=async(state:string,title:string)=>{
  execFileSync('python3',[join(SUPPORT_DIR,'music_metadata.py'),fixture.dir,state]);
  expect((await page.request.post('/api/admin/rescan',{data:{}})).ok()).toBe(true);
  await expect.poll(async()=>(await detail()).media.music?.title||'').toBe(title);
  const refreshed=await detail();
  const source=await page.request.head(`/api/media/${item.id}/download`);
  expect(source.ok()).toBe(true);
  expect(Number(source.headers()['content-length'])).toBeGreaterThan(0);
  expect(refreshed.media.file_size_bytes).toBe(Number(source.headers()['content-length']));
  const downloaded=await page.request.get(`/api/media/${item.id}/download`);
  expect(downloaded.ok()).toBe(true);
  expect((await downloaded.body()).byteLength).toBe(refreshed.media.file_size_bytes);
  expect(downloaded.headers()['content-disposition']).toContain(item.file_name);
 };
 try {
  expect((await page.request.post(`/api/media/${item.id}/favorite`)).ok()).toBe(true);
  expect((await page.request.post('/api/progress',{data:{media_id:item.id,position_seconds:4}})).ok()).toBe(true);
  const before=await detail();
  expect(before.progress.position_seconds).toBe(4);
  const initialList=await page.request.get(`/api/media?set_id=${fixture.setIds.Music}`);
  expect(initialList.ok()).toBe(true);
  const initialCount=(await initialList.json()).length;
  await retag('changed','Changed 東京 Title');
  await openFolder(page,fixture.setIds.Music,'Tag Verification');
  await info(page,'Changed 東京 Title');await row(page,'Filename',item.file_name);await row(page,'Artist','New Artist');
  await page.keyboard.press('Escape');
  await page.keyboard.press('/');await page.getByRole('searchbox',{name:'Search media'}).fill('Changed 東京 Title');
  await expect(page.getByRole('button',{name:'Changed 東京 Title',exact:true})).toBeVisible();
  await retag('cleared','');
  await openFolder(page,fixture.setIds.Music,'Tag Verification');
  await info(page,'source_001');await row(page,'Filename',item.file_name);
  await expect(page.getByRole('rowheader',{name:'Artist',exact:true})).toHaveCount(0);
  await page.keyboard.press('Escape');
  const after=await detail();
  expect(after.media.id).toBe(before.media.id);expect(after.media.play_count).toBe(before.media.play_count);
  expect(after.favorite).toBe(before.favorite);expect(after.progress).toEqual(before.progress);
  expect(after.media.rel_path).toBe(before.media.rel_path);
  const listed=await page.request.get(`/api/media?set_id=${fixture.setIds.Music}`);
  expect(listed.ok()).toBe(true);
  const all=await listed.json();
  expect(all).toHaveLength(initialCount);
  const copies=all.filter((m:any)=>m.rel_path===before.media.rel_path);
  expect(copies).toHaveLength(1);
  expect(copies[0].id).toBe(item.id);
  evidence.record({feature:'M01',scenario:'rescan change/removal, title search, unchanged identity/favorite/progress',result:'pass',data:{before,after}});
 } catch(error) { evidence.record({feature:'M01',scenario:'rescan change/removal, title search, unchanged identity/favorite/progress',result:'fail',detail:String(error)});throw error;
 } finally { await retag('initial',item.title); }
});
