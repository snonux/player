import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, captureOutput, assertValidCapture, audioState, openFolder, playCard } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires a disposable local music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
const gapless = (page: Page) => page.locator('.loudness-control').filter({ has: page.locator('summary', { hasText: 'Gapless' }) });
const queue = (page: Page) => page.evaluate(async () => (await import('/js/queue.js')).queueState());
const identity = (page: Page) => page.evaluate(async () => (await import('/js/playback.js')).currentMediaId());
const position = (page: Page) => page.evaluate(async () => (await import('/js/playback.js')).queuePlaybackPosition());

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  expect(new URL(fixture.origin).hostname).toBe('127.0.0.1');
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.user);
  // The fixture has no reported browser connection type; allow successor
  // preparation through the app's explicit Wi-Fi policy for this measurement.
  await page.evaluate(() => {
    localStorage.setItem('player-music-network', JSON.stringify({ mode: 'wifi' }));
    localStorage.setItem('player-playback-cache', JSON.stringify({ budget: 0, buffer: 'stream', prefetch: false }));
    document.dispatchEvent(new Event('music-network:changed'));
  });
});

async function makePlaylist(page: Page, codec: 'FLAC' | 'MP3') {
  const ext = codec.toLowerCase();
  const titles = ['Movement I', 'Movement II', 'Movement III'];
  const songs = titles.map((title, i) => track(fixture, `Music/Gapless Suite (${codec})/0${i + 1} - ${title}.${ext}`));
  for (const song of songs) {
    const response = await page.request.post('/api/v1/progress/status', { data: { media_id: song.id, status: 'not_started' } });
    expect(response.ok()).toBe(true);
  }
  const created = await page.request.post('/api/v1/playlists', { data: { name: `M21 ${codec} ${Date.now()}` } });
  expect(created.ok()).toBe(true);
  const id = (await created.json()).playlist_id as string;
  const added = await page.request.post(`/api/v1/playlists/${id}/songs`, {
    data: { action: 'add', media_ids: songs.map(song => song.id) },
  });
  expect(added.ok()).toBe(true);
  await page.goto(`/?music=playlist&id=${id}`);
  await expect(page.locator('.media-row')).toHaveCount(3);
  return { id, songs };
}

async function setGapless(page: Page, enabled: boolean) {
  const panel = gapless(page);
  if (!(await panel.evaluate((details: HTMLDetailsElement) => details.open))) await panel.locator('summary').click();
  const box = panel.getByRole('checkbox', { name: 'Gapless music queue' });
  if (enabled) await box.check(); else await box.uncheck();
  await expect(box).toBeChecked({ checked: enabled });
  return panel;
}

async function playPlaylist(page: Page, firstId: number) {
  await page.getByRole('button', { name: '▶ Play playlist', exact: true }).click();
  await expect.poll(() => identity(page), { timeout: 15000 }).toBe(firstId);
  await expect.poll(() => position(page), { timeout: 15000 }).toBeGreaterThan(0.1);
  expect((await queue(page)).active).toBe(true);
}

