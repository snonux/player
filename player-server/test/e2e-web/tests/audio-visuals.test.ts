import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';

const webRoot = resolve(__dirname, '../../../web');

// Synthetic PCM gives the real browser decoder/analyser a steady audible signal;
// no media library, administrator account or server mutations are needed.
function tone() {
  const samples = 44100 * 30;
  const wav = Buffer.alloc(44 + samples * 2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(44100, 24); wav.writeUInt32LE(88200, 28);
  wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36);
  wav.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; i++) wav.writeInt16LE(Math.round(Math.sin(i * 2 * Math.PI * 440 / 44100) * 16000), 44 + i * 2);
  return wav;
}
test.use({ serviceWorkers: 'block' });

test.beforeEach(async ({ page }) => {
  // Serve production web assets in the browser without a running backend.
  await page.route('**/*', async route => {
    const pathname = new URL(route.request().url()).pathname;
    const file = resolve(webRoot, `.${pathname === '/' ? '/index.html' : pathname}`);
    const types: Record<string, string> = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' };
    try {
      if (!file.startsWith(webRoot + '/')) throw new Error('outside web root');
      await route.fulfill({ contentType: types[extname(file)] || 'application/octet-stream', body: await readFile(file) });
    } catch { await route.fulfill({ status: 404, body: 'not found' }); }
  });
  await page.route('**/visual-test.wav', route => route.fulfill({ contentType: 'audio/wav', body: tone() }));
  await page.goto('/detach.html');
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ type: 'audio', file_name: 'song.wav' }, '/visual-test.wav', '/logo.svg');
  });
});

async function pixels(page: import('@playwright/test').Page) {
  return page.locator('.audio-visual').evaluate((canvas: HTMLCanvasElement) => {
    const data = canvas.getContext('2d')!.getImageData(0, 0, canvas.width, canvas.height).data;
    const first = Array.from(data.slice(0, 3)).join(',');
    let colored = 0;
    for (let i = 0; i < data.length; i += 4) if (Array.from(data.slice(i, i + 3)).join(',') !== first) colored++;
    return colored;
  });
}

test('all five modes draw real audio; keyboard cycles through Off without interrupting sound', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  const select = page.getByRole('combobox', { name: 'Audio visualization' });
  await expect(select).toBeVisible();
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => !a.paused && a.currentTime > 0)).toBe(true);
  const snapshots = new Set<string>();
  for (const mode of ['bars', 'wave', 'radial', 'mirror', 'orbit']) {
    await select.selectOption(mode);
    await expect.poll(() => pixels(page)).toBeGreaterThan(5);
    snapshots.add(await page.locator('.audio-visual').evaluate((c: HTMLCanvasElement) => c.toDataURL()));
  }
  expect(snapshots.size).toBe(5);
  await page.locator('#btn-play').focus();
  await page.keyboard.press('v');
  await expect(select).toHaveValue('off');
  await expect(page.locator('.audio-visual')).toBeHidden();
  await expect(page.locator('#cover-art')).toBeVisible();
  const at = await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime);
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(at + 0.1);
  await page.keyboard.press('v');
  await expect(select).toHaveValue('bars');
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  await page.locator('#btn-play').click();
  const frozen = await page.locator('.audio-visual').evaluate((c: HTMLCanvasElement) => c.toDataURL());
  await page.waitForTimeout(150);
  expect(await page.locator('.audio-visual').evaluate((c: HTMLCanvasElement) => c.toDataURL())).toBe(frozen);
  await page.reload();
  expect(await page.evaluate(() => localStorage.getItem('player-audio-visual'))).toBe('bars');
  expect(errors).toEqual([]);
});

test('audiobooks and video hide visuals; routing remains playable across tracks and off', async ({ page }) => {
  await page.locator('#btn-play').click();
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  for (const file_name of ['book.m4b', 'song.wav', 'Audiobook chapter.wav']) {
    await page.evaluate(async (name) => {
      // @ts-expect-error Browser native module URL.
      const playback = await import('/js/playback.js');
      playback.loadMediaDirect({ type: 'audio', file_name: name }, '/visual-test.wav', '/logo.svg');
    }, file_name);
    const book = file_name !== 'song.wav';
    if (book) await expect(page.getByRole('combobox')).toBeDisabled();
    else await expect(page.getByRole('combobox')).toBeEnabled();
    if (book) await expect(page.locator('.audio-visual')).toBeHidden();
    await page.locator('#btn-play').click();
    await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
    if (!book) await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  }
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ type: 'video', file_name: 'video.mp4' }, '/visual-test.wav', '');
  });
  await expect(page.getByRole('combobox')).toBeHidden();
  await expect(page.locator('.audio-visual')).toBeHidden();
});

test('unsupported Web Audio falls back to artwork and native audio', async ({ page }) => {
  await page.addInitScript(() => { Object.defineProperty(window, 'AudioContext', { value: undefined }); Object.defineProperty(window, 'webkitAudioContext', { value: undefined }); });
  await page.reload();
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ type: 'audio', file_name: 'song.wav' }, '/visual-test.wav', '/logo.svg');
  });
  await expect(page.getByRole('combobox')).toBeDisabled();
  await expect(page.locator('.audio-visual')).toBeHidden();
  await expect(page.locator('#cover-art')).toBeVisible();
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
});

