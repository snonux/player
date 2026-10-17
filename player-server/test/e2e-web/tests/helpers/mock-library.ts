import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';
import type { BrowserContext } from '@playwright/test';

export const album = 'Artist & Beyoncé/Album #1 日本語';
export const tracks = Array.from({ length: 5 }, (_, index) => ({
  id: index + 11, set_id: 1, type: 'audio', file_name: `Track ${index + 1}.wav`,
  rel_path: `${album}/Track ${index + 1}.wav`, duration: 60, thumbnail_path: 'cover.jpg',
}));
const webRoot = resolve(__dirname, '../../../../web');
function tone() {
  const samples = 44100 * 60, wav = Buffer.alloc(44 + samples * 2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(44100, 24); wav.writeUInt32LE(88200, 28);
  wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; i++) wav.writeInt16LE(Math.round(Math.sin(i * 2 * Math.PI * 440 / 44100) * 16000), 44 + i * 2);
  return wav;
}

export async function mockLibrary(context: BrowserContext, pageSize = 100) {
  const audio = tone();
  const writes: string[] = [];
  await context.route('**/*', async route => {
    const url = new URL(route.request().url()), path = url.pathname;
    if (path.startsWith('/api/')) {
      if (route.request().method() !== 'GET' && route.request().method() !== 'HEAD') writes.push(path);
      if (path.endsWith('/stream')) {
        const range = /^bytes=(\d+)-(\d*)$/.exec(route.request().headers().range || '');
        const start = range ? Number(range[1]) : 0, end = range?.[2] ? Math.min(Number(range[2]), audio.length - 1) : audio.length - 1;
        return route.fulfill({ status: range ? 206 : 200, contentType: 'audio/wav',
          headers: { 'Accept-Ranges': 'bytes', ...(range ? { 'Content-Range': `bytes ${start}-${end}/${audio.length}` } : {}) }, body: audio.subarray(start, end + 1) });
      }
      if (/\/(thumbnail|cover)$/.test(path)) return route.fulfill({ contentType: 'image/svg+xml', body: await readFile(resolve(webRoot, 'logo.svg')) });
      let body: unknown = [];
      if (path === '/api/config') body = { media_page_size: pageSize };
      if (path === '/api/sets') body = [{ id: 1, name: 'Music' }];
      if (path === '/api/media') body = tracks;
      if (path === '/api/sets/1/browse') {
        const parent = url.searchParams.get('parent') || '';
        body = parent === album ? { current_path: album, folders: [], media: tracks } :
          parent === 'Artist & Beyoncé' ? { current_path: parent, folders: [{ name: 'Album #1 日本語' }], media: [] } :
          { current_path: '', folders: [{ name: 'Artist & Beyoncé' }], media: [] };
      }
      const id = /^\/api\/media\/(\d+)$/.exec(path)?.[1];
      if (id) {
        const media = tracks.find(track => String(track.id) === id);
        if (!media) return route.fulfill({ status: 404, json: { error: 'Media not found' } });
        body = { media, progress: { position_seconds: 5 } };
      }
      if (path.endsWith('/notes')) body = { content: '' };
      if (path.endsWith('/lyrics')) body = { synced: false, lines: [{ text: 'Fixture lyrics' }] };
      return route.fulfill({ json: body });
    }
    const file = resolve(webRoot, `.${path === '/' ? '/index.html' : path}`);
    const types: Record<string, string> = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' };
    try {
      if (!file.startsWith(webRoot + '/')) throw new Error('outside web root');
      await route.fulfill({ contentType: types[extname(file)] || 'application/octet-stream', body: await readFile(file) });
    } catch { await route.fulfill({ status: 404, body: 'not found' }); }
  });
  return writes;
}
