/** A03c provider and retained lyrics in installed browsers. */
import { test, expect } from '@playwright/test';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, playCard, expectPlaying, audioState,
  captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(process.env.PLAYER_A03C_ISOLATED !== '1', 'Requires an owned provider fixture');
let fixture: MusicFixture, evidence: Evidence, media: { id: number; title: string };
const modeFile = process.env.PLAYER_A03C_PROVIDER_MODE!;
const callsFile = process.env.PLAYER_A03C_PROVIDER_CALLS!;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('A03c needs a disposable localhost fixture');
  media = JSON.parse(readFileSync(join(fixture.dir, 'provider-lyrics.json'), 'utf8'));
  evidence = new Evidence(join(process.env.PLAYER_A03C_EVIDENCE!, `${Date.now()}-${process.pid}`), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build: fixture.buildRevision, origin: fixture.origin,
  });
});

test('A03c explicit provider and complete offline lyric snapshot', async ({ page, context }) => {
  test.setTimeout(240_000);
  const artifacts: string[] = [];
  let offline = false;
  try {
    writeFileSync(modeFile, 'ready');
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, fixture.user);
    expect((await page.request.delete(`/api/v1/media/${media.id}/lyrics`)).ok()).toBe(true);
    writeFileSync(callsFile, '');
    await openFolder(page, fixture.setIds.Music, 'A03c Provider');
    await playCard(page, media.title, media.id);
    await expectPlaying(page, media.id);
    const lookup = page.getByRole('button', { name: 'Find lyrics online (send track tags)' });
    const panel = page.getByRole('region', { name: 'Lyrics' });
    await expect(lookup).toBeVisible();
    expect(readFileSync(callsFile, 'utf8').trim()).toBe(''); // GET never sends tags.
    await lookup.focus(); await page.keyboard.press('Enter');
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Provider sky' })).toBeVisible();
    await expect(lookup).toBeFocused();
    await expect(page.getByRole('button', { name: 'Remove provider lyrics' })).toBeVisible();
    const calls = readFileSync(callsFile, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ title: ['Provider Sky'], artist: ['Fixture Artist'], authorization: false, cookie: false });
    await page.getByLabel('Lyrics language').selectOption('fr');
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Ciel du fournisseur' })).toBeVisible();
    await page.getByLabel('Lyrics language').selectOption('');
    const first = panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Provider sky' });
    await first.focus(); await page.keyboard.press('Enter');
    await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(1, 1);
    const output = await captureOutput(page, 850, evidence.path('provider-audio.wav'));
    assertValidCapture(output);
    expect(Math.abs((mainSegment(output.analysis)?.tone_hz || 0) - 440)).toBeLessThan(7);
    artifacts.push(output.wav);

    writeFileSync(modeFile, 'error');
    await lookup.click();
    await expect(page.getByRole('status').filter({ hasText: 'Lyric lookup failed' })).toBeVisible();
    await expect(first).toBeVisible(); // A failed retry retains saved server cache.
    writeFileSync(modeFile, 'ready');
    const remove = page.getByRole('button', { name: 'Remove provider lyrics' });
    await remove.click();
    await expect(panel).toBeHidden();
    await expect(lookup).toBeVisible();
    await lookup.click();
    await expect(first).toBeVisible();

    // Pin after lookup so the completed audio and rich lyrics share one snapshot.
    const albums = await (await page.request.get('/api/v1/music/albums')).json();
    const album = albums.find((row: any) => row.title === 'A03c Provider');
    expect(album).toBeTruthy();
    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await expect(page.getByRole('heading', { name: 'A03c Provider' })).toBeVisible();
    await page.locator('#media-grid').getByRole('button', { name: /Pin offline|Manage…/ }).click();
    const pin = page.getByRole('dialog', { name: 'A03c Provider' });
    await pin.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
    await pin.getByRole('button', { name: 'Pin offline', exact: true }).click();
    await expect(pin.locator('[data-status]')).toContainText('Pinned · 1 of 1', { timeout: 60_000 });
    await pin.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await context.setOffline(true); offline = true;
    await page.getByRole('link', { name: 'Browse offline music' }).click();
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    await page.reload();
    await page.getByRole('button', { name: media.title, exact: true }).click();
    await expect(page.locator('[data-results] h2')).toHaveText(media.title);
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, media.id);
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Provider sky' })).toBeVisible();
    await expect(page.getByRole('status').filter({ hasText: 'Saved lyrics' })).toBeVisible();
    const language = page.getByLabel('Lyrics language');
    await language.selectOption('fr');
    await expect(panel.getByRole('button', { name: 'Seek to 0:01: Voice: Lead · Ciel du fournisseur' })).toBeVisible();
    await language.selectOption('');
    await first.focus(); await page.keyboard.press('Enter');
    await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(1, 1);
    await page.setViewportSize({ width: 390, height: 844 });
    artifacts.push(evidence.path('provider-offline.png'));
    await page.screenshot({ path: artifacts.at(-1)! });
    await page.getByRole('button', { name: 'Toggle theme' }).click();
    artifacts.push(evidence.path('provider-offline-other-theme.png'));
    await page.screenshot({ path: artifacts.at(-1)! });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await context.setOffline(false); offline = false;
    await keyboardLogin(page, fixture.admin);
    const separate = await (await page.request.get(`/api/v1/media/${media.id}/lyrics`)).json();
    expect(separate.provider_enabled).toBe(true);
    expect(separate.source).not.toBe('provider');
    expect(separate.lines).toHaveLength(0);
    evidence.record({ feature: 'A03c', scenario: 'explicit provider, error/cache/removal, pinned rich lyrics after cold offline restart and real audio', result: 'pass',
      data: { media_id: media.id, provider_calls: readFileSync(callsFile, 'utf8').trim().split('\n').length,
        other_account_provider_state: separate.provider_state }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'A03c', scenario: 'explicit provider and cold offline snapshot', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (offline) await context.setOffline(false);
    writeFileSync(modeFile, 'ready');
  }
});