test('reduced motion starts Off; v respects typing and modal guards; minimized drawing stops', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.reload();
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ type: 'audio', file_name: 'song.wav' }, '/visual-test.wav', '/logo.svg');
  });
  const select = page.getByRole('combobox');
  await expect(select).toHaveValue('off');
  await page.locator('#btn-play').click();
  await page.keyboard.press('v');
  await expect(select).toHaveValue('bars');
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  await page.evaluate(() => {
    const input = document.createElement('input');
    input.id = 'typing'; document.body.appendChild(input); input.focus();
  });
  await page.keyboard.press('v');
  await expect(select).toHaveValue('bars');
  await page.evaluate(() => {
    document.getElementById('typing')!.remove();
    const modal = document.createElement('div');
    modal.id = 'test-modal'; modal.className = 'modal-overlay open'; document.body.appendChild(modal);
  });
  await page.keyboard.press('v');
  await expect(select).toHaveValue('bars');
  await page.evaluate(() => document.getElementById('test-modal')!.remove());
  await page.locator('#btn-fullscreen').click();
  await expect(page.locator('#player')).toHaveClass(/is-fullscreen/);
  await expect(select).toBeVisible();
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  await page.locator('#btn-fullscreen').click();
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.toggleMinimize();
  });
  await expect(page.locator('.audio-visual')).toBeHidden();
  const frozen = await page.locator('.audio-visual').evaluate((c: HTMLCanvasElement) => c.toDataURL());
  await page.waitForTimeout(150);
  expect(await page.locator('.audio-visual').evaluate((c: HTMLCanvasElement) => c.toDataURL())).toBe(frozen);
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.toggleMinimize();
  });
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
});

test('a suspended context never captures and silences native audio', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'AudioContext', { value: class {
      state = 'suspended';
      close() { return Promise.resolve(); }
      createMediaElementSource() { throw new Error('Suspended context must never capture audio'); }
    } });
  });
  await page.reload();
  await page.evaluate(async () => {
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ type: 'audio', file_name: 'song.wav' }, '/visual-test.wav', '/logo.svg');
  });
  await page.locator('#btn-play').click();
  await expect(page.getByRole('combobox')).toBeDisabled();
  await expect(page.locator('#cover-art')).toBeVisible();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
});

test('share viewing opens before audio loads and its player supports visuals and Off', async ({ page }) => {
  const requests: string[] = [];
  const metadata = { media: { type: 'audio', file_name: 'shared.wav' }, stream_url: '/share-visual-test.wav', thumb_url: '/logo.svg' };
  const html = (await readFile(resolve(webRoot, 'share.html'), 'utf8')).replace('<!--SHARE_MEDIA-->', JSON.stringify(metadata));
  await page.route('**/s/visual-test', route => route.fulfill({ contentType: 'text/html', body: html }));
  await page.route('**/s/visual-test/view', route => { requests.push('view'); return route.fulfill({ status: 204 }); });
  await page.route('**/share-visual-test.wav', route => { requests.push('audio'); return route.fulfill({ contentType: 'audio/wav', body: tone() }); });
  await page.goto('/s/visual-test');
  await expect(page.locator('body')).toHaveAttribute('data-share-viewing', 'open');
  await page.locator('#btn-play').click();
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  expect(requests[0]).toBe('view');
  expect(requests).toContain('audio');
  await page.getByRole('combobox').selectOption('off');
  await expect(page.locator('#cover-art')).toBeVisible();
});

test('library async playback preserves visuals, help modal guard and naming-based book exclusion', async ({ page }) => {
  const media = { id: 1, set_id: 1, type: 'audio', file_name: 'song.wav', rel_path: 'song.wav', duration: 30, thumbnail_path: 'cover.jpg' };
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/stream')) return route.fulfill({ contentType: 'audio/wav', body: tone() });
    if (path.endsWith('/thumbnail')) return route.fulfill({ contentType: 'image/svg+xml', body: await readFile(resolve(webRoot, 'logo.svg')) });
    let body: unknown = [];
    if (path === '/api/config') body = { media_page_size: 100 };
    if (path === '/api/sets') body = [{ id: 1, name: 'Music' }];
    if (path.endsWith('/browse')) body = { current_path: '', folders: [], media: [media] };
    if (path === '/api/media/1') body = { media, progress: null };
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Set Music', exact: true }).click();
  await expect(page.locator('.media-card')).toHaveCount(1);
  await page.locator('.media-card').getByRole('button', { name: '▶', exact: true }).click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  // A slow detail fetch may outlive transient activation; this starts it then.
  await page.locator('#btn-play').focus();
  await page.keyboard.press('v');
  await expect.poll(() => pixels(page)).toBeGreaterThan(5);
  const value = await page.getByRole('combobox', { name: 'Audio visualization' }).inputValue();
  await page.keyboard.press('?');
  await expect(page.locator('#help-modal')).toHaveClass(/open/);
  await page.keyboard.press('v');
  expect(await page.getByRole('combobox', { name: 'Audio visualization' }).inputValue()).toBe(value);
  await page.keyboard.press('Escape');
  await page.evaluate(async (item) => {
    // @ts-expect-error Browser native module URL.
    const { state } = await import('/js/state.js');
    state.sets = [{ id: 1, name: 'Audiobooks' }];
    // @ts-expect-error Browser native module URL.
    const playback = await import('/js/playback.js');
    playback.selectAndPlay(item, 0);
  }, media);
  await expect(page.getByRole('combobox', { name: 'Audio visualization' })).toBeDisabled();
  await expect(page.locator('.audio-visual')).toBeHidden();
});
