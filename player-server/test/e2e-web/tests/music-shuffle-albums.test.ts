import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, audioState, expectPlaying, captureOutput, assertValidCapture, mainSegment, openSidebarMusic } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires a disposable music fixture with --multi-disc');
let fixture: MusicFixture;
let evidence: Evidence;
const panel = (page: Page) => page.locator('#queue-panel');
const state = (page: Page) => page.evaluate(async () => (await import('/js/queue.js')).queueState());
const ids = (queue: any) => queue.entries.map((entry: any) => entry.media.id as number);
const occurrences = (queue: any) => queue.entries.map((entry: any) => entry.id as number);

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  expect(new URL(fixture.origin).hostname).toBe('127.0.0.1');
  expect(fixture.mediaIds['Music/M04/Album/Disc 2/1.flac']).toBeGreaterThan(0);
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.user);
});

async function makePlaylist(page: Page) {
  const listed = await (await page.request.get('/api/v1/music/albums')).json();
  const names = ['Night Transit', 'M04 Two Disc Album', 'M04 Compilation', 'Synth Compilation', '東京 Nights (夜)'];
  const albums: Record<string, any> = {};
  for (const name of names) {
    const item = listed.find((album: any) => album.title === name);
    expect(item, `catalog album ${name}`).toBeTruthy();
    albums[name] = await (await page.request.get(`/api/v1/music/albums/${encodeURIComponent(item.album_key)}`)).json();
  }
  for (const name of names.slice(1, 4)) {
    expect(albums[name].songs).toHaveLength(3);
    expect(albums[name].songs.map((song: any) => song.music.disc)).toEqual([1, 1, 2]);
  }
  const night = albums['Night Transit'].songs, disc = albums['M04 Two Disc Album'].songs;
  const compilation = albums['M04 Compilation'].songs, synth = albums['Synth Compilation'].songs;
  const tokyo = albums['東京 Nights (夜)'].songs;
  expect(night).toHaveLength(3); expect(tokyo).toHaveLength(2);
  // Deliberately interleave catalog albums and reverse some disc positions.
  const mixed = [night[0], disc[2], synth[2], compilation[1], tokyo[1], night[2],
    disc[0], synth[0], compilation[2], tokyo[0], night[1], disc[1], synth[1], compilation[0]];
  const created = await page.request.post('/api/v1/playlists', { data: { name: `M19 albums ${Date.now()}` } });
  expect(created.ok()).toBe(true);
  const id = (await created.json()).playlist_id as string;
  expect((await page.request.post(`/api/v1/playlists/${id}/songs`, {
    data: { action: 'add', media_ids: mixed.map(song => song.id) },
  })).ok()).toBe(true);
  await page.goto(`/?music=playlist&id=${id}`);
  await expect(page.locator('.media-row')).toHaveCount(mixed.length);
  return { id, albums, mixed, night, disc, compilation, synth, tokyo };
}

async function openQueue(page: Page) {
  if (!(await panel(page).isVisible())) {
    await page.getByRole('button', { name: 'Log out', exact: true }).focus();
    await page.keyboard.press('Q');
  }
  await expect(panel(page)).toBeVisible();
}

async function albumMap(page: Page, mediaIds: number[]) {
  const response = await page.request.post('/api/music/queue-albums', { data: { media_ids: mediaIds } });
  expect(response.ok()).toBe(true);
  return await response.json() as Record<string, { album_key: string; order: number }>;
}

