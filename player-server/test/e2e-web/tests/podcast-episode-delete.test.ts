import { test, expect, type Page } from '@playwright/test';

// Delete an episode from the podcast player and by a right-to-left touch swipe
// in the episode list. Browser device podcasts need no account; the publisher
// feed and its enclosures are served by the test, so no network is used.
const FEED = 'https://feeds.player-e2e.test/delete.xml';

function wav(seconds = 20, rate = 8000): Buffer {
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
  const items = [3, 2, 1].map(n => `<item><title>Delete Episode ${n}</title><guid>delete-${n}</guid>
    <pubDate>${new Date(Date.UTC(2026, 9, n)).toUTCString()}</pubDate>
    <enclosure url="https://feeds.player-e2e.test/episode-${n}.wav" type="audio/wav" length="1"/></item>`).join('');
  return `<?xml version="1.0"?><rss version="2.0"><channel><title>Delete Fixture</title>${items}</channel></rss>`;
}

async function subscribe(page: Page) {
  const audio = wav();
  await page.route('https://feeds.player-e2e.test/**', route => {
    const cors = { 'Access-Control-Allow-Origin': '*' };
    if (route.request().url().endsWith('.xml')) return route.fulfill({ body: feed(), headers: { ...cors, 'Content-Type': 'application/rss+xml' } });
    return route.fulfill({ body: audio, headers: { ...cors, 'Content-Type': 'audio/wav', 'Accept-Ranges': 'none' } });
  });
  await page.goto('/index.html?local=podcasts');
  page.once('dialog', dialog => dialog.accept(FEED));
  await page.getByRole('button', { name: 'Add feed URL', exact: true }).click();
  await page.getByRole('button', { name: 'Delete Fixture', exact: true }).click();
  await expect(page.locator('.local-episode-list > li')).toHaveCount(3);
}

const episodes = (page: Page) => page.locator('.local-episode-list > li');
const playing = (page: Page) => page.evaluate(async () => (await import('/js/player.js')).currentMediaInfo()?.file_name ?? null);

async function play(page: Page, title: string) {
  await page.getByRole('button', { name: title, exact: true }).click();
  await expect.poll(() => playing(page)).toBe(title);
}

async function deleteFromPlayer(page: Page, answer: boolean) {
  await page.locator('#btn-player-options').click();
  const button = page.locator('#player-options').getByRole('button', { name: 'Delete episode', exact: true });
  await expect(button).toBeVisible();
  page.once('dialog', dialog => answer ? dialog.accept() : dialog.dismiss());
  await button.click();
}

test.describe('podcast episode deletion from the player and by swipe', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/index.html?local=podcasts');
    await page.evaluate(() => { localStorage.clear(); indexedDB.databases?.().then(list => list.forEach(db => db.name && indexedDB.deleteDatabase(db.name))); });
  });

  test('player Delete episode confirms, deletes and plays the next episode; the last one stops', async ({ page }) => {
    await subscribe(page);
    await play(page, 'Delete Episode 3');
    // Cancel keeps both the episode and playback.
    await deleteFromPlayer(page, false);
    await expect(episodes(page)).toHaveCount(3);
    expect(await playing(page)).toBe('Delete Episode 3');

    await deleteFromPlayer(page, true);
    await expect.poll(() => playing(page)).toBe('Delete Episode 2');
    await expect(episodes(page)).toHaveCount(2);
    await expect(page.getByRole('button', { name: 'Delete Episode 3', exact: true })).toHaveCount(0);

    await play(page, 'Delete Episode 1');
    await deleteFromPlayer(page, true);
    await expect.poll(() => playing(page)).toBeNull();
    await expect(episodes(page)).toHaveCount(1);
  });

  test('without a podcast episode the player offers no Delete episode', async ({ page }) => {
    await page.goto('/index.html?local=podcasts');
    await expect(page.locator('#btn-delete-episode')).toBeAttached();
    await expect(page.locator('#btn-delete-episode')).toBeHidden();
  });

  test('right-to-left touch swipe deletes after confirmation; other drags do not', async ({ page }) => {
    await subscribe(page);
    const swipe = (title: string, dx: number) => page.evaluate(({ title, dx }) => {
      const row = [...document.querySelectorAll('.local-episode-list > li')].find(li => li.textContent?.includes(title))!;
      const box = row.getBoundingClientRect();
      const y = box.top + box.height / 2, x = box.left + box.width * 0.8;
      const fire = (type: string, at: number) => row.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerId: 7, pointerType: 'touch', isPrimary: true, clientX: at, clientY: y }));
      fire('pointerdown', x); fire('pointermove', x + dx / 3); fire('pointermove', x + dx); fire('pointerup', x + dx);
    }, { title, dx });
    let dialogs = 0;
    page.on('dialog', dialog => { dialogs++; void (dialogs === 1 ? dialog.dismiss() : dialog.accept()); });
    await swipe('Delete Episode 2', 250); // left to right: nothing
    await swipe('Delete Episode 2', -30); // too short: nothing
    expect(dialogs).toBe(0);
    await swipe('Delete Episode 2', -250); // dismissed confirmation keeps it
    await expect.poll(() => dialogs).toBe(1);
    await expect(episodes(page)).toHaveCount(3);
    await swipe('Delete Episode 2', -250);
    await expect.poll(() => dialogs).toBe(2);
    await expect(episodes(page)).toHaveCount(2);
    await expect(page.getByRole('button', { name: 'Delete Episode 2', exact: true })).toHaveCount(0);
    // Grid layout keeps its cards still.
    await page.getByRole('button', { name: 'Grid', exact: true }).click();
    await swipe('Delete Episode 1', -250);
    await page.waitForTimeout(300);
    expect(dialogs).toBe(2);
  });
});
