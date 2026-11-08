import { existsSync } from 'node:fs';
import { test, expect, admin, browserLogin, createUser, podcast } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
import { headerItem } from './helpers/header-menu';
import { cardAction } from './helpers/card-actions';
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
    expect(sandbox.sql('SELECT count(*) FROM podcast_episode_state WHERE completed=1')).toBe('1');

    // Remove from server: non-admins are denied, unknown feeds are 404, and
    // the admin call removes the downloaded episode media. The feed itself
    // stays listed while anyone (here the admin) is still subscribed.
    expect(media.abs_path).toBeTruthy();
    expect(existsSync(media.abs_path)).toBe(true);
    const listener = await createUser(cookie, 'podcast-listener');
    expect((await call('DELETE', `/api/v1/podcasts/${feed.id}`, { cookie: listener.cookie })).status).toBe(403);
    expect((await call('DELETE', '/api/v1/podcasts/999999', { cookie })).status).toBe(404);
    expect((await call('DELETE', '/api/v1/podcasts/abc', { cookie })).status).toBe(400);
    expect((await call('DELETE', `/api/v1/podcasts/${feed.id}`, { cookie })).status).toBe(204);
    expect(await api('GET', '/podcasts')).toEqual([expect.objectContaining({ id: feed.id, subscribed: true })]);
    expect(sandbox.sql(`SELECT count(*) FROM media WHERE id=${media.id}`)).toBe('0');
    expect(existsSync(media.abs_path)).toBe(false);
    expect(await api('GET', `/podcasts/${feed.set_id}/episodes`)).toEqual(expect.arrayContaining([expect.objectContaining({ id: episode.id, is_downloaded: false })]));
  });
});

test('S02: podcast manager and episode cards work from the browser', async ({ page }) => {
  const { cookie, api } = await admin();
  await podcast(cookie, async feed => {
    await browserLogin(page, cookie);
    await page.goto('/');
    const title = feed.title || feed.feed_url;
    // Reach the admin panel from the header menu the keyboard way.
    await (await headerItem(page, '#admin-toggle')).focus();
    await page.keyboard.press('Enter');
    await page.locator('#admin-more-toggle').focus();
    await page.keyboard.press('Enter');
    await page.locator('#admin-podcasts').focus();
    await page.keyboard.press('Enter');
    const modal = page.locator('#podcast-modal');
    await expect(modal).toHaveClass(/open/);
    const unsubscribe = modal.getByRole('button', { name: `Unsubscribe from ${title}` });
    await expect(unsubscribe).toBeVisible();

    // Unsubscribing is per user and needs no confirmation; the feed stays on
    // the server under "Not subscribed" and can be subscribed again.
    await unsubscribe.click();
    const subscribe = modal.getByRole('button', { name: `Subscribe to ${title}` });
    await expect(subscribe).toBeVisible();
    expect((await api('GET', '/podcasts')).find((p: any) => p.id === feed.id)?.subscribed).toBeFalsy();
    await subscribe.click();
    await expect(unsubscribe).toBeVisible();
    expect((await api('GET', '/podcasts')).find((p: any) => p.id === feed.id)?.subscribed).toBe(true);

    // Removing from the server deletes it for everyone, so it asks first;
    // dismissing the confirmation keeps the feed.
    const remove = modal.getByRole('button', { name: `Remove ${title} from the server` });
    page.once('dialog', dialog => dialog.dismiss());
    await remove.click();
    await expect(remove).toBeEnabled();
    expect(await api('GET', '/podcasts')).toHaveLength(1);
    // Removal keeps a feed listed while anyone is still subscribed, so
    // unsubscribe first and then remove it.
    await unsubscribe.click();
    await expect(subscribe).toBeVisible();
    let confirmText = '';
    page.once('dialog', dialog => { confirmText = dialog.message(); dialog.accept(); });
    await remove.click();
    await expect(modal.locator('#podcast-list')).toContainText('No podcasts on the server.');
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
    // A podcast set opens straight to its episode list (uncompleted first).
    const episodes = page.locator('#media-grid .episode-card');
    await expect(episodes).toHaveCount(2);
    // Media shortcuts (F, s) must not favorite or share an episode that has
    // no server copy.
    await expect(episodes.first()).toHaveAttribute('data-episode-id', /\d+/);
    const favoritesBefore = await api('GET', '/media?favorites=true');
    await episodes.first().click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('F');
    expect(await api('GET', '/media?favorites=true')).toEqual(favoritesBefore);
    const sharesBefore = await api('GET', '/shares');
    await page.keyboard.press('s');
    await expect(page.locator('#toast')).toContainText('Select a media item first');
    expect(await api('GET', '/shares')).toEqual(sharesBefore);

    // Completing moves the episode from Uncompleted to Completed.
    const first = episodes.first();
    const firstTitle = (await first.locator('.meta .title').textContent())!;
    await cardAction(page, first, 'Mark completed');
    await expect(episodes).toHaveCount(1);
    await page.getByRole('button', { name: 'Completed', exact: true }).click();
    await expect(episodes).toHaveCount(1);
    await expect(episodes.first()).toContainText(firstTitle);
    await expect(episodes.first().locator('[data-action="complete"]')).toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('button', { name: 'Uncompleted', exact: true }).click();
    await expect(episodes).toHaveCount(1);

    // Download to the server, then Play starts the episode in the player.
    const other = episodes.first();
    await other.hover();
    await other.getByRole('button', { name: /^Download episode/ }).click();
    await expect(other).toContainText('Downloaded on server');
    await other.hover();
    await other.getByRole('button', { name: 'Play episode', exact: true }).click();
    await expect(page.locator('#player')).toHaveClass(/open/);
    await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.currentSrc)).toMatch(/\/api\/(media\/\d+|podcasts\/episodes\/\d+)\/stream/);
  });
});
