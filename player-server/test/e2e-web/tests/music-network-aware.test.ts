import { randomBytes } from 'node:crypto';
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, playCard, audioState, expectPlaying, captureOutput, assertValidCapture, mainSegment, openQueueMoreAction} from './helpers/music-ui';
import { minimizePlayer } from './helpers/player-options';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires an isolated disposable music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
const folder = 'Aurora Lane - Night Transit (2021)';
const settingsPanel = (page: Page) => page.locator('#settings-modal');
const networkPanel = (page: Page) => settingsPanel(page).locator('[data-music-network]');
const qualityPanel = (page: Page) => page.locator('details.loudness-control').filter({ has: page.locator('select[aria-label="Stream quality"]') });
const queueState = (page: Page) => page.evaluate(async () => (await import('/js/queue.js')).queueState());
const syncStatus = (page: Page) => page.evaluate(async () => (await import('/js/queue.js')).savedSessionSyncStatus());
const cachedIds = (page: Page) => page.evaluate(() => {
  const key = Object.keys(localStorage).find(value => /^player-playback-v1-\d+:index$/.test(value));
  return key ? Object.values(JSON.parse(localStorage.getItem(key) || '{}')).map((row: any) => row.media.id as number) : [];
});

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  expect(new URL(fixture.origin).hostname).toBe('127.0.0.1');
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});

async function openSettings(page: Page) {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(settingsPanel(page)).toHaveClass(/open/);
  await expect(networkPanel(page).getByLabel('Music network', { exact: true })).toBeVisible();
}

async function closeSettings(page: Page) {
  await page.keyboard.press('Escape');
  await expect(settingsPanel(page)).not.toHaveClass(/open/);
  await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeFocused();
}

async function openQuality(page: Page) {
  const panel = qualityPanel(page);
  if (!(await panel.locator('summary').isVisible())) {
    const restore = page.getByRole('button', { name: 'Restore player', exact: true });
    if (await restore.isVisible()) await restore.click();
  }
  if (!(await panel.locator('summary').isVisible())) {
    const more = page.getByRole('button', { name: 'More player controls', exact: true });
    if (await more.isVisible()) await more.click();
  }
  if (!(await panel.evaluate((details: HTMLDetailsElement) => details.open))) await panel.locator('summary').click();
  await expect(panel.getByLabel('Quality network', { exact: true })).toBeVisible();
  return panel;
}

async function reopenCard(page: Page, song: ReturnType<typeof track>) {
  if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
  await playCard(page, song.title, song.id);
  const restore = page.getByRole('button', { name: 'Restore player', exact: true });
  if (await restore.isVisible()) await restore.click();
  return audioState(page);
}

