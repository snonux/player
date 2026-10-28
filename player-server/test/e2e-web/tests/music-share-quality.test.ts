/** M65: selected public-share audio encodes in installed browsers. */
import { test, expect } from '@playwright/test';
import { writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { Evidence, loadFixture, track } from './helpers/music-fixture';
import { keyboardLogin, openFolder, selectByKeys, openCollectionMoreAction } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M65_ISOLATED, 'Requires fresh disposable M65 fixture');
test.use({ serviceWorkers: 'block' });

test('M65 original and encoded public share policies, playback and expiry', async ({ page, browser }, info) => {
  test.setTimeout(300_000);
  const f = loadFixture();
  const evidence = new Evidence(process.env.PLAYER_M65_EVIDENCE!, info.project.name,
    { feature: 'M65', browser_version: browser.version(), server_version: f.serverVersion });
  const song = track(f, 'Music/EQ Probe/Three Tones.flac');
  const profiles = [
    ['original', 'flac', '.flac'],
    ['aac128', 'aac', '.m4a'],
    ['aac256', 'aac', '.m4a'],
    ['mp3192', 'mp3', '.mp3'],
    ['opus96', 'opus', '.ogg'],
  ] as const;
  const results: unknown[] = [];
  await keyboardLogin(page, f.user);
  await openFolder(page, f.setIds.Music, 'EQ Probe');
  await selectByKeys(page, song.title);
  const tokens: string[] = [];
  try {
    for (const [policy, codec, extension] of profiles) {
      await page.keyboard.press('s');
      const dialog = page.getByRole('dialog', { name: 'Share music' });
      await expect(dialog).toBeVisible();
      await dialog.locator('summary').click();
      await expect(dialog.locator('[data-share-quality] [role=status]')).toContainText('ready');
      await dialog.getByLabel('Shared audio quality').selectOption(policy);
      await dialog.getByRole('button', { name: 'Create link' }).focus();
      await page.keyboard.press('Enter');
      await expect(dialog.locator('[data-status]')).toContainText('Link ready');
      const url = await dialog.getByLabel('Share link').inputValue();
      const token = url.split('/s/')[1];
      expect(token).toBeTruthy();
      tokens.push(token);
      const saved = await (await page.request.get('/api/shares')).json();
      expect(saved.find((share: any) => share.token === token)?.audio_quality).toBe(policy);
      await dialog.getByRole('button', { name: 'Close' }).click();

      const anonymous = await browser.newContext();
      try {
        const guest = await anonymous.newPage();
        expect((await guest.goto(`/s/${token}`))?.status()).toBe(200);
        await expect(guest.locator('#share-quality')).toContainText('Shared audio quality');
        await guest.locator('#big-play').click();
        await expect.poll(() => guest.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.currentTime),
          { timeout: 25_000 }).toBeGreaterThan(.5);
        const media = await anonymous.request.get(`/s/${token}`, { headers: { Accept: 'application/json' } });
        expect(media.ok()).toBe(true);
        const metadata = await media.json();
        expect(metadata.audio_quality).toBe(policy);
        const playable = metadata.playback_url as string;
        expect(playable).toContain(policy === 'original' ? '/stream' : '/compat');
        const response = await anonymous.request.get(playable);
        expect(response.status()).toBe(200);
        const bytes = await response.body();
        const path = evidence.path(`${policy}.bin`);
        writeFileSync(path, bytes);
        const probe = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', path], { encoding: 'utf8' }));
        expect(probe.streams[0].codec_name).toBe(codec);
        const download = await anonymous.request.get(metadata.download_url);
        expect(download.status()).toBe(200);
        expect(download.headers()['content-disposition']).toContain(extension);
        const forged = await anonymous.request.get(`/s/${token}/stream?profile=${policy === 'original' ? 'aac128' : 'original'}`);
        expect(forged.status()).toBe(200);
        writeFileSync(evidence.path(`${policy}-override.bin`), await forged.body());
        const overrideProbe = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-show_streams', '-of', 'json', evidence.path(`${policy}-override.bin`)], { encoding: 'utf8' }));
        expect(overrideProbe.streams[0].codec_name).toBe(codec);
        const head = await anonymous.request.head(playable);
        expect(head.status()).toBe(200);
        const range = await anonymous.request.get(playable, { headers: { Range: 'bytes=100-299' } });
        expect(range.status()).toBe(206);
        expect((await range.body()).equals(bytes.subarray(100, 300))).toBe(true);
        results.push({ policy, codec, extension, bitrate: probe.format.bit_rate, mime: response.headers()['content-type'], playback: playable });
      } finally {
        await anonymous.close();
      }
    }
    const revoked = tokens.at(-1)!;
    expect((await page.request.delete(`/api/shares/${revoked}`)).ok()).toBe(true);
    for (const suffix of ['/stream', '/compat', '/download'])
      expect((await page.request.get(`/s/${revoked}${suffix}`)).status()).toBe(404);
    const expiring = await page.request.post(`/api/media/${song.id}/shares`, { data: {
      audio_quality: 'mp3192', expires_at: new Date(Date.now() + 2300).toISOString(),
    } });
    expect(expiring.ok()).toBe(true);
    const expiringToken = (await expiring.json()).token;
    tokens.push(expiringToken);
    await page.waitForTimeout(2900);
    for (const suffix of ['/stream', '/compat', '/download'])
      expect((await page.request.get(`/s/${expiringToken}${suffix}`)).status()).toBe(410);
    writeFileSync(evidence.path('results.json'), JSON.stringify(results, null, 2));
    evidence.record({ feature: 'M65', scenario: 'all five UI policies, guest playback, real codecs, download formats, override, HEAD/Range, revoke and expiry',
      result: 'pass', artifacts: ['results.json', ...profiles.map(([policy]) => `${policy}.bin`)] });
  } finally {
    for (const token of tokens) await page.request.delete(`/api/shares/${token}`).catch(() => {});
  }
});

