import { test as base, expect } from '@playwright/test';
import { spawn, execFileSync, ChildProcess } from 'node:child_process';
import { cpSync, mkdtempSync, mkdirSync, rmSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { call, json, login, baseURL } from './scenario-http';

export type Sandbox = { root: string; mediaRoot: string; db: string; sql: (query: string) => string };
const component = path.resolve(__dirname, '../../../..');

async function stop(child: ChildProcess): Promise<void> {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit');
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 5000);
  await exited;
  clearTimeout(timer);
}

export const test = base.extend<{ sandbox: Sandbox; bootstrapAdmin: boolean }>({
  // Scenarios that exercise first-run setup opt out with
  // test.use({ bootstrapAdmin: false }); all others start logged-in ready.
  bootstrapAdmin: [true, { option: true }],
  sandbox: [async ({ bootstrapAdmin }, use, info) => {
    const binary = process.env.PLAYER_SCENARIO_BINARY;
    if (!binary) throw new Error('Build the server and set PLAYER_SCENARIO_BINARY (see test/e2e-llm/README.md).');
    const url = new URL(baseURL);
    if (!['127.0.0.1', 'localhost'].includes(url.hostname) || url.protocol !== 'http:' || !url.port || url.pathname !== '/' || url.username || url.password) {
      throw new Error('PLAYER_URL must be an unused loopback HTTP origin with an explicit port. External servers are not supported.');
    }
    // A TCP bind catches even an unrelated or stalled service on the target port.
    const probe = createServer();
    await new Promise<void>((resolve, reject) => {
      probe.once('error', error => reject(new Error(`Scenario port unavailable (${baseURL}): ${error.message}`)));
      probe.listen(Number(url.port), () => probe.close(error => error ? reject(error) : resolve()));
    });
    const root = mkdtempSync(path.join(tmpdir(), 'player-scenario-'));
    const mediaRoot = path.join(root, 'media');
    const db = path.join(root, 'player.db');
    cpSync(path.join(component, 'testdata/media'), mediaRoot, { recursive: true });
    mkdirSync(path.join(mediaRoot, 'empty'));
    // Give audio fixtures real artwork, ensuring thumbnail and HEAD scenarios are reachable.
    cpSync(path.join(mediaRoot, 'images/earth-pia18033.jpg'), path.join(mediaRoot, 'audiobooks/aesops-fables/cover.jpg'));
    const child = spawn(binary, [], { cwd: component, env: {
      ...process.env, PORT: url.port || '18081', MEDIA_ROOT: mediaRoot, DB_PATH: db,
      SECURE_COOKIES: 'false', MAX_UPLOAD_SIZE_MB: '1', LOG_LEVEL: 'error',
    }, stdio: ['ignore', 'pipe', 'pipe'] });
    let logs = '';
    child.stdout!.on('data', data => { logs += data; });
    child.stderr!.on('data', data => { logs += data; });
    try {
      await once(child, 'spawn', { signal: AbortSignal.timeout(5000) });
      await expect.poll(async () => {
        if (child.exitCode !== null) throw new Error(`Server exited: ${logs}`);
        try { return (await fetch(`${baseURL}/healthz`, { signal: AbortSignal.timeout(1000) })).status; } catch { return 0; }
      }, { timeout: 15000 }).toBe(200);
      if (bootstrapAdmin) {
        await json(await call('POST', '/api/v1/auth/bootstrap', { body: { username: 'admin', password: 'TestPassw0rd!' } }));
        const session = await login();
        await scan(session.cookie);
      }
      await use({ root, mediaRoot, db, sql: query => execFileSync('sqlite3', ['-cmd', '.timeout 5000', db, query], { encoding: 'utf8' }).trim() });
    } finally {
      await stop(child);
      if (info.status !== info.expectedStatus) await info.attach('server.log', { body: logs, contentType: 'text/plain' });
      rmSync(root, { recursive: true, force: true });
    }
  }, { auto: true, timeout: 90000 }],
});
export { expect };

export async function scan(cookie: string): Promise<void> {
  // The startup scan can still be active when HTTP starts accepting requests.
  await expect.poll(async () => (await json<any>(await call('GET', '/api/v1/admin/scan-progress', { cookie }))).running, { timeout: 60000 }).toBe(false);
  await json(await call('POST', '/api/v1/admin/rescan', { cookie }));
  await expect.poll(async () => {
    const progress = await json<any>(await call('GET', '/api/v1/admin/scan-progress', { cookie }));
    expect(progress.last_error || '').toBe('');
    return progress.running;
  }, { timeout: 60000 }).toBe(false);
}

export async function admin() {
  const session = await login();
  const api = async (method: string, route: string, body?: unknown) => json<any>(await call(method, `/api/v1${route}`, { ...session, body }));
  const media = await api('GET', '/media?limit=1000');
  const sets = await api('GET', '/sets');
  expect(media.length).toBeGreaterThan(4);
  return { ...session, api, media, sets };
}
export async function createUser(cookie: string, username = 'scenario-user') {
  const user = await json<any>(await call('POST', '/api/v1/admin/users', { cookie, body: { username, password: 'TestPassw0rd!', is_admin: false } }));
  expect(user).toMatchObject({ username, is_admin: false });
  expect(user.id).toBeGreaterThan(0);
  return { ...user, ...(await login(username)) };
}
export async function upload(auth: { cookie?: string; bearer?: string }, set: number, name: string, data?: Buffer) {
  const bytes = data ?? readFileSync(path.join(component, 'testdata/media/audiobooks/aesops-fables/37-chapter.mp3'));
  const form = new FormData();
  form.append('file', new Blob([new Uint8Array(bytes)]), name);
  return call('POST', `/api/v1/sets/${set}/upload`, { ...auth, form });
}
export async function browserLogin(page: import('@playwright/test').Page, cookie: string) {
  await page.context().addCookies([{ name: 'session', value: cookie.slice('session='.length), url: baseURL }]);
}

export async function podcast(cookie: string, run: (feed: any) => Promise<void>) {
  const fixture = spawn(process.execPath, [path.join(component, 'test/e2e-llm/fixtures/mock-rss-server.js')], {
    env: { ...process.env, PORT: '0' }, stdio: ['ignore', 'pipe', 'pipe'],
  });
  let logs = '';
  fixture.stderr!.on('data', data => { logs += data; });
  try {
    await once(fixture, 'spawn', { signal: AbortSignal.timeout(5000) });
    const [ready] = await once(fixture.stdout!, 'data', { signal: AbortSignal.timeout(5000) });
    const url = String(ready).match(/http:\/\/127\.0\.0\.1:\d+/)?.[0];
    expect(url, 'RSS fixture must announce its ephemeral loopback URL').toBeTruthy();
    const feed = await json<any>(await call('POST', '/api/v1/podcasts', { cookie, body: { feed_url: `${url}/feed.xml` } }));
    expect(feed.id).toBeGreaterThan(0);
    await run(feed);
  } catch (error) {
    throw new Error(`Podcast fixture failed: ${String(error)} ${logs}`);
  } finally { await stop(fixture); }
}
