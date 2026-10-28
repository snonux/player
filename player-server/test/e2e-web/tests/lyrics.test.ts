import { test, expect, Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';
const webRoot = resolve(__dirname, '../../../web');
// Silence is real decodable audio; lyric timing uses the native media clock.
function audio() {
  const wav = Buffer.alloc(44 + 8000 * 20 * 2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28);
  wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(wav.length - 44, 40);
  return wav;
}
async function load(page: Page, id: number, name = 'song.wav', type = 'audio', url?: string) {
  await page.evaluate(async ({ id, name, type, url }) => {
    // @ts-expect-error Native browser module.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ id, type, file_name: name }, url || `/api/media/${id}/stream`, '');
  }, { id, name, type, url });
}
test.use({ serviceWorkers: 'block' });
test.beforeEach(async ({ page }) => {
  await page.route('**/*', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/stream')) {
      const wav = audio();
      const range = /bytes=(\d+)-(\d*)/.exec(route.request().headers().range || '');
      if (range) {
        const start = Number(range[1]), end = range[2] ? Number(range[2]) : wav.length - 1;
        return route.fulfill({ status: 206, contentType: 'audio/wav', headers: { 'accept-ranges': 'bytes', 'content-range': `bytes ${start}-${end}/${wav.length}` }, body: wav.subarray(start, end + 1) });
      }
      return route.fulfill({ contentType: 'audio/wav', headers: { 'accept-ranges': 'bytes' }, body: wav });
    }
    const file = resolve(webRoot, `.${path === '/' ? '/index.html' : path}`);
    try {
      if (!file.startsWith(webRoot + '/')) throw Error('outside');
      const types: Record<string, string> = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };
      await route.fulfill({ contentType: types[extname(file)] || 'application/octet-stream', body: await readFile(file) });
    } catch { await route.fulfill({ status: 404 }); }
  });
  await page.goto('/detach.html');
});

test('synced lyrics follow native playback, forward/backward seeks, toggle and visuals Off', async ({ page }) => {
  await page.route('**/lyrics', route => route.fulfill({ json: { synced: true, lines: [{ start: 0, text: '<img src=x onerror=alert(1)>' }, { start: 4, text: 'Second' }, { start: 8, text: 'Third' }] } }));
  await load(page, 1);
  const panel = page.getByRole('region', { name: 'Lyrics' });
  await expect(panel).toBeVisible();
  await expect(panel.locator('img')).toHaveCount(0);
  await expect(panel.locator('[aria-current]')).toHaveText('<img src=x onerror=alert(1)>');
  await page.getByRole('combobox', { name: 'Audio visualization' }).selectOption('off');
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => { a.currentTime = 3.8; });
  await expect(panel.locator('[aria-current]')).toHaveText('Second');
  for (const [seconds, line] of [[9, 'Third'], [5, 'Second'], [0, '<img src=x onerror=alert(1)>']] as const) {
    await page.locator('#media-audio').evaluate((a: HTMLAudioElement, time) => { a.pause(); a.currentTime = time; }, seconds);
    await expect(panel.locator('[aria-current]')).toHaveText(line);
  }
  const toggle = page.getByRole('button', { name: 'Lyrics', exact: true });
  await toggle.focus(); await page.keyboard.press('Enter');
  await expect(panel).toBeHidden();
  await expect(toggle).toHaveAttribute('aria-pressed', 'false');
  await toggle.click(); await expect(panel).toBeVisible();
  await load(page, 2, 'book.m4b');
  await expect(panel).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Audio visualization', exact: true })).toBeDisabled();
});

