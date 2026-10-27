/** M51: real encoded ZIPs through the browser dialog and offline playback. */
import { test, expect, request, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
test.use({ serviceWorkers: 'block' });
const f: MusicFixture = loadFixture();
const root = process.env.PLAYER_M51_EVIDENCE || join(tmpdir(), 'player-m51-evidence');
const dialog = (page: Page) => page.getByRole('dialog', { name: 'Download music', exact: true });

async function signIn(page: Page) {
  await page.goto(f.origin + '/login.html');
  await page.locator('#username').fill(f.user.username);
  await page.locator('#password').fill(f.user.password);
  await page.locator('#password').press('Enter');
  await page.waitForURL(url => url.pathname === '/', { waitUntil: 'domcontentloaded' });
}

function inspectArchive(path: string) {
  const script = `import json,sys,zipfile,subprocess,tempfile,pathlib,base64
with zipfile.ZipFile(sys.argv[1]) as z, tempfile.TemporaryDirectory() as d:
 rows=[]
 for name in z.namelist():
  target=pathlib.Path(d)/pathlib.Path(name).name
  data=z.read(name); target.write_bytes(data)
  probe=json.loads(subprocess.check_output(['ffprobe','-v','error','-show_entries','stream=codec_name,bit_rate:format=format_name,bit_rate','-of','json',str(target)]))
  rows.append({'name':name,'bytes':len(data),'codec':probe['streams'][0]['codec_name'],'stream_bitrate':probe['streams'][0].get('bit_rate'),'format_bitrate':probe['format'].get('bit_rate'),'format':probe['format']['format_name'],'audio_b64':base64.b64encode(data).decode() if len(rows)==0 else None})
 print(json.dumps(rows))`;
  return JSON.parse(execFileSync('python3', ['-c', script, path], { maxBuffer: 16 * 1024 * 1024 }).toString()) as
    { name: string; bytes: number; codec: string; stream_bitrate?: string; format_bitrate?: string; format: string; audio_b64?: string }[];
}

test('M51 download quality is independent, encoded truthfully and playable offline', async ({ page, context }, info) => {
  test.setTimeout(180_000);
  const e = new Evidence(root, info.project.name, { feature: 'M51', browser_version: page.context().browser()?.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
  await signIn(page);
  const albums = await (await page.request.get(f.origin + '/api/v1/music/albums')).json();
  const album = albums.find((a: any) => a.title === 'Night Transit');
  expect(album).toBeTruthy();
  await page.goto(f.origin + '/?music=album&id=' + encodeURIComponent(album.album_key));
  const opener = page.locator('#media-grid').getByRole('button', { name: 'Download…', exact: true });
  const streamBefore = await page.evaluate(() => localStorage.getItem('player-stream-quality'));
  const results: any[] = [];
  await page.evaluate(() => {
    const original = window.fetch.bind(window);
    window.fetch = async (...args) => {
      const response = await original(...args);
      if (String(args[0]).includes('/api/music/downloads') && response.ok) {
        (window as any).__m51Profile = JSON.parse(String(args[1]?.body)).profile;
        (window as any).__m51Archive = response.clone().arrayBuffer().then(buffer => {
          const bytes = new Uint8Array(buffer);
          let binary = '';
          for (let i = 0; i < bytes.length; i += 16384)
            binary += String.fromCharCode(...bytes.subarray(i, i + 16384));
          return btoa(binary);
        });
      }
      return response;
    };
  });
  for (const [profile, codec, ext] of [['aac128', 'aac', '.m4a'], ['opus96', 'opus', '.ogg']] as const) {
    await page.evaluate(() => { (window as any).__m51Archive = null; });
    await opener.focus();
    await page.keyboard.press('Enter');
    await expect(dialog(page)).toBeVisible();
    const select = dialog(page).getByRole('combobox', { name: 'Download quality' });
    await expect.poll(() => select.locator(`option[value="${profile}"]`).count()).toBe(1);
    await select.selectOption(profile);
    await expect(select).toHaveValue(profile);
    const responsePromise = page.waitForResponse(r => r.url().includes('/api/music/downloads') && r.request().method() === 'POST', { timeout: 120_000 });
    await dialog(page).getByRole('button', { name: 'Download ZIP' }).click();
    const response = await responsePromise;
    expect(response.status()).toBe(200);
    expect(await page.evaluate(() => (window as any).__m51Profile)).toBe(profile);
    await expect(dialog(page).locator('[data-status]')).toContainText('Archive ready:');
    const path = e.path(`${profile}.zip`);
    const archive = await page.evaluate(() => (window as any).__m51Archive);
    expect(archive).toBeTruthy();
    writeFileSync(path, Buffer.from(archive, 'base64'));
    const rows = inspectArchive(path);
    expect(rows).toHaveLength(3);
    for (const row of rows) {
      expect(row.name.endsWith(ext)).toBe(true);
      expect(row.codec).toBe(codec);
      expect(row.bytes).toBeGreaterThan(1000);
      const bitrate = Number(row.stream_bitrate || row.format_bitrate);
      expect(bitrate).toBeGreaterThan(20000);
      expect(bitrate).toBeLessThan(profile === 'aac128' ? 180000 : 135000);
    }
    const local = await context.newPage();
    await local.addInitScript(installAudioTap);
    await local.goto(f.origin + '/api/v1/config');
    await local.setContent('<audio></audio><button>Play saved song</button>');
    await context.setOffline(true);
    try {
      await local.evaluate(({ data, mime }) => {
        const audio = document.querySelector('audio')!;
        audio.src = `data:${mime};base64,${data}`;
        document.querySelector('button')!.onclick = async () => {
          const w = window as any;
          w.ctx ||= new AudioContext();
          if (!w.source) { w.source = w.ctx.createMediaElementSource(audio); w.source.connect(w.ctx.destination); }
          try { await w.ctx.resume(); await audio.play(); }
          catch (error) { w.playError = String(error); }
        };
      }, { data: rows[0].audio_b64, mime: codec === 'aac' ? 'audio/mp4' : 'audio/ogg' });
      await local.getByRole('button', { name: 'Play saved song' }).click();
      await expect.poll(() => local.locator('audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(.5);
      const output = await captureOutput(local, 1200, e.path(`${profile}-offline.wav`));
      assertValidCapture(output);
      expect(mainSegment(output.analysis)?.duration_s).toBeGreaterThan(.5);
      results.push({ profile, rows: rows.map(({ audio_b64, ...row }) => row), offline_audio: mainSegment(output.analysis) });
    } finally {
      await context.setOffline(false);
      await local.close();
    }
    await page.keyboard.press('Escape');
    await expect(dialog(page)).toHaveCount(0);
    await expect(opener).toBeFocused();
  }
  expect(await page.evaluate(() => localStorage.getItem('player-stream-quality'))).toBe(streamBefore);
  await opener.focus(); await page.keyboard.press('Enter');
  await expect(dialog(page).getByRole('combobox', { name: 'Download quality' })).toHaveValue('opus96');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: e.path('dialog-narrow-dark.png') });
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
  await page.screenshot({ path: e.path('dialog-narrow-light.png') });
  expect(await dialog(page).evaluate(n => n.getBoundingClientRect().width <= innerWidth)).toBe(true);
  await page.keyboard.press('Escape');
  e.record({ feature: 'M51', scenario: 'AAC/Opus archive encoding and offline playback', result: 'pass', data: results });
});

test('M51 selected conversion reports access denial and retries after restoration', async ({ page }) => {
  await signIn(page);
  const albums = await (await page.request.get(f.origin + '/api/v1/music/albums')).json();
  const album = albums.find((a: any) => a.title === 'Night Transit');
  await page.goto(f.origin + '/?music=album&id=' + encodeURIComponent(album.album_key));
  const opener = page.locator('#media-grid').getByRole('button', { name: 'Download…', exact: true });
  await opener.click();
  const select = dialog(page).getByRole('combobox', { name: 'Download quality' });
  await expect.poll(() => select.locator('option[value="aac128"]').count()).toBe(1);
  await select.selectOption('aac128');
  const admin = await request.newContext({ baseURL: f.origin });
  await admin.post('/api/v1/auth/login', { data: f.admin });
  const users = await (await admin.get('/api/v1/admin/users')).json();
  const user = users.find((u: any) => u.username === f.user.username);
  const grant = { set_id: f.setIds.Music, user_id: user.id, role: 'viewer' };
  try {
    expect((await admin.delete('/api/v1/admin/permissions', { data: grant })).ok()).toBe(true);
    const denied = page.waitForResponse(r => r.url().includes('/api/music/downloads') && r.request().method() === 'POST');
    await dialog(page).getByRole('button', { name: 'Download ZIP' }).click();
    expect((await denied).status()).toBe(403);
    await expect(dialog(page).getByRole('button', { name: 'Retry' })).toBeEnabled();
    await expect(dialog(page).locator('[data-status]')).not.toContainText('Archive ready');
  } finally {
    expect((await admin.post('/api/v1/admin/permissions', { data: grant })).ok()).toBe(true);
    await admin.dispose();
  }
  await dialog(page).getByRole('button', { name: 'Retry' }).click();
  await expect(dialog(page).locator('[data-status]')).toContainText('Archive ready:');
  await page.keyboard.press('Escape');
  await expect(opener).toBeFocused();
});
