/** A03b rich lyrics in installed browsers against the owned local fixture. */
import { test, expect } from '@playwright/test';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { audioState, keyboardLogin, openFolder, playCard, expectPlaying,
  captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(process.env.PLAYER_A03B_ISOLATED !== '1', 'Requires an owned rich-lyrics fixture');
let fixture: MusicFixture, evidence: Evidence, rich: { id: number; title: string };

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('A03b needs a disposable localhost fixture');
  rich = JSON.parse(readFileSync(join(fixture.dir, 'rich-lyrics.json'), 'utf8'));
  evidence = new Evidence(join(process.env.PLAYER_A03B_EVIDENCE!, `${Date.now()}-${process.pid}`), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build: fixture.buildRevision, origin: fixture.origin,
  });
});

test('A03b words, voices and translations follow playback and seeking', async ({ page, context }) => {
  test.setTimeout(180_000);
  const artifacts: string[] = [];
  let offline = false;
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, fixture.user);
    await openFolder(page, fixture.setIds.Music, 'A03b Rich');
    await playCard(page, rich.title, rich.id);
    await expectPlaying(page, rich.id);
    const panel = page.getByRole('region', { name: 'Lyrics' });
    await expect(panel).toBeVisible();
    const lead = panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Hello world' });
    const harmony = panel.getByRole('button', { name: 'Seek to 0:01: Voice: Harmony · Harmony sings' });
    const last = panel.getByRole('button', { name: 'Seek to 0:06: Voice: Lead · Road home' });
    await expect(lead).toBeVisible();
    await lead.focus();
    await page.keyboard.press('p');
    await expect.poll(async () => (await audioState(page)).paused).toBe(true);
    await page.keyboard.press('Enter');
    await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(1, 1);
    await expect(lead).toBeFocused();
    await expect(lead).toHaveAttribute('aria-current', 'true');
    await expect(harmony).toHaveAttribute('aria-current', 'true');
    await expect(lead.locator('.lyric-word-current')).toHaveText('Hello ');

    const language = page.getByLabel('Lyrics language');
    await language.focus();
    await language.selectOption('fr');
    await expect(language).toBeFocused();
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Bonjour monde' })).toBeVisible();
    await expect(harmony).toContainText('Original: en');
    expect((await audioState(page)).time).toBeCloseTo(1, 1);
    await language.selectOption('');
    await expect(lead.locator('.lyric-word-current')).toHaveText('Hello ');

    await lead.focus();
    await page.keyboard.press('p');
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThanOrEqual(2);
    await expect(lead.locator('.lyric-word-current')).toHaveText('world');
    const output = await captureOutput(page, 900, evidence.path('rich-lyrics-audio.wav'));
    assertValidCapture(output);
    expect(Math.abs((mainSegment(output.analysis)?.tone_hz || 0) - 523.25)).toBeLessThan(7);
    artifacts.push(output.wav);
    await lead.focus();
    await page.keyboard.press('p');
    await page.setViewportSize({ width: 390, height: 844 });
    await last.click();
    await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(6, 1);
    await expect(last.locator('.lyric-word-current')).toHaveText('Road ');
    await expect(harmony).not.toHaveAttribute('aria-current', 'true');
    await lead.click();
    await expect(lead.locator('.lyric-word-current')).toHaveText('Hello ');
    await expect(harmony).toHaveAttribute('aria-current', 'true');

    const more = page.getByRole('button', { name: 'More player controls' });
    await more.click();
    await page.getByRole('group', { name: 'More player controls' })
      .getByLabel('Lyrics language').selectOption('fr');
    await expect(more).toBeFocused();
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Bonjour monde' })).toBeVisible();
    await more.click();
    await page.getByRole('group', { name: 'More player controls' })
      .getByLabel('Lyrics language').selectOption('');
    await expect(more).toBeFocused();
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await expect(lead.locator('.lyric-word-current')).toHaveText('Hello ');
    expect(await lead.locator('.lyric-word-current').evaluate(element =>
      getComputedStyle(element).textDecorationLine)).toContain('underline');

    for (const theme of ['dark', 'light']) {
      await page.getByRole('button', { name: 'Settings', exact: true }).click();
      await page.locator('#appearance-mode').selectOption(theme);
      await page.keyboard.press('Escape');
      await expect(page.locator('#settings-modal')).toBeHidden();
      const shot = evidence.path(`rich-lyrics-narrow-${theme}.png`);
      await page.screenshot({ path: shot }); artifacts.push(shot);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    await context.setOffline(true); offline = true;
    await last.click();
    await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(6, 1);
    await expect(last).toHaveAttribute('aria-current', 'true');
    await context.setOffline(false); offline = false;

    await openFolder(page, fixture.setIds.Music, 'Aurora Lane - Night Transit (2021)');
    const simple = fixture.mediaIds['Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3'];
    await playCard(page, 'Long Road Home', simple);
    await expectPlaying(page, simple);
    await expect(panel).toContainText('Every mile a quiet song');
    await expect(language).toBeHidden();
    await expect(panel.locator('.lyric-word-current')).toHaveCount(0);
    evidence.record({ feature: 'A03b', scenario: 'real word/voice/language timing, seek, offline loaded and line fallback',
      result: 'pass', data: { rich_id: rich.id, fallback_id: simple }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'A03b', scenario: 'real word/voice/language timing, seek, offline loaded and line fallback',
      result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (offline) await context.setOffline(false);
  }
});