function assertAlbumBlocks(queue: any, map: Record<string, { album_key: string; order: number }>,
  current: number, pinned: number, continuation: number[]) {
  const sequence = ids(queue);
  expect(sequence.slice(0, 2)).toEqual([current, pinned]);
  expect(sequence.slice(2, 2 + continuation.length)).toEqual(continuation);
  const rest = sequence.slice(2 + continuation.length);
  const seen = new Set<string>();
  let previous = '', lastOrder = -1;
  for (const id of rest) {
    const album = map[String(id)];
    expect(album, `catalog map includes ${id}`).toBeTruthy();
    if (album.album_key !== previous) {
      expect(seen.has(album.album_key), `album ${album.album_key} is one block`).toBe(false);
      seen.add(album.album_key); previous = album.album_key; lastOrder = -1;
    }
    expect(album.order).toBeGreaterThan(lastOrder);
    lastOrder = album.order;
  }
  expect(seen.size).toBe(4);
  return [...seen];
}

test('M19 named album mode groups multiple discs, retains pin/source, Undo and restores exact order', async ({ page }) => {
  test.setTimeout(120_000);
  const artifacts: string[] = [];
  let playlist = '';
  try {
    const setup = await makePlaylist(page); playlist = setup.id;
    const current = setup.night[0].id, pin = setup.compilation[2].id;
    const allIds = setup.mixed.map((song: any) => song.id);
    const map = await albumMap(page, allIds);
    expect(Object.keys(map)).toHaveLength(allIds.length);
    await page.locator('.media-row').first().click();
    await expectPlaying(page, current);
    await page.locator('#btn-play').click();
    await expect.poll(async () => (await audioState(page)).paused).toBe(true);
    const progress = page.locator('#progress-track');
    const box = await progress.boundingBox(); expect(box).toBeTruthy();
    await progress.click({ position: { x: box!.width * .3, y: box!.height / 2 } });
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThan(10);
    await openSidebarMusic(page, 'Albums');
    await page.locator(`[data-album="${setup.albums['M04 Compilation'].album_key}"]`).click();
    await page.getByRole('button', { name: `Actions for ${setup.compilation[2].music.title}`, exact: true }).click();
    await page.getByRole('group', { name: 'Media actions' }).getByRole('button', { name: 'Play next', exact: true }).click();
    const canonical = await state(page), source = await audioState(page);
    expect(ids(canonical).slice(0, 2)).toEqual([current, pin]);
    expect(canonical.entries[1].upNext).toBe(true);
    await openQueue(page);
    const albums = panel(page).locator('[data-queue-action="shuffle-albums"]');
    await albums.click();
    await expect.poll(async () => (await state(page)).loading).toBe(false);
    const shuffled = await state(page);
    expect(shuffled.shuffle).toBe(true); expect(shuffled.shuffleMode).toBe('albums');
    await expect(panel(page).locator('[data-queue-action="shuffle"]')).toContainText('Albums');
    const continuation = [setup.night[1].id, setup.night[2].id];
    const orders = new Set<string>();
    orders.add(assertAlbumBlocks(shuffled, map, current, pin, continuation).join('|'));
    expect([...occurrences(shuffled)].sort((a, b) => a - b)).toEqual([...occurrences(canonical)].sort((a, b) => a - b));
    expect(await audioState(page)).toMatchObject({ src: source.src, paused: true });
    expect((await audioState(page)).time).toBeCloseTo(source.time, 1);
    let beforeLast = shuffled;
    for (let i = 0; i < 5; i++) {
      beforeLast = await state(page);
      await panel(page).locator('[data-queue-action="reshuffle"]').focus();
      await page.keyboard.press('Enter');
      await expect.poll(async () => (await state(page)).loading).toBe(false);
      orders.add(assertAlbumBlocks(await state(page), map, current, pin, continuation).join('|'));
    }
    expect(orders.size, 'repeated choices should change album block order').toBeGreaterThan(1);
    await panel(page).getByRole('button', { name: 'Undo', exact: true }).click();
    expect(ids(await state(page))).toEqual(ids(beforeLast));
    const retained = await state(page);
    expect(retained.shuffleMode).toBe('albums');
    assertAlbumBlocks(retained, map, current, pin, continuation);
    // Native typing keeps slash/r/Q text in the search box rather than
    // activating queue or library shortcuts.
    await panel(page).getByRole('button', { name: 'Close queue', exact: true }).click();
    await page.keyboard.press('/');
    await page.getByRole('searchbox', { name: 'Search media' }).fill('r Q Shuffle albums');
    await expect(page.getByRole('searchbox', { name: 'Search media' })).toHaveValue('r Q Shuffle albums');
    expect(ids(await state(page))).toEqual(ids(retained));
    await page.keyboard.press('Escape');
    await openQueue(page);
    for (const [theme, width] of [['dark', 1366], ['light', 1366], ['dark', 390], ['light', 390]] as const) {
      await page.setViewportSize({ width, height: 844 });
      if (await page.locator('html').getAttribute('data-theme') !== theme) {
        await panel(page).getByRole('button', { name: 'Close queue', exact: true }).click();
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
        await openQueue(page);
      }
      await expect(panel(page).locator('[data-queue-action="shuffle-albums"]')).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const shot = evidence.path(`m19-albums-${theme}-${width}.png`);
      await page.screenshot({ path: shot, animations: 'disabled' }); artifacts.push(shot);
    }
    const prior = await state(page);
    await page.reload(); await openQueue(page);
    await expect(panel(page).getByRole('button', { name: 'Resume queue', exact: true })).toBeVisible();
    await expect.poll(async () => (await state(page)).shuffleMode).toBe('albums');
    const recovered = await state(page);
    expect(recovered.active).toBe(false); expect(recovered.shuffleMode).toBe('albums');
    expect(ids(recovered)).toEqual(ids(prior)); expect(occurrences(recovered)).toEqual(occurrences(prior));
    expect(recovered.position).toBeCloseTo(source.time, 0);
    await panel(page).getByRole('button', { name: 'Resume queue', exact: true }).click();
    await expectPlaying(page, current);
    await panel(page).getByRole('button', { name: 'Close queue', exact: true }).click();
    await page.locator('#btn-next').click();
    await expectPlaying(page, pin);
    const audible = await captureOutput(page, 1100, evidence.path('m19-pinned-next.wav'));
    assertValidCapture(audible);
    const segment = mainSegment(audible.analysis);
    const expected = fixture.tracks.find(track => fixture.mediaIds[track.path] === pin)!;
    expect(Math.abs(segment!.tone_hz! - expected.tone_hz[0])).toBeLessThan(5);
    artifacts.push(audible.wav);
    await page.locator('#btn-next').click(); await expectPlaying(page, continuation[0]);
    await openQueue(page);
    const beforeMode = await state(page), beforeModeSource = await audioState(page);
    await panel(page).locator('[data-queue-action="shuffle-tracks"]').click();
    await expect.poll(async () => (await state(page)).shuffleMode).toBe('tracks');
    await panel(page).locator('[data-queue-action="shuffle"]').click();
    const off = await state(page);
    expect(off.shuffle).toBe(false);
    const fixed = occurrences(beforeMode).slice(0, beforeMode.index + 1);
    expect(occurrences(off)).toEqual([...fixed, ...occurrences(canonical).filter(id => !fixed.includes(id))]);
    expect((await audioState(page)).src).toBe(beforeModeSource.src);
    evidence.record({ feature: 'M19', scenario: 'canonical multi-disc album groups, active continuation, Play next FIFO, mode/focus/Undo, restart and audible Next',
      result: 'pass', data: { shuffled: ids(shuffled), modes: [...orders], canonical: ids(canonical), recovered: ids(recovered), off: ids(off), source, audible: segment }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M19', scenario: 'album shuffle core', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (playlist) await page.request.delete(`/api/v1/playlists/${playlist}`);
  }
});

test('M19 offline retry, bounded catalog ACL and account isolation', async ({ page, context }) => {
  test.setTimeout(75_000);
  let playlist = '';
  const admin = await request.newContext({ baseURL: fixture.origin });
  try {
    const setup = await makePlaylist(page); playlist = setup.id;
    await page.locator('.media-row').first().click();
    await expectPlaying(page, setup.night[0].id);
    await page.locator('#btn-play').click();
    await openQueue(page);
    const trackButton = panel(page).locator('[data-queue-action="shuffle-tracks"]');
    await trackButton.focus(); await page.keyboard.press('Enter');
    await expect(trackButton).toBeFocused();
    const before = await state(page);
    expect(before.shuffleMode).toBe('tracks'); expect(before.shuffle).toBe(true);
    await context.setOffline(true);
    await panel(page).locator('[data-queue-action="shuffle-albums"]').click();
    await expect.poll(async () => (await state(page)).status).toContain('Could not shuffle albums');
    const offline = await state(page);
    expect(ids(offline)).toEqual(ids(before));
    expect(occurrences(offline)).toEqual(occurrences(before));
    expect(offline.shuffleMode).toBe('tracks'); expect(offline.shuffle).toBe(true);
    await context.setOffline(false);
    const retry = panel(page).locator('[data-queue-action="shuffle-albums"]');
    await retry.focus(); await page.keyboard.press('Enter');
    await expect.poll(async () => (await state(page)).shuffleMode).toBe('albums');
    await expect(retry).toBeFocused();
    const restored = await state(page);
    expect(restored.shuffle).toBe(true);
    const hidden = fixture.mediaIds['Restricted/M04/hidden.flac'];
    const visible = setup.disc[0].id;
    const visibleMap = await albumMap(page, [hidden, visible]);
    expect(Object.keys(visibleMap)).toEqual([String(visible)]);
    expect((await page.request.post('/api/music/queue-albums', { data: { media_ids: [-1] } })).status()).toBe(400);
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    const privileged = await admin.post('/api/music/queue-albums', { data: { media_ids: [hidden, visible] } });
    expect(privileged.ok()).toBe(true);
    expect(Object.keys(await privileged.json()).sort()).toEqual([String(hidden), String(visible)].sort());
    const users = await (await admin.get('/api/v1/admin/users')).json();
    const userId = users.find((user: any) => user.username === fixture.user.username).id;
    expect((await admin.delete('/api/v1/admin/permissions', {
      data: { set_id: fixture.setIds.Music, user_id: userId },
    })).ok()).toBe(true);
    try {
      expect(await albumMap(page, [hidden, visible])).toEqual({});
    } finally {
      expect((await admin.post('/api/v1/admin/permissions', {
        data: { set_id: fixture.setIds.Music, user_id: userId, role: 'viewer' },
      })).ok()).toBe(true);
    }
    await panel(page).getByRole('button', { name: 'Close queue', exact: true }).click();
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await keyboardLogin(page, fixture.admin);
    await openQueue(page);
    expect((await state(page)).entries).toHaveLength(0);
    await expect(panel(page).locator('[data-queue-action="shuffle-albums"]')).toBeDisabled();
    await panel(page).getByRole('button', { name: 'Close queue', exact: true }).click();
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await keyboardLogin(page, fixture.user);
    await openQueue(page);
    await expect(panel(page).getByRole('button', { name: 'Resume queue', exact: true })).toBeVisible();
    await expect.poll(async () => (await state(page)).shuffleMode).toBe('albums');
    expect(occurrences(await state(page))).toEqual(occurrences(restored));
    evidence.record({ feature: 'M19', scenario: 'offline album lookup preserves Tracks and order, online keyboard retry, bounded ACL and private account recovery',
      result: 'pass', data: { offline: ids(offline), recovered: ids(await state(page)), hidden, visible } });
  } catch (error) {
    evidence.record({ feature: 'M19', scenario: 'offline, ACL and account boundaries', result: 'fail', detail: String(error) });
    throw error;
  } finally {
    await context.setOffline(false);
    await admin.dispose();
    if (playlist) await page.request.delete(`/api/v1/playlists/${playlist}`).catch(() => {});
  }
});