test('M52 Auto unknown uses Mobile; separate manual qualities change only next real source', async ({ page }) => {
  test.setTimeout(100_000);
  const artifacts: string[] = [];
  const requests: { url: string; status: number }[] = [];
  const song = track(fixture, `Music/${folder}/01 - First Light.mp3`);
  await page.addInitScript(installAudioTap);
  page.on('response', response => {
    if (response.url().includes(`/api/media/${song.id}/`)) requests.push({ url: response.url(), status: response.status() });
  });
  try {
    await keyboardLogin(page, fixture.user);
    await openSettings(page);
    await expect(networkPanel(page).getByLabel('Music network', { exact: true })).toHaveValue('auto');
    await expect(networkPanel(page).getByRole('status')).toContainText('Mobile settings (network type unavailable)');
    expect(await page.evaluate(async () => (await import('/js/network-policy.js')).detectedNetwork())).toBe('unknown');
    await settingsPanel(page).getByLabel('Playback cache budget').selectOption('0');
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('mobile');
    await closeSettings(page);
    await openFolder(page, fixture.setIds.Music, folder);
    await playCard(page, song.title, song.id);
    const panel = await openQuality(page);
    await panel.getByLabel('Quality network', { exact: true }).selectOption('mobile');
    await panel.getByLabel('Stream quality', { exact: true }).selectOption('opus96');
    const beforeMobile = await audioState(page);
    expect(beforeMobile.paused).toBe(false);
    const mobile = await reopenCard(page, song);
    expect(mobile.src).toContain(`/media/${song.id}/compat?profile=opus96`);
    await expect.poll(() => requests.some(item => item.url.includes('profile=opus96') && item.status === 200)).toBe(true);
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('wifi');
    await closeSettings(page);
    expect((await audioState(page)).src).toBe(mobile.src);
    await openQuality(page);
    await panel.getByLabel('Quality network', { exact: true }).selectOption('wifi');
    await panel.getByLabel('Stream quality', { exact: true }).selectOption('aac128');
    expect((await audioState(page)).src).toBe(mobile.src);
    const wifi = await reopenCard(page, song);
    expect(wifi.src).toContain(`/media/${song.id}/compat?profile=aac128`);
    await expect.poll(() => requests.some(item => item.url.includes('profile=aac128') && item.status === 200)).toBe(true);
    const output = await captureOutput(page, 1100, evidence.path('m52-wifi-aac128.wav'));
    assertValidCapture(output);
    expect(Math.abs(mainSegment(output.analysis)!.tone_hz! - song.tone_hz[0])).toBeLessThan(5);
    artifacts.push(output.wav);
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('auto');
    await expect(networkPanel(page).getByRole('status')).toContainText('Mobile settings (network type unavailable)');
    await closeSettings(page);
    expect((await audioState(page)).src).toBe(wifi.src);
    const auto = await reopenCard(page, song);
    expect(auto.src).toContain(`/media/${song.id}/compat?profile=opus96`);
    await page.reload();
    await openSettings(page);
    await expect(networkPanel(page).getByLabel('Music network', { exact: true })).toHaveValue('auto');
    await closeSettings(page);
    await reopenCard(page, song);
    await openQuality(page);
    await panel.getByLabel('Quality network', { exact: true }).selectOption('wifi');
    await expect(panel.getByLabel('Stream quality', { exact: true })).toHaveValue('aac128');
    await panel.getByLabel('Quality network', { exact: true }).selectOption('mobile');
    await expect(panel.getByLabel('Stream quality', { exact: true })).toHaveValue('opus96');
    await panel.locator('summary').focus();
    await page.keyboard.press('Escape');
    await expect(panel).not.toHaveAttribute('open', '');
    await expect(panel.locator('summary')).toBeFocused();
    evidence.record({ feature: 'M52', scenario: 'Auto unknown fallback and separate manual Wi-Fi/Mobile quality on actual source requests, stable current source and reload',
      result: 'pass', data: { requests, mobile, wifi, auto, audio: mainSegment(output.analysis) }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M52', scenario: 'network quality and fallback', result: 'fail', detail: String(error), data: { requests }, artifacts });
    throw error;
  }
});

test('M52 Wi-Fi-only policy preserves current song, blocks cached/direct/converted starts and retries', async ({ page }) => {
  test.setTimeout(95_000);
  const [first, second, third] = [
    '01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac',
  ].map(name => track(fixture, `Music/${folder}/${name}`));
  const attempted: string[] = [];
  const artifacts: string[] = [];
  await page.addInitScript(installAudioTap);
  page.on('request', request => {
    if (new RegExp(`/api/media/(${second.id}|${third.id})/(stream|compat)`).test(request.url())) attempted.push(request.url());
  });
  async function attempt(song: ReturnType<typeof track>) {
    if (await page.locator('#player.open:not(.minimized)').count()) await minimizePlayer(page);
    const card = page.locator('.media-card').filter({ has: page.getByRole('button', { name: song.title, exact: true }) });
    await card.hover(); await card.getByRole('button', { name: 'Play', exact: true }).click();
    await expect(page.locator('#player-status')).toContainText('Library audio streaming waits for Wi-Fi.');
    await expect(page.locator('#player-status')).toContainText('Press Play to retry');
  }
  try {
    await keyboardLogin(page, fixture.user);
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('wifi');
    await settingsPanel(page).getByLabel('Playback cache budget').selectOption('128');
    await settingsPanel(page).getByLabel('Playback buffering').selectOption('complete');
    await closeSettings(page);
    await openFolder(page, fixture.setIds.Music, folder);
    await playCard(page, second.title, second.id);
    await expect.poll(() => cachedIds(page), { timeout: 20000 }).toContain(second.id);
    await reopenCard(page, first);
    const current = await audioState(page);
    await openSettings(page);
    await networkPanel(page).getByLabel('Stream library audio on Wi-Fi only').check();
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('mobile');
    await expect(networkPanel(page).getByRole('status')).toContainText('New library audio streams paused until Wi-Fi');
    await closeSettings(page);
    expect((await audioState(page)).src).toBe(current.src);
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThan(current.time + .4);
    const beforeCached = attempted.length;
    await attempt(second);
    expect(attempted.length).toBe(beforeCached);
    await openQuality(page);
    await qualityPanel(page).getByLabel('Quality network', { exact: true }).selectOption('mobile');
    await qualityPanel(page).getByLabel('Stream quality', { exact: true }).selectOption('opus96');
    const beforeCompat = attempted.length;
    await attempt(third);
    expect(attempted.length).toBe(beforeCompat);
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('wifi');
    await closeSettings(page);
    const restore = page.getByRole('button', { name: 'Restore player', exact: true });
    if (await restore.isVisible()) await restore.click();
    await page.locator('#btn-play').click();
    await expectPlaying(page, third.id);
    const recovered = await captureOutput(page, 1100, evidence.path('m52-wifi-retry.flac-output.wav'));
    assertValidCapture(recovered);
    expect(Math.abs(mainSegment(recovered.analysis)!.tone_hz! - third.tone_hz[0])).toBeLessThan(5);
    artifacts.push(recovered.wav);
    const cached = await reopenCard(page, second);
    expect(cached.src.startsWith('blob:')).toBe(true);
    const cachedAudio = await captureOutput(page, 1100, evidence.path('m52-cached-retry.wav'));
    assertValidCapture(cachedAudio);
    expect(Math.abs(mainSegment(cachedAudio.analysis)!.tone_hz! - second.tone_hz[0])).toBeLessThan(5);
    artifacts.push(cachedAudio.wav);
    evidence.record({ feature: 'M52', scenario: 'current source stable across policy change, cached/direct/compat starts blocked before network, visible Play retry and cached audio on Wi-Fi',
      result: 'pass', data: { current, attempted, cached: await cachedIds(page), retry: mainSegment(recovered.analysis), cachedAudio: mainSegment(cachedAudio.analysis) }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M52', scenario: 'Wi-Fi-only stream restriction', result: 'fail', detail: String(error), data: { attempted }, artifacts });
    throw error;
  }
});

test('M52 prefetch policy and Wi-Fi-only saved-session sync defer durable edits then retry', async ({ page, context }, info) => {
  test.setTimeout(130_000);
  const [first, second, third] = [
    '01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac',
  ].map(name => track(fixture, `Music/${folder}/${name}`));
  const artifacts: string[] = [];
  const mediaRequests: string[] = [], queueRequests: string[] = [];
  const admin = await request.newContext({ baseURL: fixture.origin });
  let userId = 0, playlist = '';
  await page.addInitScript(installAudioTap);
  page.on('request', candidate => {
    const url = new URL(candidate.url());
    if (url.pathname === '/api/v1/queue') queueRequests.push(candidate.method());
    if ([second.id, third.id].some(id => new RegExp(`^/api/media/${id}/(stream|compat)$`).test(url.pathname)))
      mediaRequests.push(`${candidate.method()} ${url.pathname}${url.search}`);
  });
  try {
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    const account = { username: `m52-${info.project.name}-${Date.now()}`, password: randomBytes(18).toString('hex') };
    const created = await admin.post('/api/v1/admin/users', { data: { ...account, is_admin: false } });
    expect(created.ok()).toBe(true); userId = (await created.json()).id;
    expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: fixture.setIds.Music, user_id: userId, role: 'viewer' },
    })).ok()).toBe(true);
    await keyboardLogin(page, account);
    await openSettings(page);
    await settingsPanel(page).getByLabel('Playback cache budget').selectOption('128');
    await settingsPanel(page).getByLabel('Playback buffering').selectOption('stream');
    await settingsPanel(page).getByLabel('Prefetch next queued song').check();
    const network = networkPanel(page);
    await network.getByLabel('Music network', { exact: true }).selectOption('mobile');
    await expect(network.getByLabel('Allow prefetch on Mobile / unknown')).not.toBeChecked();
    await network.getByLabel('Sync saved music session on Wi-Fi only').check();
    await closeSettings(page);
    const made = await page.request.post('/api/v1/playlists', { data: { name: `M52 sync ${Date.now()}` } });
    expect(made.ok()).toBe(true); playlist = (await made.json()).playlist_id;
    expect((await page.request.post(`/api/v1/playlists/${playlist}/songs`, {
      data: { action: 'add', media_ids: [first.id, second.id, third.id] },
    })).ok()).toBe(true);
    await page.goto(`/?music=playlist&id=${playlist}`);
    await expect(page.locator('.media-row')).toHaveCount(3);
    await page.getByRole('button', { name: '▶ Play playlist', exact: true }).click();
    await expect.poll(async () => (await queueState(page)).active).toBe(true);
    await expect.poll(() => cachedIds(page), { timeout: 20000 }).toContain(first.id);
    await page.waitForTimeout(700);
    expect(mediaRequests.some(value => value.includes(`/media/${second.id}/`))).toBe(false);
    await expect.poll(async () => (await syncStatus(page)).pending.length).toBeGreaterThan(0);
    expect(queueRequests.filter(method => method === 'PUT')).toHaveLength(0);
    expect((await page.request.post(`/api/v1/progress/status`, {
      data: { media_id: first.id, status: 'not_started' },
    })).ok()).toBe(true);
    await page.reload();
    await page.keyboard.press('Q');
    const queue = page.locator('#queue-panel');
    await expect(queue.getByRole('button', { name: 'Resume queue', exact: true })).toBeVisible();
    expect((await queueState(page)).entries.map((entry: any) => entry.media.id)).toEqual([first.id, second.id, third.id]);
    await expect.poll(async () => (await syncStatus(page)).pending.length).toBeGreaterThan(0);
    page.once('dialog', dialog => dialog.accept());
    await openQueueMoreAction(page, 'Continue session');
    await expect(queue).toContainText('Continue session waits for Wi-Fi');
    expect(queueRequests.filter(method => method === 'PUT')).toHaveLength(0);
    await queue.getByRole('button', { name: 'Close queue', exact: true }).click();
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('wifi');
    await closeSettings(page);
    await expect.poll(() => queueRequests.filter(method => method === 'PUT').length, { timeout: 15000 }).toBeGreaterThan(0);
    await expect.poll(async () => (await syncStatus(page)).pending.length).toBe(0);
    await expect.poll(async () => {
      const remote = await (await page.request.get('/api/v1/queue')).json();
      return remote.entries.map((entry: any) => entry.media_id);
    }).toEqual([first.id, second.id, third.id]);
    await page.keyboard.press('Q');
    await queue.getByRole('button', { name: 'Resume queue', exact: true }).click();
    await expect.poll(async () => (await queueState(page)).active).toBe(true);
    await expect.poll(() => mediaRequests.some(value => value.includes(`/media/${second.id}/`)), { timeout: 20000 }).toBe(true);
    await expect.poll(() => cachedIds(page), { timeout: 20000 }).toContain(second.id);
    expect(await cachedIds(page)).not.toContain(third.id);
    await queue.getByRole('button', { name: 'Close queue', exact: true }).click();
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('mobile');
    await networkPanel(page).getByLabel('Allow prefetch on Mobile / unknown').check();
    await closeSettings(page);
    await page.locator('#btn-next').click();
    await expect.poll(async () => (await queueState(page)).index).toBe(1);
    await expect.poll(() => mediaRequests.some(value => value.includes(`/media/${third.id}/`)), { timeout: 20000 }).toBe(true);
    await expect.poll(() => cachedIds(page), { timeout: 20000 }).toContain(third.id);
    await context.setOffline(true);
    await page.keyboard.press('Q');
    await queue.locator('[data-queue-action="repeat"]').click();
    expect((await queueState(page)).repeat).toBe('all');
    await expect.poll(async () => (await syncStatus(page)).pending.length).toBeGreaterThan(0);
    const beforeReconnect = queueRequests.filter(method => method === 'PUT').length;
    await context.setOffline(false);
    await queue.getByRole('button', { name: 'Close queue', exact: true }).click();
    await openSettings(page);
    await networkPanel(page).getByLabel('Music network', { exact: true }).selectOption('wifi');
    await expect.poll(() => queueRequests.filter(method => method === 'PUT').length).toBeGreaterThan(beforeReconnect);
    await expect.poll(async () => (await (await page.request.get('/api/v1/queue')).json()).repeat).toBe('all');
    await expect.poll(async () => (await syncStatus(page)).pending.length).toBe(0);
    const pendingAfterRetry = await syncStatus(page);
    await page.setViewportSize({ width: 390, height: 844 });
    for (const theme of ['dark', 'light']) {
      await settingsPanel(page).locator('#appearance-mode').selectOption(theme);
      await networkPanel(page).getByRole('heading', { name: 'Music network' }).scrollIntoViewIfNeeded();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const shot = evidence.path(`m52-settings-${theme}-390.png`);
      await page.screenshot({ path: shot, animations: 'disabled' }); artifacts.push(shot);
    }
    await closeSettings(page);
    evidence.record({ feature: 'M52', scenario: 'Mobile prefetch denial, Wi-Fi and Mobile allowance, durable offline/session edits and Wi-Fi retry over actual queue/media requests',
      result: 'pass', data: { mediaRequests, queueRequests, cached: await cachedIds(page), pendingAfterRetry }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M52', scenario: 'prefetch and saved-session sync policy', result: 'fail', detail: String(error),
      data: { mediaRequests, queueRequests, pending: await syncStatus(page).catch(() => null) }, artifacts });
    throw error;
  } finally {
    await context.setOffline(false);
    if (playlist) await page.request.delete(`/api/v1/playlists/${playlist}`).catch(() => {});
    if (userId) await admin.delete(`/api/v1/admin/users/${userId}`).catch(() => {});
    await admin.dispose();
  }
});
