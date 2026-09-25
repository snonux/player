import { test, expect, admin, createUser, podcast } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
test('S17: podcast feed and episode shapes, missing IDs and subscribe authorization', async () => {
  const { cookie, api } = await admin(); expect(await api('GET', '/podcasts')).toEqual([]);
  await podcast(cookie, async feed => {
    const feeds = await api('GET', '/podcasts'); expect(feeds).toHaveLength(1);
    expect(feeds[0]).toMatchObject({ id: feed.id, set_id: expect.any(Number), feed_url: expect.any(String), title: expect.any(String), check_interval_minutes: expect.any(Number), auto_download: expect.any(Boolean), created_at: expect.any(String) });
    const episodes = await api('GET', `/podcasts/${feed.set_id}/episodes`); expect(episodes).toHaveLength(2);
    expect(episodes[0]).toMatchObject({ id: expect.any(Number), feed_id: feed.id, guid: expect.any(String), title: expect.any(String), episode_url: expect.any(String), is_downloaded: false, is_completed: false, position_seconds: 0 });
    expect((await call('GET', '/api/v1/podcasts/999999999/episodes', { cookie })).status).toBe(404);
    expect((await call('GET', '/api/v1/podcasts/0/episodes', { cookie })).status).toBe(400);
    const user = await createUser(cookie);
    expect(Array.isArray(await json(await call('GET', '/api/v1/podcasts', { cookie: user.cookie })))).toBe(true);
    expect((await call('POST', '/api/v1/podcasts', { cookie: user.cookie, body: { feed_url: feed.feed_url } })).status).toBe(403);
    expect(await api('GET', '/podcasts')).toHaveLength(1);
    await api('DELETE', `/admin/users/${user.id}`);
  });
});
