/** A06d: installed-browser acceptance against an owned, opt-in scan fixture. */
import { test, expect, type Page } from '@playwright/test';
import { existsSync, renameSync, unlinkSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { basename, join } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin } from './helpers/music-ui';

test.skip(process.env.PLAYER_A06D_ISOLATED !== '1', 'Requires an owned automatic-scan fixture');
let fixture: MusicFixture, evidence: Evidence;

type Scan = {
  state: string; trigger: string; running: boolean; pending: boolean;
  watch_seconds: number; interval_minutes: number; watch_error?: string;
  files_done: number; files_total: number;
};

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('A06d needs an owned localhost fixture');
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build: fixture.buildRevision, origin: fixture.origin,
  });
});

async function progress(page: Page): Promise<Scan> {
  const response = await page.request.get('/api/v1/admin/scan-progress');
  expect(response.ok()).toBe(true);
  return response.json();
}

async function rows(page: Page): Promise<any[]> {
  const response = await page.request.get(`/api/v1/media?set_id=${fixture.setIds.Music}&limit=1000`);
  expect(response.ok()).toBe(true);
  return response.json();
}

async function openAdmin(page: Page): Promise<void> {
  await page.locator('body').click({ position: { x: 20, y: 200 } });
  await page.keyboard.press('Shift+A');
  await expect(page.locator('#admin-modal')).toHaveClass(/open/);
  // Library scan is a focused admin workflow; Cancel scan stays on the toolbar.
  await page.locator('#admin-section-select').selectOption('scan');
  await expect(page.locator('#admin-scan-cancel')).toBeVisible();
  await expect(page.locator('#admin-scan-status')).not.toContainText('Loading');
}

function retag(source: string, stage: string, target: string, title: string): void {
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-i', source,
    '-map', '0:a', '-c:a', 'copy', '-metadata', `title=${title}`, stage], { timeout: 30_000 });
  renameSync(stage, target);
}

