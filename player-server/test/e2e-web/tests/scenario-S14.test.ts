import { test, expect } from '@playwright/test';
import { call, json, login } from './helpers/scenario-http';

type Progress = {
  running: boolean;
  sets_total: number;
  sets_done: number;
  files_total: number;
  files_done: number;
  last_error?: string;
};
type User = { id: number; username: string; is_admin: boolean };

function checkProgress(progress: Progress): void {
  expect(typeof progress.running).toBe('boolean');
  for (const field of ['sets_total', 'sets_done', 'files_total', 'files_done'] as const) {
    expect(Number.isInteger(progress[field]), field).toBe(true);
    expect(progress[field], field).toBeGreaterThanOrEqual(0);
  }
  expect(progress.last_error ?? '').toBe('');
}

test('S14: admin rescan progress is stable and denied to a regular user', async () => {
  test.setTimeout(90_000);
  const admin = await login();
  let userId: number | undefined;

  try {
    expect(await json<{ status: string }>(await call('POST', '/api/v1/admin/rescan', {
      cookie: admin.cookie, body: {},
    }))).toEqual({ status: 'ok' });

    let previousFilesDone = 0;
    for (let poll = 0; poll < 3; poll++) {
      const progress = await json<Progress>(await call('GET', '/api/v1/admin/scan-progress', {
        cookie: admin.cookie,
      }));
      checkProgress(progress);
      expect(progress.files_done).toBeGreaterThanOrEqual(previousFilesDone);
      previousFilesDone = progress.files_done;
    }

    // The scenario's database assertion requires at least one scanned media
    // row. Wait for the asynchronous scan before checking through the API.
    const scanDeadline = Date.now() + 60_000;
    while (true) {
      const progress = await json<Progress>(await call('GET', '/api/v1/admin/scan-progress', {
        cookie: admin.cookie,
      }));
      checkProgress(progress);
      if (!progress.running) break;
      expect(Date.now(), 'rescan did not finish within 60 seconds').toBeLessThan(scanDeadline);
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    const media = await json<Array<{ id: number }>>(await call('GET', '/api/v1/media', {
      cookie: admin.cookie,
    }));
    expect(media.length, 'rescan must populate media from the fixture root').toBeGreaterThan(0);

    const username = `e2e-rescan-user-${Date.now()}`;
    const password = 'TestPassw0rd!';
    const user = await json<User>(await call('POST', '/api/v1/admin/users', {
      cookie: admin.cookie, body: { username, password, is_admin: false },
    }));
    expect(user.id).toBeGreaterThan(0);
    expect(user.is_admin).toBe(false);
    userId = user.id;

    const regular = await login(username, password);
    expect((await call('POST', '/api/v1/admin/rescan', {
      cookie: regular.cookie, body: {},
    })).status).toBe(403);
    expect((await call('GET', '/api/v1/admin/scan-progress', {
      cookie: regular.cookie,
    })).status).toBe(403);
  } finally {
    if (userId !== undefined) {
      expect(await json<{ status: string }>(await call('DELETE', `/api/v1/admin/users/${userId}`, {
        cookie: admin.cookie,
      }))).toEqual({ status: 'ok' });
    }
  }
});
