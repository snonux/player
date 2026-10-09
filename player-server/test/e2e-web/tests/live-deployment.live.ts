/**
 * live-deployment.live.ts — end-to-end journeys against a deployed Player
 * instance that already holds the generated "all formats" test library
 * (sets test-videos, test-audio, test-images; one sample-<ext>.<ext> file per
 * supported extension).
 *
 * Run through playwright.live.config.ts. Required environment:
 *   PLAYER_URL                         base URL of the instance
 *   E2E_ADMIN_USER / E2E_ADMIN_PASS    existing admin account
 *   E2E_USER / E2E_USER_PASS           existing non-admin account
 *
 * The suite cleans up the favorites, notes, tags, progress, shares and
 * permissions it creates. The uploaded file is soft-deleted (it stays in the
 * admin trash) so the operator can remove it from disk afterwards.
 */
import { test, expect, type Browser, type Page } from '@playwright/test';

test.use({ serviceWorkers: 'block' });
test.describe.configure({ mode: 'serial' });

const ADMIN = { user: process.env.E2E_ADMIN_USER || '', pass: process.env.E2E_ADMIN_PASS || '' };
const USER = { user: process.env.E2E_USER || '', pass: process.env.E2E_USER_PASS || '' };

const FORMATS = {
  'test-videos': ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm'],
  'test-audio': ['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'wma', 'm4b', 'opus'],
  'test-images': ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'avif', 'svg'],
} as const;

// Formats expected not to play in the web UI. Empty since the server offers a
// compatibility stream (transcoded MP4/M4A) for what browsers cannot decode
// (AVI, WMV, FLV, WMA, ...): all 16 audio/video formats must play.
const BROWSER_UNSUPPORTED = new Set<string>();

// A first play of a transcoded format waits for the server-side transcode
// (the web UI shows "Preparing ..." and retries); plain streams start at once.
const PLAIN_START_MS = 10_000;
const TRANSCODE_START_MS = 90_000;

type Media = {
  id: number; set_id: number; file_name: string; type: string; file_size_bytes: number; thumbnail_path: string;
  // Server hint: play /compat instead of /stream.
  transcoded?: boolean;
};

async function login(page: Page, who: { user: string; pass: string }) {
  await page.goto('/login.html');
  await page.locator('#username').fill(who.user);
  await page.locator('#password').fill(who.pass);
  await page.click('button[type="submit"]');
  await page.waitForURL(url => url.pathname === '/');
  await page.waitForSelector('#logout-btn', { state: 'attached' });
}

// The site header slides out of view until hovered; reveal it before clicking.
async function revealHeader(page: Page) {
  await page.mouse.move(200, 2);
  await page.locator('header').first().hover({ force: true }).catch(() => {});
}

async function openSet(page: Page, name: string) {
  await page.goto('/');
  await page.locator('.set-card').filter({ hasText: name }).locator('.title').click();
  await expect(page.locator('#media-grid .media-card[data-id]').first()).toBeVisible();
}

function card(page: Page, fileName: string) {
  return page.locator(`#media-grid .media-card[aria-label="${fileName}"]`);
}

async function allMedia(page: Page): Promise<Media[]> {
  const res = await page.request.get('/api/v1/media?limit=500');
  expect(res.ok()).toBeTruthy();
  return (await res.json()) as Media[];
}

async function mediaByName(page: Page, fileName: string): Promise<Media> {
  const found = (await allMedia(page)).find(m => m.file_name === fileName);
  expect(found, `media ${fileName} must exist`).toBeTruthy();
  return found!;
}

// playbackState polls the <video>/<audio> element until it really advances
// or reports a decode error, and returns what the browser saw. While a compat
// stream is being prepared the element has no source and no error, so the
// poll simply keeps waiting up to timeoutMs.
async function playbackState(page: Page, kind: 'video' | 'audio', timeoutMs = PLAIN_START_MS) {
  return page.evaluate(async ({ elementId, timeoutMs }) => {
    const el = document.getElementById(elementId) as HTMLMediaElement;
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (el.error) return { ok: false, reason: `MediaError ${el.error.code} ${el.error.message}` };
      if (el.readyState >= 2 && el.currentTime > 0.4) {
        return { ok: true, reason: '', duration: el.duration, width: (el as HTMLVideoElement).videoWidth || 0 };
      }
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    return { ok: false, reason: `timeout readyState=${el.readyState} networkState=${el.networkState} paused=${el.paused} t=${el.currentTime}` };
  }, { elementId: kind === 'video' ? 'media-video' : 'media-audio', timeoutMs });
}

