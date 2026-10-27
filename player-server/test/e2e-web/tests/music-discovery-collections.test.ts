/** M38 installed-browser discovery acceptance on one disposable music server. */
import { test, expect, request, type Page, type APIRequestContext } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openSidebarMusic, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(process.env.PLAYER_M38_ISOLATED !== '1', 'Requires an isolated disposable music fixture');
test.use({ serviceWorkers: 'block' });
const keys = ['recently_added', 'recently_played', 'most_played', 'random', 'favorites', 'top_rated'] as const;
const names = ['Recently added', 'Recently played', 'Most played', 'Random albums', 'Favorite albums', 'Top rated albums'];
const attempt = `${Date.now()}-${process.pid}`;
let f: MusicFixture, e: Evidence, ids: number[], albumKeys: string[];

async function loginApi(account: { username: string; password: string }): Promise<APIRequestContext> {
  const client = await request.newContext({ baseURL: f.origin });
  expect((await client.post('/api/v1/auth/login', { data: account })).ok()).toBe(true);
  return client;
}

async function apiCollection(client: APIRequestContext, key: string): Promise<any[]> {
  const response = await client.get(`/api/v1/music/albums?collection=${key}`);
  expect(response.ok()).toBe(true);
  return response.json();
}

async function discovery(page: Page): Promise<void> {
  await openSidebarMusic(page, 'Albums');
  const panel = page.locator('details[aria-label="Discover albums"]');
  await expect(panel).toBeVisible();
  if (!(await panel.evaluate(element => (element as HTMLDetailsElement).open))) {
    const summary = panel.locator(':scope > summary');
    await summary.focus();
    await page.keyboard.press('Enter');
  }
  await expect(panel).toHaveAttribute('open', '');
}

async function listKeys(page: Page): Promise<string[]> {
  return page.locator('#media-grid [data-album]').evaluateAll(cards => cards.map(card => (card as HTMLElement).dataset.album!));
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  if (!existsSync(join(f.dir, '.player-music-fixture')) || new URL(f.origin).hostname !== '127.0.0.1')
    throw Error('M38 requires an isolated localhost music fixture');
  const state = JSON.parse(readFileSync(join(f.dir, 'state.json'), 'utf8'));
  for (const [name, hz] of [['M38 Listening A', 440], ['M38 Listening B', 550]] as const) {
    const folder = join(state.media_root, 'Music', name); mkdirSync(folder, { recursive: true });
    const path = join(folder, `01 - ${name}.mp3`);
    if (!existsSync(path)) execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'lavfi', '-i',
      `sine=frequency=${hz}:sample_rate=44100`, '-t', '70', '-af', 'volume=0.12', '-c:a', 'libmp3lame',
      '-b:a', '128k', '-metadata', `title=${name}`, '-metadata', `album=${name}`,
      '-metadata', 'artist=M38 Listener', path]);
  }
  const admin = await loginApi(f.admin);
  try {
    const freshUser = { username: `m38-${info.project.name}-${Date.now()}`, password: randomBytes(18).toString('hex') };
    const created = await admin.post('/api/v1/admin/users', { data: { ...freshUser, is_admin: false } });
    expect(created.ok()).toBe(true);
    const accountId = (await created.json()).id;
    expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: f.setIds.Music, user_id: accountId, role: 'viewer' },
    })).ok()).toBe(true);
    f.user = freshUser;
    expect((await admin.post('/api/v1/admin/rescan', { data: {} })).ok()).toBe(true);
    await expect.poll(async () => {
      const status = await (await admin.get('/api/v1/admin/scan-progress')).json();
      if (status.state === 'failed') throw Error(JSON.stringify(status));
      return status.state === 'complete' && !status.running && !status.pending;
    }, { timeout: 60_000 }).toBe(true);
    const albums = await (await admin.get('/api/v1/music/albums')).json();
    albumKeys = ['M38 Listening A', 'M38 Listening B'].map(name => albums.find((row: any) => row.title === name)?.album_key);
    expect(albumKeys.every(Boolean)).toBe(true);
    ids = [];
    for (const key of albumKeys) ids.push((await (await admin.get(`/api/v1/music/albums/${key}`)).json()).songs[0].id);
    expect(ids.every(Number.isSafeInteger)).toBe(true);
  } finally { await admin.dispose(); }
  e = new Evidence(join(resolve(process.env.PLAYER_M38_EVIDENCE || join(tmpdir(), 'player-m38-web-evidence')), attempt), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: f.serverVersion, server_revision: f.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), origin: f.origin,
    albums: albumKeys, media_ids: ids,
  });
});

