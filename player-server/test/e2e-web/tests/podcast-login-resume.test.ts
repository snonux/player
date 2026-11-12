import { test, expect, type Page } from '@playwright/test';

// Started "Podcasts on this device" are listed on the login page and resume
// where listening stopped without signing in. The publisher feed and its
// enclosures are served by the test, so no network is used.
const FEED = 'https://feeds.player-e2e.test/resume.xml';

function wav(seconds = 30, rate = 8000): Buffer {
  const samples = seconds * rate;
  const buffer = Buffer.alloc(44 + samples * 2);
  buffer.write('RIFF', 0); buffer.writeUInt32LE(36 + samples * 2, 4); buffer.write('WAVE', 8);
  buffer.write('fmt ', 12); buffer.writeUInt32LE(16, 16); buffer.writeUInt16LE(1, 20); buffer.writeUInt16LE(1, 22);
  buffer.writeUInt32LE(rate, 24); buffer.writeUInt32LE(rate * 2, 28); buffer.writeUInt16LE(2, 32); buffer.writeUInt16LE(16, 34);
  buffer.write('data', 36); buffer.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; i++) buffer.writeInt16LE(Math.round(Math.sin(i / 8) * 2000), 44 + i * 2);
  return buffer;
}

function feed(): string {
  const items = [2, 1].map(n => `<item><title>Resume Episode ${n}</title><guid>resume-${n}</guid>
    <pubDate>${new Date(Date.UTC(2026, 9, n)).toUTCString()}</pubDate>
    <enclosure url="https://feeds.player-e2e.test/episode-${n}.wav" type="audio/wav" length="1"/></item>`).join('');
  return `<?xml version="1.0"?><rss version="2.0"><channel><title>Resume Fixture</title>${items}</channel></rss>`;
}

async function serveFeed(page: Page) {
  const audio = wav();
  await page.route('https://feeds.player-e2e.test/**', route => {
    const cors = { 'Access-Control-Allow-Origin': '*' };
    if (route.request().url().endsWith('.xml')) return route.fulfill({ body: feed(), headers: { ...cors, 'Content-Type': 'application/rss+xml' } });
    return route.fulfill({ body: audio, headers: { ...cors, 'Content-Type': 'audio/wav', 'Accept-Ranges': 'none' } });
  });
}

const audioState = (page: Page) => page.evaluate(() => {
  const audio = document.querySelector<HTMLAudioElement>('#audio-player, audio');
  return { time: audio?.currentTime ?? 0, paused: audio?.paused ?? true };
});
const playing = (page: Page) => page.evaluate(async () => (await import('/js/player.js')).currentMediaInfo()?.file_name ?? null);

test('login page resumes a started device podcast without signing in', async ({ page }) => {
  await page.goto('/index.html?local=podcasts');
  await page.evaluate(() => { localStorage.clear(); indexedDB.databases?.().then(list => list.forEach(db => db.name && indexedDB.deleteDatabase(db.name))); });
  await serveFeed(page);

  // Nothing started yet: the login page shows no shortcut.
  await page.goto('/login.html');
  await expect(page.locator('#login-podcast-resume')).toBeHidden();

  await page.goto('/index.html?local=podcasts');
  page.once('dialog', dialog => dialog.accept(FEED));
  await page.getByRole('button', { name: 'Add feed URL', exact: true }).click();
  await page.getByRole('button', { name: 'Resume Fixture', exact: true }).click();
  await page.getByRole('button', { name: 'Resume Episode 1', exact: true }).click();
  await expect.poll(() => playing(page)).toBe('Resume Episode 1');
  // Listen for a few seconds (the test enclosure does not support seeking).
  await expect.poll(async () => (await audioState(page)).time, { timeout: 10_000 }).toBeGreaterThan(4);
  // Leaving the page saves the position, like closing the tab.
  await page.goto('/login.html');

  const list = page.getByRole('region', { name: 'Continue listening' });
  await expect(list).toBeVisible();
  const link = list.getByRole('link', { name: /^Resume podcast: Resume Episode 1, Resume Fixture · Resume at 00:0[3-9]$/ });
  await expect(link).toBeVisible();
  const box = await link.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(44);

  await link.click();
  await expect(page).toHaveURL(/\/index\.html\?local=podcasts$/);
  await expect.poll(() => playing(page)).toBe('Resume Episode 1');
  await expect.poll(async () => (await audioState(page)).time, { timeout: 10_000 }).toBeGreaterThan(3.5);
});