// expectCleanPlayback waits until playback has passed upTo seconds ('end':
// until it is within a second of the end of the clip) and fails if the
// element reports an error or the player raises its error toast on the way.
// A first buffered chunk is not proof of playback: a stream that breaks on a
// later request still advances currentTime for a moment before it fails.
async function expectCleanPlayback(page: Page, kind: 'video' | 'audio', upTo: number | 'end') {
  const result = await page.evaluate(async ({ elementId, upTo }) => {
    const el = document.getElementById(elementId) as HTMLMediaElement;
    const deadline = Date.now() + 30_000;
    while (Date.now() < deadline) {
      if (el.error) return `MediaError ${el.error.code} ${el.error.message} at t=${el.currentTime}`;
      const target = upTo === 'end' ? el.duration - 1 : upTo;
      if (el.ended || el.currentTime >= target) return '';
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    return `timeout before ${upTo}: t=${el.currentTime} duration=${el.duration} paused=${el.paused} readyState=${el.readyState}`;
  }, { elementId: kind === 'video' ? 'media-video' : 'media-audio', upTo });
  expect(result, 'playback must continue without an element error').toBe('');
  expect(await page.locator(`#media-${kind}`).evaluate((el: HTMLMediaElement) => el.error)).toBeNull();
  await expect(page.locator('#toast')).not.toHaveClass(/error/);
  await expect(page.locator('#toast')).not.toContainText('Cannot play');
}

// Past this position a sample (8 s video, 12 s audio) has needed more than
// its first buffered chunk; cheap enough to check for every format.
const CLEAN_PLAY_SECONDS = 3;

// playAndObserve starts an item from the grid and reports whether it plays.
// The URL the element ends up with must follow the server's "transcoded" hint.
async function playAndObserve(page: Page, set: string, kind: 'video' | 'audio', ext: string) {
  await openSet(page, set);
  const item = await mediaByName(page, `sample-${ext}.${ext}`);
  await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
  await expect(page.locator('#player')).toHaveClass(/open/);
  const state = await playbackState(page, kind, item.transcoded ? TRANSCODE_START_MS : PLAIN_START_MS);
  const src = await page.locator(`#media-${kind}`).evaluate((el: HTMLMediaElement) => el.getAttribute('src') || '');
  test.info().annotations.push({ type: 'playback', description: `${ext}: ${state.ok ? 'plays' : state.reason}; src=${src}; toast="${await page.locator('#toast').textContent()}"` });
  if (state.ok) {
    expect(src).toBe(`/api/media/${item.id}/${item.transcoded ? 'compat' : 'stream'}`);
    await expectCleanPlayback(page, kind, CLEAN_PLAY_SECONDS);
  }
  return state;
}

// openShare loads a share page and waits until it has opened its viewing
// (POST /s/{token}/view) and handed the media to the player. Before that the
// player has nothing to play and a press on play would do nothing.
async function openShare(guest: Page, token: string) {
  expect((await guest.goto(`/s/${token}`))?.status()).toBe(200);
  await expect(guest.locator('body')).toHaveAttribute('data-share-viewing', 'open');
}

// withShare creates a share for mediaId (body: e.g. { max_uses: 50 }), hands
// an anonymous page and the new share's token to run, and always revokes the
// share and closes the anonymous context, also when an assertion fails.
async function withShare(page: Page, browser: Browser, mediaId: number, body: Record<string, unknown>,
  run: (guest: Page, token: string) => Promise<void>) {
  const created = await page.request.post(`/api/v1/media/${mediaId}/shares`, { data: body });
  expect(created.ok()).toBeTruthy();
  const { token } = (await created.json()) as { token: string };
  const anonymous = await browser.newContext();
  try {
    await run(await anonymous.newPage(), token);
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/v1/shares/${token}`);
  }
}

test.beforeAll(() => {
  for (const [key, value] of Object.entries({ PLAYER_URL: process.env.PLAYER_URL, ...ADMIN, ...USER })) {
    if (!value) throw new Error(`live suite needs credentials and PLAYER_URL in the environment (missing ${key})`);
  }
});

test.describe('auth', () => {
  test('health endpoints answer', async ({ request }) => {
    expect((await request.get('/healthz')).status()).toBe(200);
    expect((await request.get('/readyz')).status()).toBe(200);
  });

  test('anonymous browser is sent to the login page', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/\/login\.html/);
    await expect(page.locator('#login-form')).toBeVisible();
  });

  test('wrong password shows an error and stays on login', async ({ page }) => {
    await page.goto('/login.html');
    await page.locator('#username').fill(ADMIN.user);
    await page.locator('#password').fill('definitely-wrong-password');
    await page.click('button[type="submit"]');
    await expect(page.locator('#login-error')).not.toBeEmpty();
    await expect(page).toHaveURL(/\/login\.html/);
  });

  test('bootstrap stays closed on a bootstrapped instance', async ({ request }) => {
    const res = await request.post('/api/v1/auth/bootstrap', { data: { username: 'intruder', password: 'Intruder-Passw0rd!' } });
    expect(res.status()).toBe(403);
  });
});

test.describe('library as admin', () => {
  test.beforeEach(async ({ page }) => { await login(page, ADMIN); });

  test('the three test sets are listed with every expected file', async ({ page }) => {
    for (const [set, exts] of Object.entries(FORMATS)) {
      await openSet(page, set);
      for (const ext of exts) await expect(card(page, `sample-${ext}.${ext}`)).toBeVisible();
      await expect(page.locator('#media-grid .media-card[data-id]')).toHaveCount(exts.length);
    }
  });

  test('video and image thumbnails render in the grid', async ({ page }) => {
    for (const set of ['test-videos', 'test-images']) {
      await openSet(page, set);
      const images = page.locator('#media-grid .media-card[data-id] .thumb-wrap img');
      const count = await images.count();
      expect(count).toBe(FORMATS[set as keyof typeof FORMATS].length);
      for (let i = 0; i < count; i++) {
        await images.nth(i).scrollIntoViewIfNeeded();
        await expect.poll(() => images.nth(i).evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0),
          { message: `thumbnail ${i} in ${set}` }).toBe(true);
      }
    }
  });

  for (const ext of FORMATS['test-videos']) {
    test(`video .${ext} ${BROWSER_UNSUPPORTED.has(ext) ? 'is not browser-playable' : 'plays'}`, async ({ page }) => {
      const state = await playAndObserve(page, 'test-videos', 'video', ext);
      if (BROWSER_UNSUPPORTED.has(ext)) {
        expect(state.ok, 'format unexpectedly plays in the browser').toBe(false);
      } else {
        expect(state.ok, state.reason).toBe(true);
        expect(state.width).toBeGreaterThan(0);
      }
    });
  }

  for (const ext of FORMATS['test-audio']) {
    test(`audio .${ext} ${BROWSER_UNSUPPORTED.has(ext) ? 'is not browser-playable' : 'plays'}`, async ({ page }) => {
      const state = await playAndObserve(page, 'test-audio', 'audio', ext);
      expect(state.ok, state.reason).toBe(!BROWSER_UNSUPPORTED.has(ext));
    });
  }

  for (const ext of FORMATS['test-images']) {
    test(`image .${ext} opens in the viewer`, async ({ page }) => {
      await openSet(page, 'test-images');
      const item = await mediaByName(page, `sample-${ext}.${ext}`);
      await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
      await expect(page.locator('#player')).toHaveClass(/open.*has-image/);
      const image = page.locator('#media-image');
      await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${item.id}/stream$`));
      await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
    });
  }

  test('an undecodable source shows an error toast and resets the play button', async ({ page }) => {
    const item = await mediaByName(page, 'sample-mp4.mp4');
    // Serve garbage instead of the file: the element fails to decode it.
    await page.route(`**/api/media/${item.id}/stream`, route => route.fulfill({
      status: 200,
      contentType: 'video/mp4',
      body: Buffer.alloc(64 * 1024, 0x5a),
    }));
    await openSet(page, 'test-videos');
    await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
    await expect(page.locator('#toast')).toContainText(`Cannot play ${item.file_name}:`, { timeout: 15_000 });
    await expect(page.locator('#toast')).toHaveClass(/error/);
    await expect(page.locator('#btn-play')).toHaveText('▶');
    await expect(page.locator('#big-play')).not.toHaveClass(/hidden/);
    expect(await page.locator('#media-video').evaluate((el: HTMLMediaElement) => el.error?.code ?? 0)).toBeGreaterThan(0);
  });

  test('a failed compatibility stream shows an error toast instead of loading', async ({ page }) => {
    const item = await mediaByName(page, 'sample-avi.avi');
    expect(item.transcoded, 'the server must flag AVI as transcoded').toBe(true);
    await page.route(`**/api/media/${item.id}/compat`, route => route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'transcode failed' }),
    }));
    await openSet(page, 'test-videos');
    await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
    await expect(page.locator('#toast')).toContainText(`Cannot play ${item.file_name}: the server could not convert this file`);
    await expect(page.locator('#btn-play')).toHaveText('▶');
    expect(await page.locator('#media-video').getAttribute('src')).toBeNull();

    // Pressing play again retries; this time the real server answers.
    await page.unroute(`**/api/media/${item.id}/compat`);
    await page.locator('#btn-play').click();
    const state = await playbackState(page, 'video', TRANSCODE_START_MS);
    expect(state.ok, state.reason).toBe(true);
  });

  // A share's max_uses counts viewings, not HTTP requests. Fetching the page
  // is free; the page opens the viewing with POST /s/{token}/view (one use)
  // and gets a cookie, while the JSON form opens it itself and carries the
  // credential as the "view" parameter in its URLs. Either way the credential
  // covers every ranged request of that viewing.
  test('a transcoded share plays to the end through its playback_url without a session', async ({ page, browser }) => {
    const item = await mediaByName(page, 'sample-wmv.wmv');
    await withShare(page, browser, item.id, {}, async (guest, token) => {
      const meta = (await (await guest.request.get(`/s/${token}`, { headers: { Accept: 'application/json' } })).json()) as { playback_url: string; transcoded: boolean; view: string };
      expect(meta.transcoded).toBe(true);
      // The JSON form carries the credential in its URLs; the page does not
      // need it (cookie) and plays the plain path.
      expect(meta.view).toBeTruthy();
      expect(meta.playback_url).toBe(`/s/${token}/compat?view=${meta.view}`);
      await openShare(guest, token);
      // The share page does not autoplay; the click is honoured once the stream is ready.
      await guest.locator('#btn-play').click();
      const state = await playbackState(guest, 'video', TRANSCODE_START_MS);
      expect(state.ok, state.reason).toBe(true);
      expect(await guest.locator('#media-video').getAttribute('src')).toBe(`/s/${token}/compat`);
      await expectCleanPlayback(guest, 'video', 'end');
    });
  });

  // The regression this suite once had to leave out: with one use per HTTP
  // request a max_uses: 1 share broke after the first chunk (second ranged
  // request: 410, MediaError 2), for transcoded media as for plain MP4.
  test('a single-use transcoded share plays to the end and is then spent for others', async ({ page, browser }) => {
    const item = await mediaByName(page, 'sample-wmv.wmv');
    await withShare(page, browser, item.id, { max_uses: 1 }, async (guest, token) => {
      const refused: string[] = [];
      guest.on('response', res => {
        if (new URL(res.url()).pathname.startsWith(`/s/${token}`) && res.status() === 410) refused.push(res.url());
      });
      // What a messenger does with the link before the recipient sees it:
      // fetching the page is free and must leave the one use untouched.
      const preview = await page.request.get(`/s/${token}`, { headers: { 'User-Agent': 'TelegramBot (like TwitterBot)' } });
      expect(preview.status()).toBe(200);
      await openShare(guest, token);
      await guest.locator('#btn-play').click();
      const state = await playbackState(guest, 'video', TRANSCODE_START_MS);
      expect(state.ok, state.reason).toBe(true);
      await expectCleanPlayback(guest, 'video', 'end');
      // A reload in the same browser is the same viewing.
      expect((await guest.reload())?.status()).toBe(200);
      expect(refused, 'no request of the viewing may be refused').toEqual([]);

      const shares = (await (await page.request.get('/api/v1/shares')).json()) as Array<{ token: string; used_count: number }>;
      expect(shares.find(s => s.token === token)?.used_count).toBe(1);

      // Any other browser finds the single use spent.
      const stranger = await browser.newContext();
      try {
        for (const path of [`/s/${token}`, `/s/${token}/compat`, `/s/${token}/stream`]) {
          expect((await stranger.request.get(path)).status(), `${path} for another browser`).toBe(410);
        }
      } finally {
        await stranger.close();
      }
    });
  });

  test('a transcoded audio share with a use limit plays to the end', async ({ page, browser }) => {
    const item = await mediaByName(page, 'sample-wma.wma');
    await withShare(page, browser, item.id, { max_uses: 50 }, async (guest, token) => {
      await openShare(guest, token);
      await guest.locator('#btn-play').click();
      const state = await playbackState(guest, 'audio', TRANSCODE_START_MS);
      expect(state.ok, state.reason).toBe(true);
      expect(await guest.locator('#media-audio').getAttribute('src')).toBe(`/s/${token}/compat`);
      await expectCleanPlayback(guest, 'audio', 'end');
    });
  });

  test('seeking uses HTTP range requests', async ({ page }) => {
    const item = await mediaByName(page, 'sample-mp4.mp4');
    const res = await page.request.get(`/api/v1/media/${item.id}/stream`, { headers: { Range: 'bytes=1000-1999' } });
    expect(res.status()).toBe(206);
    expect((await res.body()).length).toBe(1000);
  });

  test('search narrows the grid to the matching file', async ({ page }) => {
    await openSet(page, 'test-videos');
    await page.keyboard.press('/');
    await expect(page.locator('#search-input')).toBeFocused();
    await page.locator('#search-input').fill('webm');
    await expect(page.locator('#media-grid .media-card[data-id]')).toHaveCount(1);
    await expect(card(page, 'sample-webm.webm')).toBeVisible();
  });

  test('favorite can be toggled on and off', async ({ page }) => {
    await openSet(page, 'test-audio');
    const item = await mediaByName(page, 'sample-mp3.mp3');
    const heart = card(page, item.file_name).locator('[data-action="favorite"]');
    await heart.click({ force: true });
    await expect(page.locator('#toast')).toContainText('Added to favorites');
    const favorites = (await (await page.request.get('/api/v1/media?favorites=true')).json()) as Media[];
    expect(favorites.map(m => m.id)).toContain(item.id);
    await heart.click({ force: true });
    await expect(page.locator('#toast')).toContainText('Removed from favorites');
  });

  test('a note is saved, survives a reload and can be deleted', async ({ page }) => {
    const text = `live e2e note ${Date.now()}`;
    await openSet(page, 'test-audio');
    const notes = () => card(page, 'sample-flac.flac').locator('[data-action="notes"]');
    await notes().click({ force: true });
    await expect(page.locator('#notes-modal')).toHaveClass(/open/);
    await page.locator('#notes-textarea').fill(text);
    await page.locator('#notes-save').click();
    await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
    await openSet(page, 'test-audio');
    await notes().click({ force: true });
    await expect(page.locator('#notes-textarea')).toHaveValue(text);
    await page.locator('#notes-delete').click();
    await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
    const item = await mediaByName(page, 'sample-flac.flac');
    expect((await page.request.get(`/api/v1/media/${item.id}/notes`)).status()).toBe(204);
  });

  test('a tag can be added, used as a filter and removed', async ({ page }) => {
    const tag = `live-e2e-${Date.now()}`;
    await openSet(page, 'test-images');
    const target = card(page, 'sample-png.png');
    await target.locator('[data-action="tags"]').click({ force: true });
    await expect(page.locator('#tags-modal')).toHaveClass(/open/);
    await page.locator('#tags-new').fill(tag);
    await page.locator('#tags-add').click();
    await expect(page.locator('#tags-list .tag-chip').filter({ hasText: tag })).toHaveCount(1);
    await page.locator('#tags-close').click();
    await page.keyboard.press('/');
    await page.locator('#search-input').fill(`tag:${tag}`);
    await expect(page.locator('#media-grid .media-card[data-id]')).toHaveCount(1);
    await target.locator('[data-action="tags"]').click({ force: true });
    await page.locator('#tags-list .tag-chip').filter({ hasText: tag }).locator('.tag-remove').click();
    await expect(page.locator('#tags-list .tag-chip').filter({ hasText: tag })).toHaveCount(0);
  });

  test('playback progress is stored on the server', async ({ page }) => {
    const item = await mediaByName(page, 'sample-ogg.ogg');
    await openSet(page, 'test-audio');
    await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
    // The player posts its position every 3s while playing. /in-progress is
    // not usable here: it needs 60s of accumulated playback and the samples
    // are 12s long, so the saved position is read from the media detail.
    const saved = page.waitForResponse(res => res.request().method() === 'POST' && /\/api\/(v1\/)?progress$/.test(res.url()));
    expect((await playbackState(page, 'audio')).ok).toBe(true);
    expect((await saved).ok()).toBeTruthy();
    await expect.poll(() => page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.currentTime), { timeout: 15_000 }).toBeGreaterThan(6.5);
    await page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.pause());
    const detail = (await (await page.request.get(`/api/v1/media/${item.id}`)).json()) as { progress?: { position_seconds: number } };
    expect(detail.progress?.position_seconds ?? 0).toBeGreaterThan(2);

    // Reopening the item resumes from the saved position instead of 0.
    await openSet(page, 'test-audio');
    await card(page, item.file_name).locator('[data-action="play"]').click({ force: true });
    await expect.poll(() => page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.currentTime), { timeout: 10_000 }).toBeGreaterThan(2.5);
    const firstSeen = await page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.currentTime);
    expect(firstSeen, 'playback should resume near the saved position').toBeGreaterThan((detail.progress?.position_seconds ?? 0) - 0.5);
    const reset = await page.request.post('/api/v1/progress/status', { data: { media_id: item.id, status: 'not_started' } });
    expect(reset.ok()).toBeTruthy();
  });

  test('a share link works without a session and stops working when revoked', async ({ page, browser }) => {
    const item = await mediaByName(page, 'sample-mp4.mp4');
    await openSet(page, 'test-videos');
    await card(page, item.file_name).focus();
    await page.keyboard.press('s');
    await expect(page.locator('#toast')).toContainText(/Share link copied|Clipboard unavailable/);
    const shares = (await (await page.request.get(`/api/v1/media/${item.id}/shares`)).json()) as Array<{ token: string }>;
    expect(shares.length).toBeGreaterThan(0);
    const token = shares[shares.length - 1].token;

    const anonymous = await browser.newContext();
    const guest = await anonymous.newPage();
    const response = await guest.goto(`/s/${token}`);
    expect(response?.status()).toBe(200);
    await expect(guest).toHaveTitle(/sample-mp4/);
    const stream = await guest.request.get(`/s/${token}/stream`);
    expect(stream.status()).toBe(200);
    expect((await stream.body()).length).toBe(item.file_size_bytes);

    for (const share of shares) expect((await page.request.delete(`/api/v1/shares/${share.token}`)).ok()).toBeTruthy();
    expect((await guest.request.get(`/s/${token}/stream`)).status()).toBeGreaterThanOrEqual(400);
    await anonymous.close();
  });

  test('download delivers the original file', async ({ page }) => {
    const item = await mediaByName(page, 'sample-wav.wav');
    await openSet(page, 'test-audio');
    const downloading = page.waitForEvent('download');
    await card(page, item.file_name).locator('[data-action="download"]').click({ force: true });
    const download = await downloading;
    expect(download.suggestedFilename()).toBe(item.file_name);
    const { statSync } = await import('node:fs');
    expect(statSync(await download.path()).size).toBe(item.file_size_bytes);
  });

  test('upload writes a new file to the set and it can be trashed', async ({ page }) => {
    const name = `e2e-upload-${Date.now()}.png`;
    const source = await mediaByName(page, 'sample-png.png');
    const bytes = await (await page.request.get(`/api/v1/media/${source.id}/stream`)).body();
    await openSet(page, 'test-images');
    await page.keyboard.press('u');
    await expect(page.locator('#upload-modal')).toHaveClass(/open/);
    await page.locator('#upload-file').setInputFiles({ name, mimeType: 'image/png', buffer: bytes });
    await page.locator('#upload-submit').click();
    await expect(page.locator('#upload-modal')).not.toHaveClass(/open/, { timeout: 60_000 });
    await expect(card(page, name)).toBeVisible({ timeout: 30_000 });
    const uploaded = await mediaByName(page, name);
    expect(uploaded.file_size_bytes).toBe(bytes.length);
    expect((await page.request.get(`/api/v1/media/${uploaded.id}/thumbnail`)).status()).toBe(200);

    expect((await page.request.delete(`/api/v1/media/${uploaded.id}`)).ok()).toBeTruthy();
    const trash = (await (await page.request.get('/api/v1/admin/trash')).json()) as Media[];
    expect(trash.map(m => m.file_name)).toContain(name);
    expect((await allMedia(page)).map(m => m.file_name)).not.toContain(name);
  });

  test('admin panel opens and lists the accounts', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#admin-toggle')).not.toHaveClass(/hidden/);
    await revealHeader(page);
    await page.locator('#admin-toggle').click();
    await expect(page.locator('#admin-modal')).toHaveClass(/open/);
    await expect(page.locator('#admin-users')).toContainText(ADMIN.user);
    await expect(page.locator('#admin-users')).toContainText(USER.user);
  });
});