test('M38 personal discovery collections expose complete ACL-scoped order, empty states, Play/View all, facets and recovery', async ({ page, context }) => {
  test.setTimeout(240_000);
  const artifacts: string[] = [];
  const actor = await loginApi(f.user), admin = await loginApi(f.admin);
  let revoked = false;
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, f.user);
    await discovery(page);
    const panel = page.locator('details[aria-label="Discover albums"]');
    for (const [name, empty] of [
      ['Recently played', 'Play music to start your listening history.'],
      ['Most played', 'No qualified plays yet. Listen to music to build this collection.'],
      ['Favorite albums', 'Favorite an album using its heart button.'],
      ['Top rated albums', 'Rate an album to add it here.'],
    ]) {
      await expect(panel.locator(`section[aria-label="${name}"]`)).toContainText(empty);
    }
    const emptyShot = e.path('m38-empty-personal-collections.png');
    await page.screenshot({ path: emptyShot, fullPage: true, animations: 'disabled' }); artifacts.push(emptyShot);

    // These are real authenticated offline-playback acknowledgements, not
    // direct database mutations. M36 already verifies client qualification.
    const times = [Date.now() - 40_000, Date.now() - 20_000, Date.now() - 10_000];
    for (const [id, when] of [[ids[1], times[0]], [ids[0], times[1]], [ids[0], times[2]]] as const) {
      const token = randomBytes(16).toString('hex');
      const sync = await actor.post(`/api/v1/media/${id}/playback-sync`, {
        data: { music_plays: [{ id: token, observed_ms: when, accumulated_seconds: 60 }] },
      });
      expect(sync.ok()).toBe(true);
      expect((await sync.json()).accepted_music_plays).toContain(token);
    }
    expect((await actor.put(`/api/v1/music/favorites/album/${albumKeys[1]}`)).ok()).toBe(true);
    const longAlbum = (await (await actor.get('/api/v1/music/albums')).json()).find((row: any) => row.title.startsWith('Long Album'));
    expect(longAlbum).toBeTruthy();
    expect((await actor.put(`/api/v1/music/favorites/album/${longAlbum.album_key}`)).ok()).toBe(true);
    expect((await actor.put(`/api/v1/music/ratings/album/${albumKeys[0]}`, { data: { rating: 5 } })).ok()).toBe(true);
    expect((await actor.put(`/api/v1/music/ratings/album/${albumKeys[1]}`, { data: { rating: 3 } })).ok()).toBe(true);
    await page.reload(); await discovery(page);
    const expected = Object.fromEntries(await Promise.all(keys.map(async key => [key, await apiCollection(actor, key)]))) as Record<string, any[]>;
    expect(expected.recently_played.map(row => row.album_key)).toEqual(albumKeys);
    expect(expected.most_played.map(row => row.album_key)).toEqual(albumKeys);
    expect(expected.favorites.map(row => row.album_key)).toContain(albumKeys[1]);
    expect(expected.top_rated.map(row => row.album_key)).toEqual(albumKeys);
    for (const key of keys) {
      const index = keys.indexOf(key), name = names[index], section = panel.locator(`section[aria-label="${name}"]`);
      await expect(section).toBeVisible();
      const rows = expected[key];
      await expect(section).toContainText(`${rows.length} albums`);
      await expect(section.locator('[data-preview]')).toHaveCount(Math.min(4, rows.length));
      await expect(section.locator('[data-collection-play]')).toBeEnabled();
      await section.getByRole('button', { name: `View all ${name.toLowerCase()}`, exact: true }).click();
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
      const seen = await listKeys(page);
      if (key === 'random') {
        expect(new Set(seen).size).toBe(seen.length);
        expect(new Set(seen)).toEqual(new Set(expected.random.map(row => row.album_key)));
      } else expect(seen).toEqual(rows.map(row => row.album_key));
      if (key === 'random') {
        const first = page.locator('#media-grid [data-album]').first();
        const firstKey = await first.getAttribute('data-album');
        await first.focus(); await page.keyboard.press('Enter');
        await expect(page.locator('.album-header')).toBeVisible();
        await page.keyboard.press('Backspace');
        await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
        expect((await listKeys(page))[0]).toBe(firstKey);
      }
      await page.keyboard.press('Backspace');
      await expect(panel).toBeVisible();
      if (!(await panel.evaluate(element => (element as HTMLDetailsElement).open))) await panel.locator(':scope > summary').click();
    }
    const populated = e.path('m38-six-populated-collections.png');
    await page.screenshot({ path: populated, fullPage: true, animations: 'disabled' }); artifacts.push(populated);

    // Play all uses every authorized album, not the four-item preview.
    const added = expected.recently_added;
    const firstSongs = (await (await actor.get(`/api/v1/music/albums/${added[0].album_key}`)).json()).songs;
    await panel.locator('section[aria-label="Recently added"]').locator('[data-collection-play]').click();
    await expectPlaying(page, firstSongs[0].id);
    const queue = await page.evaluate(async () => (await import('/js/queue.js' as string)).queueState());
    expect(queue.entries).toHaveLength(added.reduce((sum, row) => sum + row.song_count, 0));
    const audio = await captureOutput(page, 1400, e.path('m38-discovery-play-all.wav'));
    assertValidCapture(audio);
    expect(mainSegment(audio.analysis)?.duration_s).toBeGreaterThan(.8);
    artifacts.push(audio.wav);

    // Existing Genre and Year/decade entry points retain their normal filters.
    if (await page.locator('#btn-minimize').isVisible()) await page.locator('#btn-minimize').click();
    await page.getByLabel('Genre', { exact: true }).selectOption('Synthwave');
    await expect(page.getByRole('button', { name: 'Remove Genre: Synthwave' })).toBeVisible();
    await page.getByLabel('Year / decade').selectOption('2020-2029');
    await expect(page.getByRole('button', { name: 'Remove Year / decade: 2020-2029' })).toBeVisible();
    await expect(page.locator('#media-grid [data-album]').first()).toBeVisible();
    await page.getByRole('button', { name: 'Clear filters' }).click();
    await expect(page.locator('#media-grid [data-album]')).toHaveCount(added.length);

    await page.setViewportSize({ width: 390, height: 844 });
    for (const theme of ['dark', 'light']) {
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      await discovery(page);
      const shot = e.path(`m38-${theme}-narrow.png`);
      await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' }); artifacts.push(shot);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }

    const userId = (await (await admin.get('/api/v1/admin/users')).json()).find((row: any) => row.username === f.user.username)?.id;
    expect(userId).toBeGreaterThan(0);
    expect((await admin.delete('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId } })).ok()).toBe(true);
    revoked = true;
    await page.reload(); await discovery(page);
    await expect(panel.locator('section[aria-label="Recently added"]')).toContainText('No albums yet.');
    await expect(panel.locator('section[aria-label="Random albums"]')).toContainText('No albums available.');
    expect((await apiCollection(actor, 'recently_added'))).toHaveLength(0);
    const hidden = e.path('m38-acl-empty.png'); await page.screenshot({ path: hidden, fullPage: true }); artifacts.push(hidden);
    expect((await admin.post('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' } })).ok()).toBe(true);
    revoked = false;
    await discovery(page);
    await expect(panel.locator('section[aria-label="Recently added"]')).toContainText(`${added.length} albums`);
    await context.setOffline(true);
    await panel.locator('section[aria-label="Recently added"]').getByRole('button', { name: 'View all recently added' }).click();
    await expect(page.locator('#media-grid [role="alert"]')).toContainText('Could not load albums');
    await context.setOffline(false);
    await page.getByRole('button', { name: 'Retry', exact: true }).click();
    await discovery(page);
    await expect(panel.locator('section[aria-label="Most played"]')).toContainText('2 albums');
    e.record({ feature: 'M38', scenario: 'six personal authorized discovery collections, whole ordered Play/View all, real audio, random Back, facets, empty states, ACL and offline Retry', result: 'pass',
      data: { media_ids: ids, ranked_keys: albumKeys, recently_added_count: added.length,
        queue_entries: queue.entries.length, audio: mainSegment(audio.analysis) }, artifacts });
  } catch (error) {
    e.record({ feature: 'M38', scenario: 'browser discovery collections acceptance', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (revoked) {
      const userId = (await (await admin.get('/api/v1/admin/users')).json()).find((row: any) => row.username === f.user.username)?.id;
      if (userId) await admin.post('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' } });
    }
    await actor.dispose(); await admin.dispose();
  }
});
