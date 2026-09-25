import { existsSync } from 'node:fs';
import { test, expect, admin, browserLogin, createUser, podcast } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S02: podcast subscription downloads real audio and records completion', async ({ sandbox }) => {
  const { cookie, api } = await admin();
  await podcast(cookie, async feed => {
    expect(await api('GET', '/podcasts')).toEqual(expect.arrayContaining([expect.objectContaining({ id: feed.id, feed_url: feed.feed_url })]));
    const episodes = await api('GET', `/podcasts/${feed.set_id}/episodes`);
    expect(episodes).toHaveLength(2);
    const episode = episodes[0];
    expect(episode.title).toBeTruthy();
    expect(episode.episode_url).toMatch(/\.mp3$/);
    const media = await api('POST', `/podcasts/episodes/${episode.id}/download`);
    expect(media.id).toBeGreaterThan(0);
    expect(media.file_name).toBeTruthy();
    expect(media.duration).toBeGreaterThan(0);
    expect(await api('GET', `/podcasts/${feed.set_id}/episodes`)).toEqual(expect.arrayContaining([expect.objectContaining({ id: episode.id, is_downloaded: true })]));
    // Crossing the actual playback threshold is required; a one-shot seek is not listening.
    for (const position_seconds of [12, 24, 36, 48, 60, 72, 120]) await api('POST', '/progress', { media_id: media.id, position_seconds });
    expect(await api('GET', '/in-progress')).toEqual(expect.arrayContaining([expect.objectContaining({ id: media.id })]));
    expect((await call('POST', `/api/v1/podcasts/episodes/${episode.id}/complete`, { cookie })).status).toBe(204);
    expect(await api('GET', `/podcasts/${feed.set_id}/episodes`)).toEqual(expect.arrayContaining([expect.objectContaining({ id: episode.id, is_completed: true })]));
    expect(sandbox.sql('SELECT count(*) FROM podcast_status WHERE is_completed=1')).toBe('1');

    // Unsubscribe: non-admins are denied, unknown feeds are 404, and the
    // admin call removes the feed plus its downloaded episode media.
    expect(media.abs_path).toBeTruthy();
    expect(existsSync(media.abs_path)).toBe(true);
    const listener = await createUser(cookie, 'podcast-listener');
    expect((await call('DELETE', `/api/v1/podcasts/${feed.id}`, { cookie: listener.cookie })).status).toBe(403);
    expect((await call('DELETE', '/api/v1/podcasts/999999', { cookie })).status).toBe(404);
    expect((await call('DELETE', '/api/v1/podcasts/abc', { cookie })).status).toBe(400);
    expect((await call('DELETE', `/api/v1/podcasts/${feed.id}`, { cookie })).status).toBe(204);
    expect(await api('GET', '/podcasts')).toEqual([]);
    expect(sandbox.sql(`SELECT count(*) FROM media WHERE id=${media.id}`)).toBe('0');
    expect(sandbox.sql('SELECT count(*) FROM podcast_episodes')).toBe('0');
    expect(existsSync(media.abs_path)).toBe(false);
  });
});

test('S02: podcast manager and episode cards work from the browser', async ({ page }) => {
  const { cookie, api } = await admin();
  await podcast(cookie, async feed => {
    await browserLogin(page, cookie);
    await page.goto('/');
    const title = feed.title || feed.feed_url;
    // The header auto-hides; reach the admin panel the keyboard way.
    await page.locator('#admin-toggle').focus();
    await page.keyboard.press('Enter');
    await page.locator('#admin-podcasts').focus();
    await page.keyboard.press('Enter');
    const modal = page.locator('#podcast-modal');
    await expect(modal).toHaveClass(/open/);
    const unsubscribe = modal.getByRole('button', { name: `Unsubscribe from ${title}` });
    await expect(unsubscribe).toBeVisible();

    // Dismissing the confirmation keeps the feed.
    page.once('dialog', dialog => dialog.dismiss());
    await unsubscribe.click();
    await expect(unsubscribe).toBeEnabled();
    expect(await api('GET', '/podcasts')).toHaveLength(1);

    let confirmText = '';
    page.once('dialog', dialog => { confirmText = dialog.message(); dialog.accept(); });
    await unsubscribe.click();
    await expect(modal.locator('#podcast-list')).toContainText('No podcasts subscribed.');
    expect(confirmText).toContain(title);
    expect(await api('GET', '/podcasts')).toEqual([]);

    // Resubscribe through the form with Enter.
    await page.locator('#podcast-url').fill(feed.feed_url);
    await page.keyboard.press('Enter');
    await expect(modal.getByRole('button', { name: `Unsubscribe from ${title}` })).toBeVisible();
    const [again] = await api('GET', '/podcasts');
    await page.keyboard.press('Escape');
    await expect(modal).not.toHaveClass(/open/);

    const set = (await api('GET', '/sets')).find((s: any) => s.id === again.set_id);
    await page.getByRole('button', { name: `Set ${set.name}`, exact: true }).click();
    await page.locator('#media-grid .folder-card .folder-open').first().click();
    const episodes = page.locator('#media-grid .episode-card');
    await expect(episodes).toHaveCount(2);
    // Episode IDs are not media IDs: media shortcuts (F, t, s) must not see one.
    await expect(episodes.first()).not.toHaveAttribute('data-id');
    await expect(episodes.first()).toHaveAttribute('data-episode-id', /\d+/);
    const favoritesBefore = await api('GET', '/media?favorites=true');
    await episodes.first().click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('F');
    expect(await api('GET', '/media?favorites=true')).toEqual(favoritesBefore);
    // s (share) explains instead of calling the API with an undefined ID.
    const sharesBefore = await api('GET', '/shares');
    await page.keyboard.press('s');
    await expect(page.locator('#toast')).toContainText('Select a media item first');
    expect(await api('GET', '/shares')).toEqual(sharesBefore);

    const complete = episodes.first().getByRole('button', { name: 'Mark as listened' });
    await complete.click();
    await expect(episodes.first().getByRole('button', { name: 'Mark as unlistened' })).toHaveAttribute('aria-pressed', 'true');

    // Play on an episode card downloads it and starts it as normal media.
    await episodes.nth(1).getByRole('button', { name: 'Download and play episode' }).click();
    await expect(page.locator('#player')).toHaveClass(/open/);
    await expect(episodes).toHaveCount(1);
    await expect(page.locator('#media-grid .audio-card')).toHaveCount(1);
    await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.currentSrc)).toMatch(/\/api\/media\/\d+\/stream/);
  });
});