test('M65 My Shares edit keeps policy through offline error and changes actual rendition', async ({ page, context, browser }, info) => {
  test.setTimeout(120_000);
  const f = loadFixture();
  const evidence = new Evidence(process.env.PLAYER_M65_EVIDENCE!, info.project.name,
    { feature: 'M65 edit', browser_version: browser.version(), server_version: f.serverVersion });
  const song = track(f, 'Music/EQ Probe/Three Tones.flac');
  await keyboardLogin(page, f.user);
  const created = await page.request.post(`/api/media/${song.id}/shares`, { data: { audio_quality: 'mp3192' } });
  expect(created.ok()).toBe(true);
  const token = (await created.json()).token;
  try {
    await page.keyboard.press('L');
    const modal = page.locator('#shares-modal');
    await expect(modal).toHaveClass(/open/);
    const row = modal.locator('.share-row').first();
    await expect(row).toContainText('MP3 192');
    await row.getByRole('button', { name: 'Edit' }).click();
    const form = modal.locator('[data-share-editor]');
    await expect(form).toBeVisible();
    await form.locator('summary').click();
    await expect(form.locator('[data-share-quality] [role=status]')).toContainText('ready');
    await form.getByLabel('Shared audio quality').selectOption('aac128');
    await form.getByRole('button', { name: 'Save' }).click();
    await expect(row).toContainText('AAC 128');
    const forged = await page.request.get(`/s/${token}/stream?profile=original`);
    expect(forged.status()).toBe(200);
    const file = evidence.path('edited-aac128.bin');
    writeFileSync(file, await forged.body());
    const probe = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-show_streams', '-of', 'json', file], { encoding: 'utf8' }));
    expect(probe.streams[0].codec_name).toBe('aac');

    await row.getByRole('button', { name: 'Edit' }).click();
    await form.locator('summary').click();
    await form.getByLabel('Shared audio quality').selectOption('opus96');
    await context.setOffline(true);
    await form.getByRole('button', { name: 'Save' }).click();
    await expect(form.locator('[data-status]')).toContainText('Could not save share');
    await expect(form.getByLabel('Shared audio quality')).toHaveValue('opus96');
    await context.setOffline(false);
    await form.getByRole('button', { name: 'Save' }).click();
    await expect(row).toContainText('Opus 96');
    const saved = await (await page.request.get('/api/shares')).json();
    expect(saved.find((share: any) => share.token === token)?.audio_quality).toBe('opus96');
    await row.getByRole('button', { name: 'Edit' }).click();
    await form.getByLabel('Description').focus();
    await page.keyboard.press('Escape');
    await expect(form).toHaveCount(0);
    await expect(row.getByRole('button', { name: 'Edit' })).toBeFocused();
    evidence.record({ feature: 'M65', scenario: 'My Shares policy edit, actual encoded bytes, offline retained draft and keyboard Escape focus', result: 'pass', artifacts: ['edited-aac128.bin'] });
  } finally {
    await context.setOffline(false);
    await page.request.delete(`/api/shares/${token}`).catch(() => {});
  }
});