test.describe('permissions and logout', () => {
  test('a regular user sees only granted sets and no admin features', async ({ page, browser }) => {
    const adminContext = await browser.newContext();
    const adminPage = await adminContext.newPage();
    await login(adminPage, ADMIN);
    const users = (await (await adminPage.request.get('/api/v1/admin/users')).json()) as Array<{ id: number; username: string }>;
    const userId = users.find(u => u.username === USER.user)!.id;
    const sets = (await (await adminPage.request.get('/api/v1/sets')).json()) as Array<{ id: number; name: string }>;
    const images = sets.find(s => s.name === 'test-images')!;
    const grant = { set_id: images.id, user_id: userId };
    await adminPage.request.delete('/api/v1/admin/permissions', { data: grant });

    await login(page, USER);
    await expect(page.locator('#admin-toggle')).toHaveClass(/hidden/);
    expect((await page.request.get('/api/v1/admin/users')).status()).toBe(403);
    expect(((await (await page.request.get('/api/v1/sets')).json()) as unknown[] | null) || []).toHaveLength(0);

    expect((await adminPage.request.post('/api/v1/admin/permissions', { data: { ...grant, role: 'viewer' } })).ok()).toBeTruthy();
    await page.goto('/');
    await expect(page.locator('.set-card')).toHaveCount(1);
    await expect(page.locator('.set-card')).toContainText('test-images');
    await openSet(page, 'test-images');
    await card(page, 'sample-jpg.jpg').locator('[data-action="play"]').click({ force: true });
    await expect.poll(() => page.locator('#media-image').evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
    const video = (await allMedia(adminPage)).find(m => m.file_name === 'sample-mp4.mp4')!;
    expect((await page.request.get(`/api/v1/media/${video.id}/stream`)).status()).toBeGreaterThanOrEqual(400);

    expect((await adminPage.request.delete('/api/v1/admin/permissions', { data: grant })).ok()).toBeTruthy();
    await adminContext.close();
  });

  test('logout ends the session', async ({ page }) => {
    await login(page, ADMIN);
    await revealHeader(page);
    await page.locator('#logout-btn').click();
    await expect(page.locator('#login-form')).toBeVisible();
    await page.goto('/');
    await expect(page).toHaveURL(/\/login\.html/);
  });
});
