import { test, expect, request, type Page, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { Evidence, loadFixture, type Account, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { audioState, captureOutput, assertValidCapture, mainSegment, expectPlaying, keyboardLogin, openSidebarMusic, resetProgress } from './helpers/music-ui';

test.skip(process.env.PLAYER_M36_ISOLATED !== '1', 'Requires an exclusively owned disposable fixture');
test.use({ serviceWorkers: 'block' });

const albumName = 'M36 Personal Ledger';
const artistName = 'Ledger Artist';
const folderName = 'M36 Personal Ledger';
const longName = '01 - Qualified Listening.mp3';
const shortName = '02 - Skipped Listening.mp3';
const attempt = `${Date.now()}-${process.pid}`;
let fixture: MusicFixture, evidence: Evidence, ids: { long: number; short: number };
let albumKey: string, artistKey: string;

async function signedIn(account: Account): Promise<APIRequestContext> {
  const client = await request.newContext({ baseURL: fixture.origin });
  expect((await client.post('/api/v1/auth/login', { data: account })).ok()).toBe(true);
  return client;
}

async function album(client: APIRequestContext): Promise<any> {
  const response = await client.get(`/api/v1/music/albums/${albumKey}`);
  expect(response.ok()).toBe(true);
  return response.json();
}

async function artist(client: APIRequestContext): Promise<any> {
  const response = await client.get(`/api/v1/music/artists/${artistKey}`);
  expect(response.ok()).toBe(true);
  return response.json();
}

async function openAlbum(page: Page, byKeyboard: boolean): Promise<void> {
  await openSidebarMusic(page, 'Albums');
  const card = page.getByRole('button', { name: `Open album ${albumName} by ${artistName}`, exact: true });
  await card.scrollIntoViewIfNeeded();
  if (byKeyboard) {
    await card.focus();
    await page.keyboard.press('Enter');
  } else {
    await card.click();
  }
  await expect(page.getByRole('heading', { name: albumName, exact: true })).toBeVisible();
}

async function startSong(page: Page, id: number): Promise<void> {
  await resetProgress(page, id);
  const row = page.locator(`.media-row[data-id="${id}"]`);
  await row.getByRole('button', { name: id === ids.long ? 'Qualified Listening' : 'Skipped Listening', exact: true }).click();
  await expectPlaying(page, id);
}

async function pauseAndReturn(page: Page): Promise<void> {
  if (!(await audioState(page)).paused) {
    await page.getByRole('button', { name: 'Log out', exact: true }).focus();
    await page.keyboard.press('p');
    await expect.poll(() => audioState(page).then(state => state.paused)).toBe(true);
  }
  const player = await page.locator('#player').getAttribute('class') || '';
  if (player.includes('open') && !player.includes('minimized')) await page.keyboard.press('Escape');
}

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  const state = JSON.parse(readFileSync(join(fixture.dir, 'state.json'), 'utf8'));
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1') {
    throw Error('M36 requires a disposable localhost music fixture');
  }
  const directory = join(state.media_root, 'Music', folderName);
  mkdirSync(directory, { recursive: true });
  for (const [name, hz, seconds, title] of [
    [longName, 440, 85, 'Qualified Listening'],
    [shortName, 660, 18, 'Skipped Listening'],
  ] as const) {
    const path = join(directory, name);
    if (!existsSync(path)) execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'lavfi', '-i', `sine=frequency=${hz}:sample_rate=44100`, '-t', String(seconds), '-af', 'volume=0.12', '-c:a', 'libmp3lame', '-b:a', '128k', '-metadata', `title=${title}`, '-metadata', `album=${albumName}`, '-metadata', `artist=${artistName}`, path]);
  }
  const admin = await signedIn(fixture.admin);
  try {
    expect((await admin.post('/api/v1/admin/rescan', { data: {} })).ok()).toBe(true);
    await expect.poll(async () => {
      const scan = await (await admin.get('/api/v1/admin/scan-progress')).json();
      if (scan.state === 'failed') throw Error(JSON.stringify(scan));
      return scan.state === 'complete' && !scan.running && !scan.pending;
    }, { timeout: 60_000 }).toBe(true);
    const media = await (await admin.get('/api/v1/media?limit=1000')).json();
    ids = {
      long: media.find((row: any) => row.file_name === longName && row.set_id === fixture.setIds.Music)?.id,
      short: media.find((row: any) => row.file_name === shortName && row.set_id === fixture.setIds.Music)?.id,
    };
    expect(Number.isSafeInteger(ids.long) && Number.isSafeInteger(ids.short)).toBe(true);
    const albums = await (await admin.get('/api/v1/music/albums')).json();
    const entry = albums.find((row: any) => row.title === albumName);
    expect(entry).toBeDefined();
    albumKey = entry.album_key;
    const artists = await (await admin.get('/api/v1/music/artists')).json();
    artistKey = artists.find((row: any) => row.name === artistName)?.artist_key;
    expect(artistKey).toBeTruthy();
  } finally {
    await admin.dispose();
  }
  const root = join(resolve(process.env.PLAYER_M36_EVIDENCE || join(tmpdir(), 'player-m36')), attempt);
  evidence = new Evidence(root, info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_revision: fixture.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), origin: fixture.origin,
    ids, albumKey, artistKey,
  });
});