test('M21 FLAC and MP3 decoded boundaries versus ordinary streaming', async ({ page }) => {
  test.setTimeout(150_000);
  const results: Record<string, unknown> = {};
  const artifacts: string[] = [];
  for (const codec of ['FLAC', 'MP3'] as const) {
    const { id, songs } = await makePlaylist(page, codec);
    try {
      await page.locator('.media-row').first().click();
      await expect.poll(() => identity(page)).toBe(songs[0].id);
      for (const enabled of [false, true]) {
        const panel = await setGapless(page, enabled);
        await playPlaylist(page, songs[0].id);
        if (enabled) await expect(panel.getByRole('status')).toContainText('Gapless ready', { timeout: 10000 });
        else await expect(panel.getByRole('status')).toHaveText('Gapless off.');
        const start = await position(page);
        const wav = evidence.path(`m21-${codec.toLowerCase()}-${enabled ? 'gapless' : 'normal'}.wav`);
        const out = await captureOutput(page, 13000, wav, [], 880);
        assertValidCapture(out);
        artifacts.push(wav);
        const first = songs[0].duration_s - start;
        const boundaries = [first, first + songs[1].duration_s];
        const near = (at: number) => boundaries.some(boundary => Math.abs(at - boundary) < 0.8);
        const gaps = out.analysis.gaps.filter(g => near(g.start_s));
        const clicks = (out.analysis.discontinuities ?? []).filter(d => near(d.time_s));
        const measured = { start, boundaries, gaps, clicks, segments: out.analysis.segments,
          audible_s: out.analysis.audible_s, events: out.events };
        results[`${codec}-${enabled ? 'gapless' : 'normal'}`] = measured;
        expect(out.analysis.audible_s).toBeGreaterThan(11);
        await expect.poll(() => identity(page), { timeout: 5000 }).toBe(songs[2].id);
        expect((await queue(page)).index).toBe(2);
        if (enabled) {
          expect(gaps, `${codec} decoded queue should not go silent at either boundary`).toEqual([]);
          expect(clicks, `${codec} decoded queue should have no phase clicks at either boundary`).toEqual([]);
          for (const song of songs.slice(0, 2)) {
            await expect.poll(async () => (await (await page.request.get(`/api/media/${song.id}`)).json()).progress?.finished,
              { timeout: 10000 }).toBe(true);
          }
        }
      }
    } finally {
      await page.request.delete(`/api/v1/playlists/${id}`);
    }
  }
  evidence.record({ feature: 'M21', scenario: 'FLAC and MP3 three-song actual output across two automatic boundaries, gapless versus normal',
    result: 'pass', data: results, artifacts });
});

test('M21 seek, queue edit, speed and source fallback, themes and account scope', async ({ page }) => {
  test.setTimeout(90_000);
  const artifacts: string[] = [];
  const { id, songs } = await makePlaylist(page, 'FLAC');
  try {
    await page.locator('.media-row').first().click();
    const panel = await setGapless(page, true);
    await playPlaylist(page, songs[0].id);
    await expect(panel.getByRole('status')).toContainText('Gapless ready', { timeout: 10000 });
    await page.locator('#btn-play').click();
    const pausedAt = await position(page);
    await page.waitForTimeout(300);
    expect(await position(page)).toBeCloseTo(pausedAt, 1);
    await page.locator('#player [data-queue-toggle]').click();
    const list = page.locator('#queue-panel');
    await expect(list).toBeVisible();
    await list.locator('[data-queue-index="1"] [data-queue-action="down"]').click();
    expect((await queue(page)).entries.map((entry: any) => entry.media.id)).toEqual([songs[0].id, songs[2].id, songs[1].id]);
    await list.getByRole('button', { name: 'Close queue' }).click();
    const seek = page.locator('#progress-track');
    const bounds = await seek.boundingBox();
    expect(bounds).toBeTruthy();
    await page.mouse.click(bounds!.x + bounds!.width * 0.72, bounds!.y + bounds!.height / 2);
    expect(await position(page)).toBeGreaterThan(4);
    await page.locator('#btn-play').click();
    await expect.poll(() => identity(page), { timeout: 10000 }).toBe(songs[2].id);
    await expect(page.locator('#player-music-identity')).toContainText('Movement III');
    await expect.poll(async () => (await (await page.request.get(`/api/media/${songs[0].id}`)).json()).progress?.finished,
      { timeout: 10000 }).toBe(true);
    await page.locator('#playback-speed').selectOption('1.5');
    await expect.poll(async () => (await audioState(page)).src, { timeout: 10000 }).toMatch(new RegExp(`/media/${songs[2].id}/`));
    await page.getByRole('button', { name: '▶ Play playlist', exact: true }).click();
    await expect(panel.getByRole('status')).toContainText('requires 1×');
    await page.locator('#playback-speed').selectOption('1');
    for (const [theme, width] of [['dark', 1366], ['light', 390]] as const) {
      await page.setViewportSize({ width, height: 844 });
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const shot = evidence.path(`m21-${theme}-${width}.png`);
      await page.screenshot({ path: shot, animations: 'disabled' });
      artifacts.push(shot);
    }
    await page.getByRole('button', { name: 'More player controls', exact: true }).click();
    const compactGapless = gapless(page);
    if (!(await compactGapless.evaluate((details: HTMLDetailsElement) => details.open)))
      await compactGapless.locator('summary').click();
    const compactBox = compactGapless.getByRole('checkbox', { name: 'Gapless music queue' });
    await expect(compactBox).toBeVisible();
    await compactBox.uncheck();
    await expect(compactGapless.getByRole('status')).toHaveText('Gapless off.');
    await compactBox.check();
    const compactShot = evidence.path('m21-light-390-gapless-controls.png');
    await page.screenshot({ path: compactShot, animations: 'disabled' });
    artifacts.push(compactShot);
    await page.getByRole('button', { name: 'Close controls', exact: true }).click();
    await openFolder(page, fixture.setIds.Music, 'Gapless Suite (FLAC)');
    await playCard(page, songs[0].title, songs[0].id);
    expect((await queue(page)).active).toBe(false);
    expect((await audioState(page)).paused).toBe(false);
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await keyboardLogin(page, fixture.admin);
    expect((await queue(page)).active).toBe(false);
    expect(await identity(page)).toBeFalsy();
    expect(await page.evaluate(() => localStorage.getItem('player-gapless'))).toBe('true');
    evidence.record({ feature: 'M21', scenario: 'visible seek/pause/queue reorder, progress and metadata, rate/source fallback, theme and account isolation',
      result: 'pass', data: { pausedAt }, artifacts });
  } finally {
    // The playlist is owned by the fixture user; cleanup uses its browser session.
    if (new URL(page.url()).pathname === '/login.html') await keyboardLogin(page, fixture.user);
    if ((await page.request.delete(`/api/v1/playlists/${id}`)).status() === 404) {
      await page.getByRole('button', { name: 'Log out', exact: true }).click();
      await keyboardLogin(page, fixture.user);
      await page.request.delete(`/api/v1/playlists/${id}`);
    }
  }
});

