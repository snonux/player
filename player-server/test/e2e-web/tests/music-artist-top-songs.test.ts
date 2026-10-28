/** M40 real artist Top songs acceptance in installed Chrome and Firefox. */
import { test, expect, request, type Page, type APIRequestContext } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { Evidence, loadFixture, SUPPORT_DIR, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openSidebarMusic, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';
import { minimizePlayer } from './helpers/player-options';

test.skip(process.env.PLAYER_M40_ISOLATED !== '1', 'Requires an isolated disposable music fixture');
test.use({ serviceWorkers: 'block' });
const attempt = `${Date.now()}-${process.pid}`;
const artistName = `M40 Probe Artist ${attempt}`;
const albumName = `M40 Local Ranking ${attempt}`;
const titles = ['Anchor Song', 'Beacon Song', 'A deliberately long local top song title across several lines'];
const tones = [440, 550, 660];
let f: MusicFixture, e: Evidence, ids: number[], artistKey: string;

async function loginApi(account: { username: string; password: string }): Promise<APIRequestContext> {
  const client = await request.newContext({ baseURL: f.origin });
  expect((await client.post('/api/v1/auth/login', { data: account })).ok()).toBe(true);
  return client;
}

async function artist(client: APIRequestContext): Promise<any> {
  const response = await client.get(`/api/v1/music/artists/${artistKey}`);
  expect(response.ok()).toBe(true);
  return response.json();
}

async function openArtist(page: Page, keyboard = false): Promise<void> {
  await openSidebarMusic(page, 'Artists');
  const card = page.getByRole('button', { name: `Open artist ${artistName}`, exact: true });
  await card.scrollIntoViewIfNeeded();
  if (keyboard) { await card.focus(); await page.keyboard.press('Enter'); }
  else await card.click();
  await expect(page.getByRole('heading', { name: artistName, exact: true })).toBeVisible();
}

async function seedGlobal(client: APIRequestContext, id: number): Promise<void> {
  for (const position of [0, 12, 24, 36, 48, 60]) {
    const response = await client.post('/api/v1/progress', { data: { media_id: id, position_seconds: position } });
    expect(response.ok()).toBe(true);
  }
}

async function seedPersonal(client: APIRequestContext, id: number): Promise<void> {
  const token = randomBytes(16).toString('hex');
  const response = await client.post(`/api/v1/media/${id}/playback-sync`, {
    data: { music_plays: [{ id: token, observed_ms: Date.now(), accumulated_seconds: 60 }] },
  });
  expect(response.ok()).toBe(true);
  expect((await response.json()).accepted_music_plays).toContain(token);
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  if (!existsSync(join(f.dir, '.player-music-fixture')) || new URL(f.origin).hostname !== '127.0.0.1')
    throw Error('M40 requires an isolated localhost fixture');
  const state = JSON.parse(readFileSync(join(f.dir, 'state.json'), 'utf8'));
  const folder = join(state.media_root, 'Music', artistName); mkdirSync(folder, { recursive: true });
  for (const [i, title] of titles.entries()) {
    const path = join(folder, `0${i + 1} - ${title}.mp3`);
    if (!existsSync(path)) execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'lavfi', '-i',
      `sine=frequency=${tones[i]}:sample_rate=44100`, '-t', '70', '-af', 'volume=0.12',
      '-c:a', 'libmp3lame', '-b:a', '128k', '-metadata', `title=${title}`,
      '-metadata', `album=${albumName}`, '-metadata', `artist=${artistName}`, path]);
  }
  const admin = await loginApi(f.admin);
  try {
    const freshUser = { username: `m40-${info.project.name}-${Date.now()}`, password: randomBytes(18).toString('hex') };
    const created = await admin.post('/api/v1/admin/users', { data: { ...freshUser, is_admin: false } });
    expect(created.ok()).toBe(true);
    const userId = (await created.json()).id;
    expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' },
    })).ok()).toBe(true);
    f.user = freshUser;
    expect((await admin.post('/api/v1/admin/rescan', { data: {} })).ok()).toBe(true);
    await expect.poll(async () => {
      const scan = await (await admin.get('/api/v1/admin/scan-progress')).json();
      if (scan.state === 'failed') throw Error(JSON.stringify(scan));
      return scan.state === 'complete' && !scan.running && !scan.pending;
    }, { timeout: 60_000 }).toBe(true);
    const people = await (await admin.get('/api/v1/music/artists')).json();
    artistKey = people.find((row: any) => row.name === artistName)?.artist_key;
    expect(artistKey).toBeTruthy();
    const detail = await artist(admin);
    // With no plays, the Top songs list is intentionally empty. Use the full
    // credited discography to identify sources before ranking is seeded.
    const albumKey = detail.albums.find((row: any) => row.title === albumName)?.album_key;
    const album = await (await admin.get(`/api/v1/music/albums/${albumKey}`)).json();
    ids = titles.map(title => album.songs.find((song: any) => song.music?.title === title)?.id);
    expect(ids.every(Number.isSafeInteger)).toBe(true);
  } finally { await admin.dispose(); }
  e = new Evidence(join(resolve(process.env.PLAYER_M40_EVIDENCE || join(tmpdir(), 'player-m40-web-evidence')), attempt), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: f.serverVersion, server_revision: f.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), origin: f.origin,
    artist_key: artistKey, media_ids: ids,
  });
});

