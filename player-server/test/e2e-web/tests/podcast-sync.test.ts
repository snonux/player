/** Staged real-browser journey. Run catalog before native B, then resume in
 * each browser, native B finish, completed in both browsers, uncomplete once,
 * observe-uncompleted on all clients, cleanup and boundaries.
 *
 * PLAYER_PODCAST_STAGE=catalog|resume|completed|uncomplete|observe-uncompleted|cleanup|boundaries
 * PLAYER_PODCAST_FIXTURE=/marked/path npx playwright test -c playwright.podcasts.config.ts --project=chrome
 * No API edits supply listening/completion proof. GETs are read-only oracles;
 * download, playback, completion, retry and cleanup use actual visible controls.
 */
import { readFileSync, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type Page, type Locator } from '@playwright/test';
import { keyboardLogin, audioState, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';
import { installAudioTap } from './helpers/audio-tap';

const directory = process.env.PLAYER_PODCAST_FIXTURE!;
const stage = process.env.PLAYER_PODCAST_STAGE;
const allowed = ['catalog', 'resume', 'completed', 'uncomplete', 'observe-uncompleted', 'cleanup', 'boundaries', 'feedback', 'unsubscribed', 'conflict'];
if (!allowed.includes(stage || '')) throw new Error('Select one explicit PLAYER_PODCAST_STAGE');
const state = JSON.parse(readFileSync(join(directory, 'state.json'), 'utf8'));
const auth = JSON.parse(readFileSync(join(directory, 'auth.json'), 'utf8'));
const feed = state.feeds[0], episode = feed.episodes[0];
const exactCard = (p: Page, key = episode.key) => p.locator('.episode-card').filter({ hasText: key === episode.key ? episode.title : feed.episodes[1].title });

async function openEpisodes(p: Page, filter = 'Uncompleted') {
  const toggle = p.getByRole('button', { name: 'Toggle sets sidebar', exact: true });
  const all = p.getByRole('button', { name: 'All podcast episodes', exact: true });
  if (!await all.isVisible()) await toggle.click();
  await all.click();
  if (filter === 'Uncompleted') {
    await expect(p.getByRole('button', { name: 'Uncompleted', exact: true })).toHaveAttribute('aria-pressed', 'true');
  }
  await p.getByRole('button', { name: filter, exact: true }).click();
  await expect(p.locator('.podcast-view-title')).toHaveText('All podcast episodes');
}

async function rows(p: Page) {
  const response = await p.request.get('/api/v1/podcasts/episodes?filter=all&limit=200');
  expect(response.ok()).toBe(true);
  const body = await response.json();
  return Array.isArray(body) ? body : body.episodes;
}
async function history(p: Page, key = episode.key) {
  const result = (await rows(p)).find((row: any) => row.episode_key === key && row.feed_key === feed.key);
  expect(result, 'fixture episode catalog must exist before playback').toBeTruthy();
  return result;
}
async function fullSync(p: Page) {
  const button = p.locator('#global-sync button');
  await expect(button).toBeEnabled();
  await button.click();
  await expect(p.locator('#global-sync [data-sync-status]')).toHaveText('Synced', { timeout: 30_000 });
}
async function actionByKeyboard(p: Page, target: Locator) {
  await target.scrollIntoViewIfNeeded();
  await target.focus();
  await expect(target).toBeFocused();
  await p.keyboard.press('Enter');
}
async function manager(p: Page) {
  await p.keyboard.press('Escape');
  await p.keyboard.press('A');
  await p.locator('#admin-podcasts').click();
  await expect(p.locator('#podcast-modal')).toHaveClass(/open/);
}
async function catalogFeed(p: Page) {
  const response = await p.request.get('/api/v1/podcasts');
  expect(response.ok()).toBe(true);
  return (await response.json()).find((item: any) => item.feed_key === feed.key);
}
async function playCard(p: Page, target: Locator) {
  await actionByKeyboard(p, target.getByRole('button', { name: /^(Download and play|Play) episode$/ }));
  await expect.poll(async () => (await audioState(p)).paused, { timeout: 15_000 }).toBe(false);
  const start = (await audioState(p)).time;
  await expect.poll(async () => (await audioState(p)).time).toBeGreaterThan(start + .5);
}
async function pause(p: Page) {
  if (!(await audioState(p)).paused) await p.locator('#btn-play').click();
  await expect.poll(async () => (await audioState(p)).paused).toBe(true);
}

test(`podcast four-client acceptance: ${stage}`, async ({ page, browser }, info) => {
  const output = join(directory, 'evidence', info.project.name,
    `${Date.now()}-${stage}`);
  mkdirSync(output, { recursive: true });
  const records: any[] = [];
  const record = (name: string, data: any = {}) => records.push({ name, result: 'pass', data });
  const traffic: any[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.pathname.includes('/podcasts') || url.pathname.includes('/progress')) {
      traffic.push({ method: request.method(), path: url.pathname, at: Date.now() });
    }
  });
  await page.addInitScript(installAudioTap);
  try {
    await keyboardLogin(page, auth.admin);
    if (stage === 'catalog') {
      await page.keyboard.press('A');
      await page.locator('#admin-podcasts').click();
      for (const publisher of state.feeds) {
        await page.locator('#podcast-url').fill(publisher.url);
        await page.locator('#podcast-url').press('Enter');
        await expect(page.locator('#podcast-list')).toContainText(publisher.title);
      }
      await page.keyboard.press('Escape');
      await expect.poll(async () => (await rows(page)).length).toBe(106);
      const episodes = await rows(page);
      expect(episodes.length).toBe(106);
      expect(episodes.every((e: any) => !e.server_copy?.media_id)).toBe(true);
      expect(traffic.filter(r => /\/download$/.test(r.path))).toHaveLength(0);
      record('visible admin subscription refresh creates catalog without audio', { episodes: episodes.length });
      return;
    }
    await fullSync(page);
    await openEpisodes(page);
    if (stage === 'unsubscribed') {
      expect((await catalogFeed(page)).subscribed).toBe(false);
      await expect(page.getByRole('button', { name: 'Podcast Acceptance Alpha', exact: true })).toHaveCount(0);
      await expect(exactCard(page)).toHaveCount(0);
      record('native unsubscribe tombstone removes browser subscription and default episodes');
    } else if (stage === 'conflict') {
      await manager(page);
      const unsub = () => page.getByRole('button', { name: 'Unsubscribe from Acceptance Alpha', exact: true });
      const sub = () => page.getByRole('button', { name: 'Subscribe to Acceptance Alpha', exact: true });
      if ((await catalogFeed(page)).subscribed) {
        await actionByKeyboard(page, unsub());
        await expect(sub()).toBeVisible();
      }
      await page.route('**/api/podcasts/sync', route => route.abort('connectionrefused'));
      await actionByKeyboard(page, sub());
      await expect(page.locator('#global-sync [data-sync-status]')).toContainText('pending');
      const otherContext = await browser.newContext({ baseURL: state.origin });
      try {
        const other = await otherContext.newPage();
        await keyboardLogin(other, auth.admin);
        await manager(other);
        await actionByKeyboard(other, other.getByRole('button', { name: 'Subscribe to Acceptance Alpha', exact: true }));
        await expect(other.getByRole('button', { name: 'Unsubscribe from Acceptance Alpha', exact: true })).toBeVisible();
        await actionByKeyboard(other, other.getByRole('button', { name: 'Unsubscribe from Acceptance Alpha', exact: true }));
        await expect(other.getByRole('button', { name: 'Subscribe to Acceptance Alpha', exact: true })).toBeVisible();
        expect((await catalogFeed(other)).subscribed).toBe(false);
        await page.unroute('**/api/podcasts/sync');
        await page.keyboard.press('Escape');
        await page.locator('#global-sync button').click();
        await expect(page.locator('#global-sync button')).toBeEnabled();
        expect((await catalogFeed(page)).subscribed).toBe(false);
        await fullSync(page);
        await openEpisodes(page);
        await expect(exactCard(page)).toHaveCount(0);
        record('newer subscription tombstone defeats stale offline browser subscribe on real retry');
      } finally {
        await page.unroute('**/api/podcasts/sync');
        await otherContext.close();
        // Explicit user restoration, not silent resurrection by reconciliation.
        await manager(page);
        await actionByKeyboard(page, sub());
        await expect(unsub()).toBeVisible();
        await page.keyboard.press('Escape');
      }
    } else if (stage === 'resume') {
      const transferFile = join(directory, 'transfer-B.json');
      expect(existsSync(transferFile), 'native B must resume while no server media exists first').toBe(true);
      const transfer = JSON.parse(readFileSync(transferFile, 'utf8'));
      expect(transfer.position_seconds).toBeGreaterThan(0);
      const current = await history(page);
      const total = (await rows(page)).filter((row: any) => !row.completed).length;
      expect(current.position_seconds).toBeGreaterThanOrEqual(transfer.position_seconds - 1);
      const feedButton = page.getByRole('button', { name: 'Podcast Acceptance Alpha', exact: true });
      if (!await feedButton.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
      await actionByKeyboard(page, feedButton);
      await expect(page.locator('.podcast-view-title')).toHaveText('Acceptance Alpha');
      await expect(page.getByRole('button', { name: 'Uncompleted', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await openEpisodes(page);
      expect(await page.locator('.episode-card').count()).toBe(100);
      await expect(exactCard(page).locator('.subtitle').first()).toContainText('Acceptance Alpha');
      const next = page.getByRole('button', { name: 'Next page', exact: true }).first();
      await actionByKeyboard(page, next);
      await expect(page.locator('.episode-card')).toHaveCount(total - 100);
      await actionByKeyboard(page, page.getByRole('button', { name: 'Previous page', exact: true }).first());
      await expect(page.locator('.episode-card')).toHaveCount(100);
      const target = exactCard(page);
      if (!current.server_copy?.media_id) {
        expect(traffic.filter(r => /\/download$/.test(r.path))).toHaveLength(0);
        await actionByKeyboard(page, target.getByRole('button', { name: 'Download episode to server', exact: true }));
        await expect(target).toHaveAttribute('data-id', /\d+/);
      }
      const started = Date.now();
      await playCard(page, target);
      const first = (await audioState(page)).time;
      expect(first).toBeGreaterThanOrEqual(current.position_seconds - 1);
      expect(first).toBeLessThan(current.position_seconds + (Date.now() - started) / 1000 + 2);
      const capture = await captureOutput(page, 2500, join(output, 'resume.wav'));
      assertValidCapture(capture);
      const audible = mainSegment(capture.analysis);
      expect(capture.analysis.audible_s).toBeGreaterThan(2);
      expect(Math.abs((audible?.tone_hz || 0) - state.audio.resume.tone_hz)).toBeLessThan(10);
      await pause(page);
      await fullSync(page);
      record('real decoded and audible shared-position resume', { before: current.position_seconds, first, after: await history(page), audio: capture.analysis });
      // Each browser must also exercise actual natural completion, using its
      // own distinct twelve-second episode so another client cannot prefinish it.
      const key = info.project.name === 'chrome' ? feed.episodes[1].key : feed.episodes[2].key;
      const title = info.project.name === 'chrome' ? feed.episodes[1].title : feed.episodes[2].title;
      const short = page.locator('.episode-card').filter({ hasText: title });
      await playCard(page, short);
      await expect.poll(async () => (await history(page, key)).completed, { timeout: 30_000 }).toBe(true);
      await pause(page);
      await expect(short).toHaveCount(0);
      record('actual natural HTML end hides completed episode', { episode_key: key });
    } else if (stage === 'completed' || stage === 'uncomplete' || stage === 'observe-uncompleted') {
      const completed = stage !== 'observe-uncompleted';
      await expect(exactCard(page)).toHaveCount(completed ? 0 : 1);
      await page.getByRole('button', { name: 'Completed', exact: true }).click();
      await expect(exactCard(page)).toHaveCount(completed ? 1 : 0);
      if (stage === 'uncomplete') {
        const before = await history(page);
        await actionByKeyboard(page, exactCard(page).getByRole('button', { name: 'Mark uncompleted', exact: true }));
        await page.getByRole('button', { name: 'Uncompleted', exact: true }).click();
        await expect(exactCard(page)).toHaveCount(1);
        const after = await history(page);
        expect(after.position_seconds).toBe(before.position_seconds);
        expect(after.completed).toBe(false);
        record('manual uncompletion returns default view and preserves position');
      } else record('cross-client completion filter', { completed });
    } else if (stage === 'cleanup') {
      const before = await history(page);
      expect(before.server_copy?.media_id).toBeGreaterThan(0);
      let warning = '';
      page.once('dialog', async dialog => { warning = dialog.message(); await dialog.accept(); });
      await actionByKeyboard(page, exactCard(page).getByRole('button', { name: 'Delete downloaded file', exact: true }));
      expect(warning).toContain('shared server');
      expect(warning).toContain('copies on Android');
      await expect.poll(async () => Boolean((await history(page)).server_copy)).toBe(false);
      const removed = await history(page);
      expect([removed.position_seconds, removed.completed]).toEqual([before.position_seconds, before.completed]);
      await actionByKeyboard(page, exactCard(page).getByRole('button', { name: 'Download episode to server', exact: true }));
      await expect.poll(async () => Boolean((await history(page)).server_copy?.media_id)).toBe(true);
      const after = await history(page);
      expect([after.position_seconds, after.completed]).toEqual([before.position_seconds, before.completed]);
      record('explicit shared server cleanup and redownload preserve episode history');
    } else if (stage === 'feedback') {
      const status = page.locator('#global-sync [data-sync-status]');
      const key = `player-global-sync-v1:${state.origin}#${state.accounts.admin}`;
      const last = await page.evaluate(storageKey => JSON.parse(localStorage.getItem(storageKey)!).lastFullSuccessMs, key);
      expect(last).toBeGreaterThan(0);
      // The existing ordinary source plays through actual browser controls.
      // Fail only its progress transport; podcast pulls continue successfully.
      await page.route('**/api/progress/batch', route => route.abort('connectionrefused'));
      await page.goto(`/?media=${state.ordinary.id}`);
      await page.locator('#btn-play').click();
      const initial = (await audioState(page)).time;
      await expect.poll(async () => (await audioState(page)).time).toBeGreaterThan(initial + 2);
      await pause(page);
      await expect(status).toContainText('pending');
      // Freeze Date only. Media clocks/timers and the actual network continue.
      await page.clock.setFixedTime(new Date(last + 48 * 3600 * 1000 - 1));
      await page.locator('#global-sync button').click();
      await expect(status).toContainText('Sync unavailable');
      await expect(status).not.toContainText('Last synced');
      await page.clock.setFixedTime(new Date(last + 48 * 3600 * 1000));
      await page.locator('#global-sync button').click();
      await expect(status).toContainText('Last synced 2 days ago');
      const retained = await page.evaluate(storageKey => JSON.parse(localStorage.getItem(storageKey)!).lastFullSuccessMs, key);
      expect(retained).toBe(last);
      expect(await page.locator('.modal-overlay.open').count()).toBe(0);
      await page.screenshot({ path: join(output, 'partial-stale.png') });
      await page.unroute('**/api/progress/batch');
      await page.clock.setFixedTime(new Date());
      await fullSync(page);
      record('ordinary progress participates in global retry; partial success preserves exact 48-hour age');
      const freshContext = await browser.newContext({ baseURL: state.origin });
      try {
        const fresh = await freshContext.newPage();
        await fresh.route('**/api/podcasts/sync', route => route.abort('connectionrefused'));
        await keyboardLogin(fresh, auth.other);
        await expect(fresh.locator('#global-sync [data-sync-status]')).toContainText('Not synced yet');
        await fresh.locator('#global-sync button').click();
        await expect(fresh.locator('#global-sync [data-sync-status]')).toContainText('Not synced yet');
        await openEpisodes(fresh);
        await expect(fresh.locator('.episode-card')).toHaveCount(0);
        await fresh.screenshot({ path: join(output, 'other-account-never-synced.png') });
        record('configured never-synced and account isolation use actual visible UI');
      } finally { await freshContext.close(); }
    } else {
      // Hold an actual scoped response after the server produced it. Playback
      // must start using its bounded local fallback; release cannot seek it.
      let release!: () => void;
      const gate = new Promise<void>(resolve => { release = resolve; });
      let held = false;
      await page.route('**/api/podcasts/sync', async route => {
        const body = route.request().postDataJSON();
        if (!body?.scope || held) return route.continue();
        const response = await route.fetch(); held = true;
        await gate;
        await route.fulfill({ response });
      });
      try {
        const start = Date.now();
        await playCard(page, exactCard(page));
        expect(held).toBe(true);
        expect(Date.now() - start).toBeLessThan(6500);
        await pause(page);
        const position = (await audioState(page)).time;
        release();
        await page.waitForTimeout(600);
        expect((await audioState(page)).paused).toBe(true);
        expect((await audioState(page)).time).toBeCloseTo(position, 1);
        record('bounded preplay and real delayed response cannot jump paused playback');
      } finally { release(); await page.unroute('**/api/podcasts/sync'); }
      const previous = (await history(page)).completed;
      await page.route('**/api/podcasts/sync', route => route.abort('connectionrefused'));
      await actionByKeyboard(page, exactCard(page).getByRole('button', { name: previous ? 'Mark uncompleted' : 'Mark completed', exact: true }));
      await expect(page.locator('#global-sync [data-sync-status]')).toContainText('pending');
      expect(await page.locator('.modal-overlay.open').count()).toBe(0);
      await page.unroute('**/api/podcasts/sync');
      await fullSync(page);
      expect((await history(page)).completed).toBe(!previous);
      record('actual failed browser completion remains pending and global Retry delivers it');
    }
  } catch (error) {
    records.push({ name: stage, result: 'fail', detail: String(error) });
    throw error;
  } finally {
    await page.screenshot({ path: join(output, 'final.png'), animations: 'disabled' }).catch(() => {});
    writeFileSync(join(output, 'results.json'), JSON.stringify({ browser: browser.version(), stage, records, traffic }, null, 1));
  }
});
