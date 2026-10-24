import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, expectPlaying, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'M67 translated music UI';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set (see music-e2e-verification.md)');

let fixture: MusicFixture;
let evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser: info.project.name, browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build_revision: fixture.buildRevision,
    checkout: fixture.checkout, origin: fixture.origin, run_id: fixture.runId,
  });
});

test('M67: device-local German labels switch live, preserve playback and persist after restart', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const song = track(fixture, 'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3');
    await page.setViewportSize({ width: 390, height: 844 });
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, 'Aurora Lane - Night Transit (2021)');
    await playCard(page, song.title, song.id);
    await expectPlaying(page, song.id);
    const before = await audioState(page);

    const settings = page.locator('#settings-toggle');
    await settings.click();
    const language = page.locator('#language-select');
    await language.selectOption('de');
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await expect(settings).toHaveText('Einstellungen');
    await expect(page.locator('#language-status')).toContainText('Sprache');
    expect((await audioState(page)).src).toBe(before.src);
    expect((await audioState(page)).paused).toBe(false);
    const germanShot = evidence.path('m67-german-narrow.png');
    await page.screenshot({ path: germanShot });
    artifacts.push(germanShot);

    await page.keyboard.press('Escape');
    await expect(settings).toBeFocused();
    await page.reload({ waitUntil: 'domcontentloaded' });
    await settings.click();
    await expect(language).toHaveValue('de');
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await language.selectOption('en');
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(settings).toHaveText('Settings');
    data.persisted_language = 'de';
    data.metadata_remained_verbatim = song.title;
    evidence.record({ feature: FEATURE, scenario: 'narrow live German/English switch, playback preservation and restart persistence', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'narrow live German/English switch, playback preservation and restart persistence', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
