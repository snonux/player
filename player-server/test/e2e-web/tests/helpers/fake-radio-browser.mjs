// fake-radio-browser.mjs — a stand-in for the Radio Browser API plus the
// station streams it lists, for the web radio e2e test
// (playwright.radio.config.ts). It answers the directory routes the server
// uses and serves an MP3 file as an endless, real-time paced live stream.
//
//   node tests/helpers/fake-radio-browser.mjs [port]
import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const port = Number(process.argv[2] || process.env.FAKE_RADIO_PORT || 18190);
const origin = `http://127.0.0.1:${port}`;
// A tone encoded like a radio stream (128 kbit/s, no ID3 or Xing header).
// Browsers start such a stream as soon as the first burst arrives; a file
// with a Xing header makes Chromium wait for far more data. Without ffmpeg
// the library's MP3 fixture is used at its own 64 kbit/s.
function loadAudio() {
  const out = join(tmpdir(), `fake-radio-${process.pid}.mp3`);
  const run = spawnSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-f', 'lavfi', '-i', 'sine=frequency=440:duration=30',
    '-ac', '2', '-ar', '44100', '-b:a', '128k', '-write_xing', '0', '-id3v2_version', '0', out]);
  if (run.status === 0) return { data: readFileSync(out), bytesPerSecond: 16000 };
  return { data: readFileSync(fileURLToPath(new URL('../../../../testdata/media/audiobooks/aesops-fables/13-chapter.mp3', import.meta.url))), bytesPerSecond: 8000 };
}
const { data: mp3, bytesPerSecond } = loadAudio();
// 1x1 PNG favicon.
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64');

const station = (n, name, tag, country, code, extra = {}) => ({
  changeuuid: '', stationuuid: `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`,
  name, url: `${origin}/stream/${n}`, url_resolved: `${origin}/stream/${n}`,
  homepage: `https://example.org/${n}`, favicon: `${origin}/favicon.png`, tags: tag,
  country, countrycode: code, language: 'english', codec: 'MP3', bitrate: 64,
  votes: 100 - n, clickcount: 1000 - n, hls: 0, lastcheckok: 1, ...extra,
});

// Station 3 is "dead" (its stream answers 404), station 9 is HLS and must
// never reach the client.
export const STATIONS = [
  station(1, 'Fake Jazz One', 'jazz,smooth jazz', 'Germany', 'DE'),
  station(2, 'Fake Jazz Two', 'jazz', 'Germany', 'DE'),
  station(3, 'Fake Jazz Dead', 'jazz', 'France', 'FR'),
  station(4, 'Fake Rock Radio', 'rock', 'France', 'FR'),
  station(5, 'Fake News Talk', 'news', 'Austria', 'AT', { favicon: '' }),
  station(9, 'Fake HLS Jazz', 'jazz', 'Germany', 'DE', { hls: 1, url_resolved: `${origin}/hls.m3u8` }),
];

function json(res, value) {
  res.writeHead(200, { 'content-type': 'application/json' });
  res.end(JSON.stringify(value));
}

function search(params) {
  let list = STATIONS.slice();
  const name = params.get('name')?.toLowerCase();
  if (name) list = list.filter((s) => s.name.toLowerCase().includes(name));
  const tag = params.get('tag')?.toLowerCase();
  if (tag) list = list.filter((s) => s.tags.split(',').includes(tag));
  const code = params.get('countrycode');
  if (code) list = list.filter((s) => s.countrycode === code);
  const order = params.get('order');
  if (order === 'name') list.sort((a, b) => a.name.localeCompare(b.name));
  if (order === 'random') list.sort(() => Math.random() - 0.5);
  const offset = Number(params.get('offset') || 0);
  const limit = Number(params.get('limit') || 100);
  return list.slice(offset, offset + limit);
}

// stream sends the MP3 in a loop at its bitrate, like a live Icecast mount
// (which also sends a burst on connect), until the client goes away.
function stream(req, res) {
  res.writeHead(200, { 'content-type': 'audio/mpeg', 'icy-name': 'fake', 'cache-control': 'no-cache' });
  const chunk = bytesPerSecond; // one second of audio
  let pos = 0;
  const send = () => {
    if (res.destroyed) return;
    const part = mp3.subarray(pos, pos + chunk);
    pos += chunk;
    if (pos >= mp3.length) pos = 0;
    res.write(part);
  };
  // A few seconds up front so playback starts at once.
  for (let i = 0; i < 4; i++) send();
  const timer = setInterval(send, 1000);
  const end = () => clearInterval(timer);
  req.on('close', end);
  res.on('close', end);
}

const server = createServer((req, res) => {
  const url = new URL(req.url, origin);
  const p = url.pathname;
  if (p === '/json/stations/search') return json(res, search(url.searchParams));
  if (p.startsWith('/json/stations/byuuid/')) {
    const uuid = decodeURIComponent(p.slice('/json/stations/byuuid/'.length));
    return json(res, STATIONS.filter((s) => s.stationuuid === uuid));
  }
  if (p.startsWith('/json/url/')) return json(res, { ok: true });
  if (p === '/json/tags') {
    return json(res, [{ name: 'jazz', stationcount: 3 }, { name: 'rock', stationcount: 1 }, { name: 'news', stationcount: 1 }]);
  }
  if (p === '/json/countries') {
    return json(res, [
      { name: 'Germany', iso_3166_1: 'DE', stationcount: 2 },
      { name: 'France', iso_3166_1: 'FR', stationcount: 2 },
      { name: 'Austria', iso_3166_1: 'AT', stationcount: 1 },
    ]);
  }
  if (p === '/favicon.png') {
    res.writeHead(200, { 'content-type': 'image/png' });
    return res.end(png);
  }
  if (p === '/stream/3') {
    res.writeHead(404, { 'content-type': 'text/plain' });
    return res.end('gone');
  }
  if (p.startsWith('/stream/')) return stream(req, res);
  if (p === '/healthz') return json(res, { ok: true });
  res.writeHead(404);
  res.end();
});

server.listen(port, '127.0.0.1', () => console.log(`fake radio browser on ${origin}`));
