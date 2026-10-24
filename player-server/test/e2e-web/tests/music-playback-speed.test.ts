import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'M23 web playback speed';
const ALBUM = 'Aurora Lane - Night Transit (2021)';
test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set');
let fixture: MusicFixture;
let evidence: Evidence;
test.beforeAll(async ({ browser }, info) => { fixture = loadFixture(); evidence = new Evidence(fixture.evidenceDir, info.project.name, { browser: info.project.name, browser_version: browser.version(), channel: info.project.use.channel, server_version: fixture.serverVersion, checkout: fixture.checkout, run_id: fixture.runId }); });

test('M23: visible speed choices change real audio progression and persist', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const item = track(fixture, `Music/${ALBUM}/02 - Long Road Home.mp3`);
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);
    await playCard(page, item.title, item.id);
    const speed = page.getByLabel('Playback speed', { exact: true });
    await expect(speed).toBeVisible();
    await speed.selectOption('2');
    await expect(speed).toHaveValue('2');
    const start = (await audioState(page)).time;
    await page.waitForTimeout(1_000);
    const advanced = (await audioState(page)).time - start;
    expect(advanced, '2× playback advances about two media seconds per wall second').toBeGreaterThan(1.5);
    await page.keyboard.press('<');
    await expect(speed).toHaveValue('1.5');
    await speed.selectOption('1');
    await expect(page.locator('#media-audio')).toHaveJSProperty('playbackRate', 1);
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect.poll(() => page.evaluate(() => localStorage.getItem('player-audio-speed'))).toBe('1');
    data.media_seconds_at_2x = advanced;
    const shot = evidence.path('m23-speed-control.png');
    await page.screenshot({ path: shot }); artifacts.push(shot);
    evidence.record({ feature: FEATURE, scenario: '2× progression, keyboard step and normal reset persistence', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: '2× progression, keyboard step and normal reset persistence', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
