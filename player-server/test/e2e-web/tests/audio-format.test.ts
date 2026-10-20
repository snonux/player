import { test, expect, Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';
const webRoot = resolve(__dirname, '../../../web');
// Silence is real decodable audio, so the player opens as it does for music.
function audio() {
  const wav = Buffer.alloc(44 + 8000 * 4 * 2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28);
  wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(wav.length - 44, 40);
  return wav;
}
async function load(page: Page, id: number, type = 'audio', url?: string) {
  await page.evaluate(async ({ id, type, url }) => {
    // @ts-expect-error Native browser module.
    const playback = await import('/js/playback.js');
    playback.loadMediaDirect({ id, type, file_name: `item-${id}` }, url || `/api/media/${id}/stream`, '');
  }, { id, type, url });
}
test.use({ serviceWorkers: 'block' });
test.beforeEach(async ({ page }) => {
  await page.route('**/*', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/stream') || path.endsWith('/compat')) {
      return route.fulfill({ contentType: 'audio/wav', headers: { 'accept-ranges': 'bytes' }, body: audio() });
    }
    if (path.endsWith('/lyrics')) return route.fulfill({ json: { lines: [] } });
    const file = resolve(webRoot, `.${path === '/' ? '/index.html' : path}`);
    try {
      if (!file.startsWith(webRoot + '/')) throw Error('outside');
      const types: Record<string, string> = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };
      await route.fulfill({ contentType: types[extname(file)] || 'application/octet-stream', body: await readFile(file) });
    } catch { await route.fulfill({ status: 404 }); }
  });
  await page.goto('/detach.html');
});

test('audio format line shows codec and quality and clears between tracks', async ({ page }) => {
  const requests: string[] = [];
  await page.route('**/format*', route => {
    const url = new URL(route.request().url());
    requests.push(url.pathname + url.search);
    const id = url.pathname.split('/')[3];
    if (id === '1') return route.fulfill({ json: { codec: 'flac', codec_label: 'FLAC', bit_depth: 24, sample_rate: 96000, bitrate: 2304000, channels: 2, lossless: true, transcoded: false } });
    if (id === '2') return route.fulfill({ status: 500 });
    if (id === '3') return route.fulfill({ json: { codec_label: 'WMA', sample_rate: 44100, bitrate: 192000, channels: 2, transcoded: true } });
    return route.fulfill({ json: { codec_label: 'Opus', sample_rate: 48000, channels: 2 } });
  });
  const line = page.getByRole('note', { name: 'Audio format' });
  await load(page, 1);
  await expect(line).toBeVisible();
  await expect(line).toHaveText('FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo');
  await page.locator('#btn-play').click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(0);
  await expect(line).toBeVisible();

  await load(page, 2);
  await expect(line).toBeHidden();
  await load(page, 3, 'audio', '/api/media/3/compat');
  await expect(line).toHaveText('WMA · 44.1 kHz · 192 kbps · Stereo (plays as AAC)');
  await load(page, 4, 'video');
  await expect(line).toBeHidden();
  await load(page, 5, 'audio', '/s/tok/stream?view=cred');
  await expect(line).toHaveText('Opus · 48 kHz · Stereo');
  expect(requests).toEqual(['/api/media/1/format', '/api/media/2/format', '/api/media/3/format', '/s/tok/format?view=cred']);
});
