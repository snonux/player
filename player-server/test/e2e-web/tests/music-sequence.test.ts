import { test, expect } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, playCard, expectPlaying, captureOutput, assertValidCapture, mainSegment, audioState, resetProgress } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
const album = 'Aurora Lane - Night Transit (2021)';
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.admin);
});

test('M12 three natural completions, real track controls, images skipped and progress retained', async ({ page }) => {
  const songs = ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac']
    .map(name => track(fixture, `Music/${album}/${name}`));
  const artifacts: string[] = [];
  const snapshots: any[] = [];
  try {
    for (const song of songs) await resetProgress(page, song.id);
    await openFolder(page, fixture.setIds.Music, album);
    await playCard(page, songs[0].title, songs[0].id);
    for (const song of songs) {
      await expectPlaying(page, song.id);
      const output = await captureOutput(page, 1500, evidence.path(`m12-natural-${song.id}.wav`));
      assertValidCapture(output);
      const segment = mainSegment(output.analysis);
      expect(segment?.duration_s).toBeGreaterThan(0.8);
      expect(Math.abs(segment!.tone_hz! - song.tone_hz[0])).toBeLessThan(5);
      artifacts.push(output.wav);
      snapshots.push({id:song.id, state:await audioState(page), audio:segment});
      await expect.poll(async () => (await audioState(page)).src, { timeout: 20_000 })
        .not.toMatch(new RegExp(`/media/${song.id}/`));
      const detail = await page.request.get(`/api/v1/media/${song.id}`);
      const body = await detail.json();
      expect(body.progress?.finished, `completion saved for ${song.id}`).toBe(true);
      snapshots.push({id:song.id, progress:body.progress});
    }
    // Crossing the folder edge must keep playable media, never cover.jpg.
    expect((await audioState(page)).src).toMatch(/\/media\/\d+\/(stream|compat)/);
    await expect(page.locator('#media-image')).toHaveClass(/hidden/);
    // Finished Sets songs restart at zero. Previous from the first folder
    // wraps to the last playable set item; Next wraps back to the first song.
    await openFolder(page,fixture.setIds.Music,album);
    const completed=page.getByRole('button',{name:songs[0].title,exact:true});
    await completed.hover();await completed.locator('button[data-action="play"]').click();
    await expectPlaying(page,songs[0].id);
    expect((await audioState(page)).time).toBeLessThan(4);
    await page.getByRole('button',{name:'Previous track',exact:true}).click();
    const last=track(fixture,'Music/Untagged/track_0042.mp3');
    await expectPlaying(page,last.id);
    await page.getByRole('button',{name:'Next track',exact:true}).click();
    await expectPlaying(page,songs[0].id);
    const wrapped=await captureOutput(page,1500,evidence.path('m12-finished-replay-set-wrap.wav'));
    assertValidCapture(wrapped);const wrappedSegment=mainSegment(wrapped.analysis);
    expect(wrappedSegment?.duration_s).toBeGreaterThan(0.8);
    expect(Math.abs(wrappedSegment!.tone_hz!-songs[0].tone_hz[0])).toBeLessThan(5);
    artifacts.push(wrapped.wav);snapshots.push({wrap:songs[0].id,audio:wrappedSegment});
    await openFolder(page, fixture.setIds.Music, album);
    for (const song of songs) await resetProgress(page, song.id);
    await playCard(page, songs[0].title, songs[0].id);
    await page.getByRole('button', {name:'Next track',exact:true}).click();
    await expectPlaying(page,songs[1].id);
    await page.getByRole('button', {name:'Previous track',exact:true}).click();
    await expectPlaying(page,songs[0].id);
    await page.keyboard.press('Shift+N');
    await expectPlaying(page,songs[1].id);
    await page.keyboard.press('Shift+P');
    await expectPlaying(page,songs[0].id);
    await page.keyboard.press('/');
    await page.keyboard.type('Np');
    await expect(page.getByRole('searchbox',{name:'Search media'})).toHaveValue('Np');
    expect((await audioState(page)).src).toMatch(new RegExp(`/media/${songs[0].id}/`));
    await page.keyboard.press('Escape');
    if ((await page.locator('#player').getAttribute('class'))?.includes('minimized')) {
      await page.keyboard.press('Shift+C');
    }
    await expect(page.locator('#player')).not.toHaveClass(/minimized/);
    for (const [theme,width] of [['dark',1366],['light',390]] as const) {
      await page.setViewportSize({width,height:844});
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
      const shot=evidence.path(`m12-${theme}-${width}.png`);
      await page.screenshot({path:shot,fullPage:true});artifacts.push(shot);
    }
    evidence.record({feature:'M12',scenario:'natural completion, track controls, folder image skip, progress, typing and themes',result:'pass',detail:JSON.stringify(snapshots),artifacts});
  } catch(error) {
    evidence.record({feature:'M12',scenario:'natural completion, track controls, folder image skip, progress, typing and themes',result:'fail',detail:String(error),artifacts});throw error;
  }
});

test('M12 saved resume, failed target visible and Retry, keyboard focus and rapid track input', async ({ page }) => {
  const first=track(fixture,`Music/${album}/01 - First Light.mp3`);
  const second=track(fixture,`Music/${album}/02 - Long Road Home.mp3`);
  const artifacts: string[]=[];
  try {
    await resetProgress(page,second.id);
    await page.request.post('/api/v1/progress',{data:{media_id:first.id,position_seconds:4}});
    await openFolder(page,fixture.setIds.Music,album);
    const card=page.getByRole('button',{name:first.title,exact:true});
    await card.hover();await card.locator('button[data-action="play"]').click();
    await expectPlaying(page,first.id);
    expect((await audioState(page)).time).toBeGreaterThanOrEqual(4);
    await page.route(`**/media/${second.id}/stream`,route=>route.fulfill({status:403,body:'Forbidden'}));
    await page.getByRole('button',{name:'Next track',exact:true}).click();
    await expect(page.locator('#toast')).toContainText('Cannot play');
    expect((await audioState(page)).src).toMatch(new RegExp(`/media/${second.id}/`));
    const shot=evidence.path('m12-failed-target.png');await page.screenshot({path:shot});artifacts.push(shot);
    await page.unroute(`**/media/${second.id}/stream`);
    await page.locator('#btn-play').click();
    await expectPlaying(page,second.id);
    await page.keyboard.press('i');
    await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
    await page.keyboard.press('Escape');
    await expect(page.locator('#btn-play')).toBeFocused();
    await page.keyboard.press('Shift+P');
    await expectPlaying(page,first.id);
    await page.keyboard.press('Shift+N');
    await page.keyboard.press('Shift+P');
    await expectPlaying(page,first.id);
    evidence.record({feature:'M12',scenario:'saved resume, failed target and Retry, keyboard focus, rapid input',result:'pass',artifacts});
  } catch(error) {
    evidence.record({feature:'M12',scenario:'saved resume, failed target and Retry, keyboard focus, rapid input',result:'fail',detail:String(error),artifacts});throw error;
  }
});
