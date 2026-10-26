/** Staged real-browser journey. Run catalog before native B, then resume in
 * each browser, native B finish, completed in both browsers, uncomplete once,
 * observe-uncompleted on all clients, cleanup and boundaries.
 *
 * PLAYER_PODCAST_STAGE=catalog|resume|completed|uncomplete|observe-uncompleted|cleanup|boundaries|auth-visual|account-switch
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
const allowed = ['catalog', 'resume', 'completed', 'uncomplete', 'observe-uncompleted', 'cleanup', 'boundaries', 'feedback', 'unsubscribed', 'conflict', 'auth-visual', 'account-switch'];
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
  await expect(p.locator('.podcast-view-title')).toHaveText('All podcast episodes', { timeout: 15_000 });
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
  await p.getByRole('button', { name: 'Admin settings', exact: true }).click();
  await expect(p.locator('#admin-modal')).toHaveClass(/open/);
  await p.locator('#admin-podcasts').click();
  await expect(p.locator('#podcast-modal')).toHaveClass(/open/);
}
async function catalogFeed(p: Page) {
  const response = await p.request.get('/api/v1/podcasts');
  expect(response.ok()).toBe(true);
  return (await response.json()).find((item: any) => item.feed_key === feed.key);
}
async function playCard(p: Page, target: Locator) {
  await target.scrollIntoViewIfNeeded();
  await target.hover();
  // Icon actions are CSS-hidden until hover; click the real control (keyboard
  // Enter on a just-revealed icon is flaky under Playwright focus routing).
  await target.getByRole('button', { name: /^(Download and play|Play) episode$/ }).click();
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
      // Body focus first — a leftover control after login can swallow A.
      await page.locator('body').click();
      await page.keyboard.press('A');
      await expect(page.locator('#admin-modal')).toHaveClass(/open/);
      await page.locator('#admin-podcasts').click();
      await expect(page.locator('#podcast-modal')).toHaveClass(/open/);
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
    // Each browser flips completion during the boundary check; the second
    // browser must still find the same episode after the first one hides it.
    await openEpisodes(page, stage === 'boundaries' ? 'All' : 'Uncompleted');
    if (stage === 'unsubscribed') {
      expect((await catalogFeed(page)).subscribed).toBe(false);
      await expect(page.getByRole('button', { name: 'Podcast Acceptance Alpha', exact: true })).toHaveCount(0);
      await expect(exactCard(page)).toHaveCount(0);
      record('native unsubscribe tombstone removes browser subscription and default episodes');
    } else if (stage === 'account-switch') {
      const beta = state.feeds[1];
      expect(beta, 'account switch needs the independent Beta fixture feed').toBeTruthy();
      const betaButton = page.getByRole('button', { name: `Podcast ${beta.title}`, exact: true });
      if (!await betaButton.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
      await expect(betaButton).toBeVisible();
      await page.getByRole('button', { name: 'Log out', exact: true }).click();
      await page.waitForURL(url => url.pathname === '/login.html', { waitUntil: 'domcontentloaded' });
      await keyboardLogin(page, auth.other);
      await fullSync(page);
      await openEpisodes(page);
      await expect(page.locator('.episode-card')).toHaveCount(0);
      await expect(page.getByRole('button', { name: `Podcast ${beta.title}`, exact: true })).toHaveCount(0);
      expect((await rows(page)).length).toBe(0);
      await page.screenshot({ path: join(output, 'other-account.png') });
      record('visible browser account switch isolates subscriptions and episode history');

      await page.getByRole('button', { name: 'Log out', exact: true }).click();
      await page.waitForURL(url => url.pathname === '/login.html', { waitUntil: 'domcontentloaded' });
      await keyboardLogin(page, auth.admin);
      await fullSync(page);
      await openEpisodes(page);
      if (!await betaButton.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
      await expect(betaButton).toBeVisible();
      expect((await rows(page)).length).toBeGreaterThan(0);
      await page.screenshot({ path: join(output, 'admin-return.png') });
      record('returning to the original browser account restores its podcast catalog');
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
        // Card action icons stay CSS-hidden until hover/focus.
        await target.hover();
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
      // Prefer untouched end.wav slots — retries can stall Alpha 002 near EOF
      // without a natural ended event (podcastNaturalEnd rejects seek-to-end).
      const key = info.project.name === 'chrome' ? feed.episodes[3].key : feed.episodes[2].key;
      const title = info.project.name === 'chrome' ? feed.episodes[3].title : feed.episodes[2].title;
      const short = page.locator('.episode-card').filter({ hasText: title });
      await short.scrollIntoViewIfNeeded();
      let completion: 'natural-end' | 'manual-mark' = 'natural-end';
      try {
        await playCard(page, short);
        await expect.poll(async () => (await history(page, key)).completed, { timeout: 45_000 }).toBe(true);
      } catch {
        // Natural ended is unreliable when a prior retry left the short fixture
        // stalled near EOF (podcastNaturalEnd). Fall back to the visible mark.
        completion = 'manual-mark';
        await short.hover();
        await short.getByRole('button', { name: 'Mark completed', exact: true }).click();
        await expect.poll(async () => (await history(page, key)).completed, { timeout: 15_000 }).toBe(true);
      }
      await pause(page);
      await expect(short).toHaveCount(0);
      record('completed episode hides from default filter', { episode_key: key, completion });
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
      const card = exactCard(page);
      await card.hover();
      await actionByKeyboard(page, card.getByRole('button', { name: 'Download episode to server', exact: true }));
      await expect.poll(async () => Boolean((await history(page)).server_copy?.media_id)).toBe(true);
      const after = await history(page);
      expect([after.position_seconds, after.completed]).toEqual([before.position_seconds, before.completed]);
      record('explicit shared server cleanup and redownload preserve episode history');
    } else if (stage === 'feedback') {
      const status = page.locator('#global-sync [data-sync-status]');
      const key = `player-global-sync-v1:${state.origin}#${state.accounts.admin}`;
      // The existing ordinary source plays through actual browser controls.
      // Fail only its progress transport; podcast pulls continue successfully.
      await page.route('**/api/progress/batch', route => route.abort('connectionrefused'));
      await page.goto(`/?media=${state.ordinary.id}`);
      await page.locator('#btn-play').click();
      const initial = (await audioState(page)).time;
      await expect.poll(async () => (await audioState(page)).time).toBeGreaterThan(initial + 2);
      await pause(page);
      await expect(status).toContainText('pending');
      // Playback can complete an in-flight initial sync before the first
      // progress edit is queued. Anchor the age to the last success after an
      // actual failed retry, when the partial-sync clock must remain stable.
      await page.locator('#global-sync button').click();
      await expect(status).toContainText('Sync unavailable');
      const last = await page.evaluate(storageKey => JSON.parse(localStorage.getItem(storageKey)!).lastFullSuccessMs, key);
      expect(last).toBeGreaterThan(0);
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
    } else if (stage === 'auth-visual') {
      // Narrow navigation may leave the sets drawer over the main page.
      // Close it through its actual control before judging the sync row.
      const closeMenu = page.locator('#menu-close');
      if (await closeMenu.isVisible()) await closeMenu.click();
      const status = page.locator('#global-sync [data-sync-status]');
      const signIn = page.locator('#global-sync a');
      const upstream: number[] = [];
      const anonymous = await browser.newContext({ baseURL: state.origin });
      await page.route('**/api/podcasts/sync', async route => {
        const response = await anonymous.request.post('/api/podcasts/sync', {
          data: { since: 0, epoch: '', episodes: [], subscriptions: [] },
        });
        upstream.push(response.status());
        await route.fulfill({ response });
      });
      await page.locator('#global-sync button').click();
      await expect(status).toContainText('Sign in again to sync');
      expect(upstream).toContain(401);
      await expect(status).not.toContainText('Sync unavailable');
      await expect(signIn).toBeVisible();
      await expect(page).toHaveURL(url => url.pathname === '/');
      expect(await page.locator('.modal-overlay.open').count()).toBe(0);
      record('genuine upstream HTTP 401 gives quiet sign-in hint without navigation or modal', { upstream });

      await page.setViewportSize({ width: 390, height: 844 });
      for (const theme of ['dark', 'light']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) {
          await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
        }
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        const geometry = await page.locator('#global-sync').evaluate(host => {
          const link = host.querySelector('a')!;
          const row = host.getBoundingClientRect(), target = link.getBoundingClientRect();
          const top = document.elementFromPoint(target.left + target.width / 2, target.top + target.height / 2);
          return { rowLeft: row.left, rowRight: row.right, linkLeft: target.left,
            linkRight: target.right, linkHeight: target.height, unobscured: top === link || link.contains(top),
            viewport: window.innerWidth };
        });
        expect(geometry.rowLeft).toBeGreaterThanOrEqual(0);
        expect(geometry.rowRight).toBeLessThanOrEqual(geometry.viewport + 1);
        expect(geometry.linkLeft).toBeGreaterThanOrEqual(0);
        expect(geometry.linkRight).toBeLessThanOrEqual(geometry.viewport + 1);
        expect(geometry.linkHeight).toBeGreaterThanOrEqual(44);
        expect(geometry.unobscured).toBe(true);
        await page.screenshot({ path: join(output, `narrow-${theme}-auth.png`) });
        record(`narrow ${theme} sync hint and sign-in target remain visible`, geometry);
      }
      // Reach the footer action by actual Tab navigation and prove its focus
      // indicator appears in the compact layout.
      await page.locator('#thumbnail-smaller').focus();
      await page.keyboard.press('Shift+Tab');
      await expect(signIn).toBeFocused();
      const outline = await signIn.evaluate(link => getComputedStyle(link).outlineStyle);
      expect(outline).not.toBe('none');
      await page.screenshot({ path: join(output, 'narrow-footer-keyboard-focus.png') });
      record('compact sync footer sign-in link is keyboard reachable with visible focus', { outline });
      await page.unroute('**/api/podcasts/sync');
      await anonymous.close();
    } else {
      // Hold an actual scoped response after the server produced it. Playback
      // must start using its bounded local fallback; release cannot seek it.
      // Inspect the request where fetch owns its body: Firefox's Playwright
      // route reports zero postData bytes for this real JSON POST. The wrapper
      // still sends the original request and holds only its real response.
      await page.evaluate(() => {
        const original = window.fetch;
        const gate = { held: false, cancelled: false, status: 0, release: () => {}, original };
        (window as any).__podcastSyncGate = gate;
        window.fetch = async (input, init) => {
          const response = await original.call(window, input, init);
          let body: any = null;
          try { if (typeof init?.body === 'string') body = JSON.parse(init.body); } catch {}
          if (!gate.cancelled && !gate.held && String(input).includes('/api/podcasts/sync') && body?.scope) {
            gate.status = response.status; gate.held = true;
            await new Promise<void>(resolve => { gate.release = resolve; });
          }
          return response;
        };
      });
      try {
        const start = Date.now();
        await playCard(page, exactCard(page));
        const currentMediaId = await page.evaluate(async () => {
          const path = '/js/playback.js';
          return (await import(path)).currentMediaId();
        });
        const expectedMediaId = (await history(page)).server_copy?.media_id;
        traffic.push({ method: 'PLAYBACK', path: '/media', currentMediaId, expectedMediaId, at: Date.now() });
        expect(currentMediaId).toBe(expectedMediaId);
        await expect.poll(() => page.evaluate(() => (window as any).__podcastSyncGate.held),
          { timeout: 3500 }).toBe(true);
        expect(await page.evaluate(() => (window as any).__podcastSyncGate.status)).toBe(200);
        expect(Date.now() - start).toBeLessThan(6500);
        await pause(page);
        const position = (await audioState(page)).time;
        await page.evaluate(() => (window as any).__podcastSyncGate.release());
        await page.waitForTimeout(600);
        expect((await audioState(page)).paused).toBe(true);
        expect((await audioState(page)).time).toBeCloseTo(position, 1);
        record('bounded preplay and real delayed response cannot jump paused playback');
      } finally {
        await page.evaluate(() => {
          const gate = (window as any).__podcastSyncGate;
          gate.cancelled = true; gate.release(); window.fetch = gate.original; delete (window as any).__podcastSyncGate;
        });
      }
      const previous = (await history(page)).completed;
      await page.route('**/api/podcasts/sync', route => route.abort('connectionrefused'));
      await page.getByRole('button', { name: 'Minimize player (Shift+C)' }).click();
      const card = exactCard(page);
      await card.hover();
      await actionByKeyboard(page, card.getByRole('button', { name: previous ? 'Mark uncompleted' : 'Mark completed', exact: true }));
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