test('A06d scheduled, watched and manual scans expose real results and recovery', async ({ page, browser }) => {
  test.setTimeout(260_000);
  const artifacts: string[] = [];
  const mediaRoot = join(fixture.dir, 'media');
  const folder = join(mediaRoot, 'Music', 'Tag Verification');
  const source = join(folder, 'source_001.flac');
  // A prior run deliberately puts its media row in Trash; each rerun needs a
  // fresh path because inventory must not restore intentionally trashed rows.
  const target = join(folder, `a06d-${browser.browserType().name()}-${process.pid}-${Date.now()}.flac`);
  const stage = join(folder, '.a06d-stage.flac');
  const rel = `Tag Verification/${basename(target)}`;
  let rootOffline = false;
  try {
    await keyboardLogin(page, fixture.admin);
    await openAdmin(page);
    const settings = page.locator('#admin-scan-options');
    await settings.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(settings).toHaveAttribute('open', '');
    await expect(page.locator('#admin-scan-settings')).toHaveText(
      'Watch: polling every 10 seconds. Schedule: every 1 minutes.');
    await expect(settings.locator('summary')).toBeFocused();
    const initial = await progress(page);
    expect(initial.watch_seconds).toBe(10);
    expect(initial.interval_minutes).toBe(1);

    // The real scheduler fires after the fixture server's first minute.
    await expect.poll(async () => {
      const p = await progress(page);
      return p.trigger === 'schedule' && p.state === 'complete';
    }, { timeout: 85_000, intervals: [400, 800, 1_000] }).toBe(true);
    await expect(page.locator('#admin-scan-status')).toContainText('Scan complete');
    const scheduled = evidence.path('a06d-scheduled-dark.png');
    await page.screenshot({ path: scheduled, animations: 'disabled' }); artifacts.push(scheduled);

    // Add a real tagged FLAC without calling the scan API. Observation alone
    // must index it, and the browser must show the new catalog item.
    retag(source, stage, target, 'A06d Watch Arrival');
    let added: any;
    await expect.poll(async () => {
      added = (await rows(page)).find(row => row.rel_path === rel);
      return added?.music?.title;
    }, { timeout: 45_000, intervals: [500, 1_000] }).toBe('A06d Watch Arrival');
    expect(added.deleted_at).toBeFalsy();
    await expect(page.locator('#admin-scan-status')).toContainText('Scan complete');
    const watched = await progress(page);
    expect(watched.trigger).toBe('watch');
    expect(watched.running).toBe(false);
    await page.keyboard.press('Escape');
    await page.goto(`/?set=${fixture.setIds.Music}&folder=Tag%20Verification`);
    await expect(page.getByRole('button', { name: 'A06d Watch Arrival', exact: true })).toBeVisible();
    await openAdmin(page);

    retag(target, stage, target, 'A06d Retagged');
    await expect.poll(async () => {
      const row = (await rows(page)).find(item => item.id === added.id);
      return row?.music?.title;
    }, { timeout: 45_000, intervals: [500, 1_000] }).toBe('A06d Retagged');
    expect((await rows(page)).filter(row => row.rel_path === rel)).toHaveLength(1);
    const beforeDelete = (await progress(page)).files_total;
    unlinkSync(target);
    // An automatic inventory retains an absent row, even after it completes.
    await expect.poll(async () => {
      const p = await progress(page);
      return p.trigger === 'watch' && p.state === 'complete' && p.files_total < beforeDelete;
    }, { timeout: 25_000, intervals: [500, 1_000] }).toBe(true);
    expect((await rows(page)).find(row => row.id === added.id)?.deleted_at).toBeFalsy();

    // The existing manual policy is still reachable by keyboard and soft
    // deletes absent rows. The button restores focus after its async action.
    const manual = page.locator('#admin-rescan');
    await manual.focus();
    await page.keyboard.press('Enter');
    await expect.poll(async () => (await progress(page)).trigger).toBe('manual');
    await expect.poll(async () => (await progress(page)).state,
      { timeout: 30_000, intervals: [500, 1_000] }).toBe('complete');
    await expect(manual).toBeFocused();
    const trash = await page.request.get('/api/v1/admin/trash');
    expect(trash.ok()).toBe(true);
    expect((await trash.json()).some((row: any) => row.id === added.id)).toBe(true);

    // A disconnected source root must become an actionable observation error,
    // then clear after the same root is restored.
    renameSync(mediaRoot, `${mediaRoot}-offline`); rootOffline = true;
    await expect.poll(async () => (await progress(page)).watch_error || '', {
      timeout: 25_000, intervals: [500, 1_000],
    }).toContain('Could not inspect');
    await expect(page.locator('#admin-scan-status')).toContainText('Check root access');
    renameSync(`${mediaRoot}-offline`, mediaRoot); rootOffline = false;
    await expect.poll(async () => (await progress(page)).watch_error || '', {
      timeout: 25_000, intervals: [500, 1_000],
    }).toBe('');

    await page.keyboard.press('Escape');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Toggle theme' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await openAdmin(page);
    await page.locator('#admin-scan-options summary').click();
    await expect(page.locator('#admin-scan-settings')).toContainText('polling every 10 seconds');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const narrow = evidence.path('a06d-recovered-light-narrow.png');
    await page.screenshot({ path: narrow, animations: 'disabled' }); artifacts.push(narrow);
    evidence.record({ feature: 'A06d', scenario: 'scheduled/watch/manual add-retag-delete, root outage/recovery, keyboard, pointer and narrow themes',
      result: 'pass', artifacts, data: { media_id: added.id, scheduled, watched } });
  } catch (error) {
    evidence.record({ feature: 'A06d', scenario: 'scheduled/watch/manual add-retag-delete, root outage/recovery, keyboard, pointer and narrow themes',
      result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (rootOffline) renameSync(`${mediaRoot}-offline`, mediaRoot);
    if (existsSync(stage)) unlinkSync(stage);
    if (existsSync(target)) unlinkSync(target);
  }
});
