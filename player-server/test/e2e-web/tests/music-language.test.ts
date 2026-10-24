import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, assertValidCapture, captureOutput, expectPlaying, keyboardLogin, mainSegment, openFolder, openSidebarMusic, playCard } from './helpers/music-ui';
import { installAudioTap } from './helpers/audio-tap';

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

test('M67: mounted playlist drafts, counts and storage/device fallback in installed browsers', async ({ page, context }) => {
  const name = `M67 verbatim Play Deutsch ${Date.now()}`;
  const comment = 'Original comment: Save changes / Änderungen';
  let id: string | undefined;
  try {
    await page.setViewportSize({ width: 390, height: 844 });
    await keyboardLogin(page, fixture.user);
    await openSidebarMusic(page, 'Playlists');
    await page.locator('[data-create]').click();
    const dialog = page.getByRole('dialog');
    await dialog.locator('[name=name]').fill(name);
    await page.keyboard.press('Tab');
    await page.keyboard.type(comment);
    const settingsPage = await context.newPage();
    await settingsPage.goto('/');
    await settingsPage.locator('#settings-toggle').click();
    await settingsPage.locator('#language-select').selectOption('de');
    await page.bringToFront();
    await expect(dialog.locator('h2')).toHaveText('Wiedergabeliste erstellen');
    await expect(dialog.locator('[name=name]')).toHaveValue(name);
    await expect(dialog.locator('[name=comment]')).toHaveValue(comment);
    await expect(dialog.locator('[name=comment]')).toBeFocused();
    await dialog.locator('[type=submit]').focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-totals]')).toContainText('0 Titel');
    id = new URL(page.url()).searchParams.get('playlist') || undefined;
    if (!id) {
      const lists = await (await page.request.get('/api/v1/playlists')).json();
      id = lists.find((p: any) => p.name === name)?.playlist_id;
    }
    expect(id).toBeTruthy();
    await page.locator('[data-add-songs]').click();
    const choices = dialog.locator('[data-choices] input');
    await expect(choices.first()).toBeVisible();
    await choices.nth(0).check();
    await dialog.locator('[data-save]').click();
    await expect(page.locator('[data-totals]')).toContainText('1 Titel');
    await page.locator('[data-add-songs]').click();
    await expect(choices.first()).toBeVisible();
    await choices.nth(1).check();
    await dialog.locator('[data-save]').click();
    await expect(page.locator('[data-totals]')).toContainText('2 Titel');
    await expect(dialog).toBeHidden();
    await page.locator('[data-back]').scrollIntoViewIfNeeded();
    await page.waitForTimeout(250); // let the closing dialog/theme transition settle
    const shots: string[] = [];
    for (const theme of ['dark', 'light']) {
      if ((await page.locator('html').getAttribute('data-theme') || 'dark') !== theme) {
        await page.locator('#theme-toggle').click();
      }
      const shot = evidence.path(`m67-playlist-german-narrow-${theme}.png`);
      await page.screenshot({ path: shot, animations: 'disabled' });
      shots.push(shot);
    }
    await page.locator('[data-edit]').click();
    await dialog.locator('[name=comment]').fill(comment + ' edited');
    await settingsPage.locator('#language-select').selectOption('en');
    await expect(dialog.locator('h2')).toHaveText('Edit playlist');
    await expect(dialog.locator('[name=comment]')).toHaveValue(comment + ' edited');
    await dialog.locator('[type=submit]').click();
    await expect(page.locator('.playlist-comment').first()).toHaveText(comment + ' edited');
    const saved = await (await page.request.get(`/api/v1/playlists/${id}`)).json();
    expect(saved.name).toBe(name);
    expect(saved.comment).toBe(comment + ' edited');
    expect(saved.songs).toHaveLength(2);
    await settingsPage.evaluate(() => {
      Object.defineProperty(navigator, 'language', { configurable: true, value: 'fr-FR' });
    });
    await settingsPage.locator('#language-select').selectOption('system');
    await expect(settingsPage.locator('html')).toHaveAttribute('lang', 'en');
    await settingsPage.evaluate(() => {
      const original = Storage.prototype.setItem;
      Storage.prototype.setItem = function(key, value) {
        if (key === 'player.language.v1') throw new DOMException('Test device storage unavailable', 'QuotaExceededError');
        return original.call(this, key, value);
      };
    });
    await settingsPage.locator('#language-select').selectOption('de');
    await expect(settingsPage.locator('#language-status')).toContainText('Sitzung');
    await expect(settingsPage.locator('html')).toHaveAttribute('lang', 'de');
    evidence.record({ feature: FEATURE, scenario: 'real playlist draft live switch, keyboard save, 0/1/2 totals, verbatim authoritative save; unsupported navigator language and injected storage exception', result: 'pass', data: { playlist_id: id, song_count: saved.songs.length, storage_failure: 'real browser Storage.setItem fault injection', unsupported_locale: 'navigator.language fr-FR override' }, artifacts: shots });
    await settingsPage.close();
  } finally {
    if (id) await page.request.delete(`/api/v1/playlists/${id}`);
  }
});

test('M67: device-local German labels switch live, preserve playback and persist after restart', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const song = track(fixture, 'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3');
    await page.addInitScript(installAudioTap);
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
    const output = await captureOutput(page, 1200, evidence.path('m67-german-output.wav'));
    assertValidCapture(output);
    expect(mainSegment(output.analysis)?.duration_s).toBeGreaterThan(0.8);
    expect((await audioState(page)).time).toBeGreaterThan(before.time + 0.8);
    data.german_output = output.analysis;
    artifacts.push(output.wav);
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