test('M40 artist Top songs empty, global fallback and personal rank play actual ordered audio with ACL and Retry', async ({ page, context }) => {
  test.setTimeout(210_000);
  const artifacts: string[] = [];
  const actor = await loginApi(f.user), admin = await loginApi(f.admin);
  let revoked = false;
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, f.user);
    await openArtist(page, true);
    const section = page.locator('section[aria-label="Top songs"]');
    await expect(section).toContainText('No top songs yet. Play this artist to build a local ranking.');
    expect((await artist(actor)).top_songs).toEqual({ source: 'none', songs: [] });
    const empty = e.path('m40-no-local-ranking.png'); await page.screenshot({ path: empty, fullPage: true }); artifacts.push(empty);

    // Real progress writes produce server-wide counters from distinct sessions.
    // The viewer has no personal plays, so the global source must be explicit.
    await seedGlobal(admin, ids[0]);
    await seedGlobal(admin, ids[1]);
    const secondSession = await loginApi(f.admin);
    try { await seedGlobal(secondSession, ids[0]); } finally { await secondSession.dispose(); }
    await page.reload();
    const global = (await artist(actor)).top_songs;
    expect(global.source).toBe('global');
    expect(global.songs.map((song: any) => song.id)).toEqual([ids[0], ids[1]]);
    await expect(section).toContainText('server-wide plays (no personal plays yet)');
    await expect(section.locator('li')).toHaveCount(2);
    await section.getByRole('button', { name: `Play ${titles[1]}`, exact: true }).click();
    await expectPlaying(page, ids[1]);
    let queue = await page.evaluate(async () => (await import('/js/queue.js' as string)).queueState());
    expect(queue.entries.map((entry: any) => entry.media.id)).toEqual([ids[1]]);
    const second = await captureOutput(page, 1300, e.path('m40-global-ranked-second.wav'));
    assertValidCapture(second);
    expect(Math.abs((mainSegment(second.analysis)?.tone_hz || 0) - tones[1])).toBeLessThan(5);
    artifacts.push(second.wav);
    if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
    await section.getByRole('button', { name: '▶ Play top songs', exact: true }).click();
    await expectPlaying(page, ids[0]);
    queue = await page.evaluate(async () => (await import('/js/queue.js' as string)).queueState());
    expect(queue.entries.map((entry: any) => entry.media.id)).toEqual([ids[0], ids[1]]);
    const first = await captureOutput(page, 1300, e.path('m40-global-ranked-first.wav'));
    assertValidCapture(first);
    expect(Math.abs((mainSegment(first.analysis)?.tone_hz || 0) - tones[0])).toBeLessThan(5);
    artifacts.push(first.wav);
    const globalShot = e.path('m40-global-source.png'); await page.screenshot({ path: globalShot, fullPage: true }); artifacts.push(globalShot);

    if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
    await page.keyboard.press('Backspace');
    await expect(page.getByRole('heading', { name: 'Artists', exact: true })).toBeVisible();
    await openArtist(page, false);
    await expect(section).toContainText('server-wide');

    await seedPersonal(actor, ids[2]); await seedPersonal(actor, ids[2]); await seedPersonal(actor, ids[0]);
    await page.reload();
    const personal = (await artist(actor)).top_songs;
    expect(personal.source).toBe('personal');
    expect(personal.songs.map((song: any) => song.id)).toEqual([ids[2], ids[0]]);
    await expect(section).toContainText('your most-played credited songs');
    await expect(section.locator('li')).toHaveCount(2);
    await expect(section).not.toContainText(titles[1]);
    if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
    await section.getByRole('button', { name: '▶ Play top songs', exact: true }).click();
    await expectPlaying(page, ids[2]);
    queue = await page.evaluate(async () => (await import('/js/queue.js' as string)).queueState());
    expect(queue.entries.map((entry: any) => entry.media.id)).toEqual([ids[2], ids[0]]);
    const own = await captureOutput(page, 1300, e.path('m40-personal-ranked-first.wav'));
    assertValidCapture(own);
    expect(Math.abs((mainSegment(own.analysis)?.tone_hz || 0) - tones[2])).toBeLessThan(5);
    artifacts.push(own.wav);

    if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
    await page.setViewportSize({ width: 390, height: 844 });
    for (const theme of ['dark', 'light']) {
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      const shot = e.path(`m40-${theme}-narrow.png`); await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' }); artifacts.push(shot);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }

    execFileSync('python3', [join(SUPPORT_DIR, 'music_fixture.py'), 'restart', f.dir]);
    await page.reload();
    await expect(section).toContainText('your most-played credited songs');
    expect((await artist(actor)).top_songs.songs.map((song: any) => song.id)).toEqual([ids[2], ids[0]]);

    const userId = (await (await admin.get('/api/v1/admin/users')).json()).find((row: any) => row.username === f.user.username)?.id;
    expect(userId).toBeGreaterThan(0);
    expect((await admin.delete('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId } })).ok()).toBe(true);
    revoked = true;
    await page.reload();
    await expect(page.getByRole('alert')).toContainText('Could not open this library link: forbidden');
    expect((await actor.get(`/api/v1/music/artists/${artistKey}`)).status()).toBe(404);
    const denied = e.path('m40-artist-revoked.png'); await page.screenshot({ path: denied, fullPage: true }); artifacts.push(denied);
    expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' },
    })).ok()).toBe(true);
    revoked = false;
    await context.setOffline(true);
    await page.getByRole('button', { name: 'Retry link' }).click();
    await expect(page.getByRole('alert')).toContainText('Could not open this library link:');
    await context.setOffline(false);
    await page.getByRole('button', { name: 'Retry link' }).click();
    await expect(section).toContainText('your most-played credited songs');
    e.record({ feature: 'M40', scenario: 'empty then global and personal ranked artist songs, per-song and list audio, keyboard/pointer, dark/light narrow, restart, ACL and offline Retry', result: 'pass',
      data: { media_ids: ids, global_ids: global.songs.map((song: any) => song.id),
        personal_ids: personal.songs.map((song: any) => song.id),
        global_audio: mainSegment(first.analysis), personal_audio: mainSegment(own.analysis) }, artifacts });
  } catch (error) {
    e.record({ feature: 'M40', scenario: 'browser artist Top songs acceptance', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (revoked) {
      const userId = (await (await admin.get('/api/v1/admin/users')).json()).find((row: any) => row.username === f.user.username)?.id;
      if (userId) await admin.post('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' } });
    }
    await actor.dispose(); await admin.dispose();
  }
});