test('M36 real listening threshold, account totals and visible personal rows', async ({ page }, info) => {
  test.setTimeout(200_000);
  const actor = info.project.name === 'chrome' ? fixture.user : fixture.admin;
  const observer = info.project.name === 'chrome' ? fixture.admin : fixture.user;
  const actorApi = await signedIn(actor);
  const observerApi = await signedIn(observer);
  const artifacts: string[] = [];
  try {
    const before = await album(actorApi);
    const otherBefore = await album(observerApi);
    const artistBefore = await artist(actorApi);
    const shortBefore = before.songs.find((song: any) => song.id === ids.short);
    const longBefore = before.songs.find((song: any) => song.id === ids.long);
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, actor);
    await openAlbum(page, true);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${before.personal_play_count}`);
    await startSong(page, ids.short);
    const skipped = await captureOutput(page, 1400, evidence.path('below-threshold.wav'));
    assertValidCapture(skipped);
    expect(Math.abs((mainSegment(skipped.analysis)?.tone_hz ?? 0) - 660)).toBeLessThan(5);
    artifacts.push(skipped.wav);
    await pauseAndReturn(page);
    expect((await album(actorApi)).songs.find((song: any) => song.id === ids.short).personal_play_count).toBe(shortBefore.personal_play_count);
    await openAlbum(page, false);
    await startSong(page, ids.long);
    const qualified = await captureOutput(page, 1400, evidence.path('qualifying-audio.wav'));
    assertValidCapture(qualified);
    expect(Math.abs((mainSegment(qualified.analysis)?.tone_hz ?? 0) - 440)).toBeLessThan(5);
    artifacts.push(qualified.wav);
    await expect.poll(async () => (await album(actorApi)).songs.find((song: any) => song.id === ids.long).personal_play_count,
      { timeout: 85_000, intervals: [1000] }).toBe(longBefore.personal_play_count + 1);
    await pauseAndReturn(page);
    const after = await album(actorApi);
    const longAfter = after.songs.find((song: any) => song.id === ids.long);
    expect(longAfter.last_played_at).toBeTruthy();
    expect(after.personal_play_count).toBe(before.personal_play_count + 1);
    expect(after.last_played_at).toBe(longAfter.last_played_at);
    expect((await artist(actorApi)).personal_play_count).toBe(artistBefore.personal_play_count + 1);
    const otherAfter = await album(observerApi);
    expect(otherAfter.personal_play_count).toBe(otherBefore.personal_play_count);
    expect(otherAfter.songs.find((song: any) => song.id === ids.long).personal_play_count)
      .toBe(otherBefore.songs.find((song: any) => song.id === ids.long).personal_play_count);
    await openAlbum(page, false);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${after.personal_play_count}`);
    await expect(page.locator(`.media-row[data-id="${ids.long}"]`)).toContainText(`Your plays: ${longAfter.personal_play_count}`);
    await expect(page.locator(`.media-row[data-id="${ids.short}"]`)).toContainText(`Your plays: ${shortBefore.personal_play_count}`);
    await page.setViewportSize({ width: 390, height: 844 });
    for (const theme of ['dark', 'light']) {
      if ((await page.locator('html').getAttribute('data-theme')) !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      const shot = evidence.path(`personal-${theme}-390.png`);
      await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' });
      artifacts.push(shot);
      expect(await page.evaluate('document.documentElement.scrollWidth <= innerWidth')).toBe(true);
    }
    evidence.record({ feature: 'M36', scenario: 'real threshold, skip, personal album/artist/song totals, two accounts and narrow themes', result: 'pass',
      data: { actor: actor.username, observer: observer.username, before: before.personal_play_count,
        after: after.personal_play_count, other: otherAfter.personal_play_count, long: longAfter.personal_play_count,
        last_played_at: longAfter.last_played_at, browser: info.project.name }, artifacts });
  } finally {
    await actorApi.dispose();
    await observerApi.dispose();
  }
});