test('plain lyrics scroll safely; absent, failed, invalid and non-audio tracks clear all lyrics', async ({ page }) => {
  const responses: Record<string, unknown> = {
    '1': { synced: false, lines: Array.from({ length: 100 }, (_, i) => ({ text: `Plain ${i}` })) },
    '2': { lines: [] }, '3': { lines: 'bad' },
  };
  await page.route('**/lyrics', route => {
    const id = new URL(route.request().url()).pathname.split('/')[3];
    if (id === '4') return route.fulfill({ status: 500 });
    if (id === '5') return route.fulfill({ contentType: 'application/json', body: 'broken' });
    return route.fulfill({ json: responses[id] });
  });
  await load(page, 1);
  const panel = page.locator('#audio-lyrics');
  await expect(panel).toContainText('Plain 99');
  await expect(panel.locator('[aria-current]')).toHaveCount(0);
  expect(await panel.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
  for (const id of [2, 3, 4, 5]) {
    await Promise.all([page.waitForResponse(response => response.url().endsWith(`/api/media/${id}/lyrics`)), load(page, id)]);
    await expect(panel).toBeEmpty();
    await expect(page.getByRole('button', { name: 'Lyrics', exact: true })).toBeHidden();
  }
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  await load(page, 1); await expect(panel).toBeVisible();
  await load(page, 6, 'video.mp4', 'video'); await expect(panel).toBeHidden();
  await expect(panel).toBeEmpty();
});

test('track changes abort late lyrics; close clears the active panel', async ({ page }) => {
  let release!: () => void;
  const waiting = new Promise<void>(resolve => { release = resolve; });
  let requested = false;
  await page.route('**/api/media/1/lyrics', async route => {
    requested = true; await waiting;
    await route.fulfill({ json: { lines: [{ text: 'Stale' }] } }).catch(() => {});
  });
  await page.route('**/api/media/2/lyrics', route => route.fulfill({ json: { lines: [{ text: 'Current' }] } }));
  await load(page, 1); await expect.poll(() => requested).toBe(true);
  await load(page, 2); await expect(page.locator('#audio-lyrics')).toHaveText('Current');
  release(); await page.waitForTimeout(100);
  await expect(page.locator('#audio-lyrics')).toHaveText('Current');
  await page.evaluate(async () => {
    // @ts-expect-error Native browser module.
    (await import('/js/playback.js')).stopAndClose();
  });
  await expect(page.locator('#audio-lyrics')).toBeEmpty();
});

test('share requests lyrics only after viewing authorization and preserves explicit view credential', async ({ page }) => {
  const calls: string[] = [];
  const metadata = { media: { id: 7, type: 'audio', file_name: 'shared.wav' }, stream_url: '/s/lyric/stream?view=credential', thumb_url: '' };
  const html = (await readFile(resolve(webRoot, 'share.html'), 'utf8')).replace('<!--SHARE_MEDIA-->', JSON.stringify(metadata));
  await page.route('**/s/lyric', route => route.fulfill({ contentType: 'text/html', body: html }));
  await page.route('**/s/lyric/view', route => { calls.push('view'); return route.fulfill({ status: 204 }); });
  await page.route('**/s/lyric/lyrics?view=credential', route => { calls.push('lyrics'); return route.fulfill({ json: { lines: [{ text: 'Shared lyrics' }] } }); });
  await page.goto('/s/lyric');
  await expect(page.locator('#audio-lyrics')).toHaveText('Shared lyrics');
  expect(calls).toEqual(['view', 'lyrics']);
  await page.route('**/s/lyric/view', route => { calls.push('failed-view'); return route.fulfill({ status: 403 }); });
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-share-viewing', 'failed');
  expect(calls).toEqual(['view', 'lyrics', 'failed-view']);
});


test('network errors and unrelated or foreign streams never expose stale lyrics or block audio', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/media/1/lyrics', route => route.abort('failed'));
  const failed = page.waitForEvent('requestfailed', request => request.url().endsWith('/lyrics'));
  await load(page, 1); await failed;
  await expect(page.locator('.lyrics-toggle')).toBeHidden();
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  const requests: string[] = [];
  page.on('request', request => { if (request.url().includes('/lyrics')) requests.push(request.url()); });
  await load(page, 1, 'song.wav', 'audio', '/unrelated/stream');
  await load(page, 1, 'song.wav', 'audio', 'https://foreign.test/api/media/1/stream');
  await page.waitForTimeout(100);
  expect(requests).toEqual([]);
  await expect(page.locator('#audio-lyrics')).toBeEmpty();
  expect(errors).toEqual([]);
});

test('focused lyrics own reading keys in the library while playback shortcuts stay usable', async ({ page }) => {
  const media = Array.from({ length: 12 }, (_, i) => ({ id: i + 1, set_id: 1, type: 'audio', file_name: `Song ${i + 1}.wav`, duration: 20 }));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/stream')) return route.fallback();
    let body: unknown = [];
    if (path === '/api/config') body = { media_page_size: 100 };
    if (path === '/api/sets') body = [{ id: 1, name: 'Music' }];
    if (path.endsWith('/browse')) body = { current_path: '', folders: [], media };
    if (path === '/api/media/1') body = { media: media[0], progress: null };
    if (path.endsWith('/lyrics')) body = { synced: false, lines: Array.from({ length: 100 }, (_, i) => ({ text: `Line ${i}` })) };
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Set Music', exact: true }).click();
  await expect(page.locator('.media-card')).toHaveCount(12);
  await page.locator('.media-card').first().getByRole('button', { name: 'Play', exact: true }).click();
  const audio = page.locator('#media-audio');
  await expect.poll(() => audio.evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  await audio.evaluate((a: HTMLAudioElement) => { a.pause(); a.currentTime = 4; });
  const panel = page.getByRole('region', { name: 'Lyrics' });
  await expect(panel).toBeVisible();
  await panel.focus();
  await page.keyboard.press('f');
  await expect(page.locator('#player')).toHaveClass(/is-fullscreen/);
  for (const key of ['ArrowDown', 'j', 'PageDown', 'End']) {
    const before = await panel.evaluate(el => el.scrollTop);
    await page.keyboard.press(key);
    await expect.poll(() => panel.evaluate(el => el.scrollTop)).toBeGreaterThan(before);
    if (key === 'End') await expect.poll(() => panel.evaluate(el => el.scrollTop)).toBe(await panel.evaluate(el => el.scrollHeight - el.clientHeight));
    await expect(page.locator('.media-card.selected')).toHaveAttribute('data-id', '1');
    expect(await audio.evaluate((a: HTMLAudioElement) => a.currentTime)).toBeCloseTo(4, 1);
  }
  for (const key of ['ArrowUp', 'k', 'PageUp', 'Home']) {
    const before = await panel.evaluate(el => el.scrollTop);
    await page.keyboard.press(key);
    await expect.poll(() => panel.evaluate(el => el.scrollTop)).toBeLessThan(before);
    await expect(page.locator('.media-card.selected')).toHaveAttribute('data-id', '1');
    expect(await audio.evaluate((a: HTMLAudioElement) => a.currentTime)).toBeCloseTo(4, 1);
  }
  await page.keyboard.press('Space');
  await expect.poll(() => audio.evaluate((a: HTMLAudioElement) => !a.paused && a.currentTime > 4)).toBe(true);
  await page.keyboard.press('p');
  await expect.poll(() => audio.evaluate((a: HTMLAudioElement) => a.paused)).toBe(true);
  await page.keyboard.press('ArrowRight');
  await expect.poll(() => audio.evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(5);
  await page.keyboard.press('Escape');
  await expect(page.locator('#player')).not.toHaveClass(/is-fullscreen/);
  await expect.poll(() => page.evaluate(() => document.fullscreenElement === null)).toBe(true);
  await page.keyboard.press('Escape');
  await expect(page.locator('#player')).toHaveClass(/minimized/);
});