test('M21 bounded download and decoder errors return to audible streaming', async ({ page }) => {
  test.setTimeout(60_000);
  const { id, songs } = await makePlaylist(page, 'FLAC');
  try {
    await page.locator('.media-row').first().click();
    const panel = await setGapless(page, true);
    const path = new RegExp(`/api/media/${songs[0].id}/stream`);
    const fallbacks: string[] = [];
    for (const fault of ['size', 'decode'] as const) {
      let hits = 0;
      await page.route(path, async route => {
        hits++;
        if (hits > 1) return route.continue();
        if (fault === 'size') return route.fulfill({ status: 200,
          headers: { 'content-type': 'audio/flac', 'content-length': String(32 * 1024 * 1024 + 1) }, body: '' });
        return route.fulfill({ status: 200, contentType: 'audio/flac', body: 'invalid FLAC bytes' });
      });
      await playPlaylist(page, songs[0].id);
      await expect.poll(async () => panel.locator('[role="status"]').textContent(), { timeout: 10000 })
        .toContain('Normal streaming');
      expect(hits).toBeGreaterThanOrEqual(2);
      const status = (await panel.locator('[role="status"]').textContent()) || '';
      if (fault === 'size') expect(status).toContain('32 MB');
      await expect.poll(async () => (await audioState(page)).paused, { timeout: 10000 }).toBe(false);
      const before = (await audioState(page)).time;
      await expect.poll(async () => (await audioState(page)).time, { timeout: 10000 }).toBeGreaterThan(before + 0.4);
      fallbacks.push(`${fault}: ${status}`);
      await page.unroute(path);
    }
    evidence.record({ feature: 'M21', scenario: 'oversized response header and invalid decoder bytes use ordinary audible stream',
      result: 'pass', data: { fallbacks } });
  } finally {
    await page.request.delete(`/api/v1/playlists/${id}`);
  }
});