test('M36 catalog permissions and offline Retry do not show stale personal totals', async ({ page }) => {
  const user = await signedIn(fixture.user);
  const admin = await signedIn(fixture.admin);
  const artifacts: string[] = [];
  const userId = (await (await admin.get('/api/v1/admin/users')).json())
    .find((row: any) => row.username === fixture.user.username)?.id;
  expect(Number.isSafeInteger(userId)).toBe(true);
  try {
    const expected = (await album(user)).personal_play_count;
    await keyboardLogin(page, fixture.user);
    await openAlbum(page, false);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${expected}`);
    await page.reload();
    await openAlbum(page, false);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${expected}`);
    expect((await admin.delete('/api/v1/admin/permissions', {
      data: { set_id: fixture.setIds.Music, user_id: userId },
    })).ok()).toBe(true);
    try {
      await openSidebarMusic(page, 'Albums');
      await expect(page.locator('#media-grid')).toContainText('No albums available.');
      expect(await page.locator('#media-grid').getByText(`Your plays: ${expected}`).count()).toBe(0);
      const hidden = evidence.path('personal-acl-hidden.png');
      await page.screenshot({ path: hidden, fullPage: true, animations: 'disabled' });
      artifacts.push(hidden);
    } finally {
      expect((await admin.post('/api/v1/admin/permissions', {
        data: { set_id: fixture.setIds.Music, user_id: userId, role: 'viewer' },
      })).ok()).toBe(true);
    }
    await openSidebarMusic(page, 'Albums');
    await openAlbum(page, false);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${expected}`);
    await page.context().setOffline(true);
    try {
      await openSidebarMusic(page, 'Albums');
      await expect(page.getByRole('alert')).toContainText('Could not load albums');
      expect(await page.locator('#media-grid').getByText(`Your plays: ${expected}`).count()).toBe(0);
      const offline = evidence.path('personal-offline-error.png');
      await page.screenshot({ path: offline, fullPage: true, animations: 'disabled' });
      artifacts.push(offline);
    } finally {
      await page.context().setOffline(false);
    }
    await page.getByRole('button', { name: 'Retry', exact: true }).click();
    await openAlbum(page, false);
    await expect(page.locator('.album-header')).toContainText(`Your plays: ${expected}`);
    evidence.record({ feature: 'M36', scenario: 'personal rows survive reload and recover after real ACL and offline errors', result: 'pass',
      data: { user: fixture.user.username, play_count: expected, set_id: fixture.setIds.Music }, artifacts });
  } finally {
    await user.dispose();
    await admin.dispose();
  }
});