test('M65 album share applies one policy to collection item playback and download', async ({ page, browser }, info) => {
  test.setTimeout(120_000);
  const f = loadFixture();
  const evidence = new Evidence(process.env.PLAYER_M65_EVIDENCE!, info.project.name,
    { feature: 'M65 collection', browser_version: browser.version(), server_version: f.serverVersion });
  await keyboardLogin(page, f.user);
  const albums = await (await page.request.get('/api/music/albums')).json();
  const album = albums.find((item: any) => item.title === 'Night Transit');
  expect(album).toBeTruthy();
  await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
  await openCollectionMoreAction(page, 'Share…');
  const dialog = page.getByRole('dialog', { name: 'Share music' });
  await expect(dialog).toContainText('songs');
  await dialog.locator('summary').click();
  await expect(dialog.locator('[data-share-quality] [role=status]')).toContainText('ready');
  await dialog.getByLabel('Shared audio quality').selectOption('mp3192');
  await dialog.getByRole('button', { name: 'Create link' }).click();
  await expect(dialog.locator('[data-status]')).toContainText('Link ready');
  const token = (await dialog.getByLabel('Share link').inputValue()).split('/s/')[1];
  await dialog.getByRole('button', { name: 'Close' }).click();
  const anonymous = await browser.newContext();
  try {
    const guest = await anonymous.newPage();
    expect((await guest.goto(`/s/${token}`))?.status()).toBe(200);
    await expect(guest.getByRole('region', { name: 'Shared collection' })).toBeVisible();
    const metadata = await (await anonymous.request.get(`/s/${token}`, { headers: { Accept: 'application/json' } })).json();
    expect(metadata.audio_quality).toBe('mp3192');
    expect(metadata.items.length).toBeGreaterThan(1);
    await guest.locator('.share-track-list button').first().click();
    await expect.poll(() => guest.getByLabel('Shared music player').evaluate((audio: HTMLAudioElement) => audio.currentTime),
      { timeout: 25_000 }).toBeGreaterThan(.5);
    for (const index of [0, metadata.items.length - 1]) {
      const item = metadata.items[index];
      expect(item.playback_url).toContain('/compat');
      const response = await anonymous.request.get(`${item.stream_url}&profile=original`);
      expect(response.status()).toBe(200);
      const file = evidence.path(`collection-${index}.bin`);
      writeFileSync(file, await response.body());
      const probe = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-show_streams', '-of', 'json', file], { encoding: 'utf8' }));
      expect(probe.streams[0].codec_name).toBe('mp3');
      const download = await anonymous.request.get(item.download_url);
      expect(download.status()).toBe(200);
      expect(download.headers()['content-disposition']).toContain('.mp3');
    }
    evidence.record({ feature: 'M65', scenario: 'album UI share policy enforced on first and last guest collection occurrences', result: 'pass', artifacts: ['collection-0.bin', `collection-${metadata.items.length - 1}.bin`] });
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/shares/${token}`).catch(() => {});
  }
});
