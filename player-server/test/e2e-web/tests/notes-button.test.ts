import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

let adminCookie: string;
let setId: number;

test.beforeAll(async () => {
  test.setTimeout(70_000);
  await waitForServer(15_000);
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie, 30_000);

  // The scanner may still be running after the first set appears.
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
    const sets = await (await fetch(`${baseURL}/api/sets`, { headers: { Cookie: adminCookie } })).json() as Array<{ id: number }>;
    for (const set of sets || []) {
      const browse = await (await fetch(`${baseURL}/api/sets/${set.id}/browse`, { headers: { Cookie: adminCookie } })).json() as { media?: Array<{ type: string }> };
      if ((browse.media || []).filter(media => media.type === 'image').length >= 2) {
        setId = set.id;
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need a set with at least two media items for notes button tests');
});

async function openMedia(page: Page, view: 'browse' | 'filtered') {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.locator(`.set-row[data-id="${setId}"]`).waitFor({ state: 'attached' });
  await page.evaluate(async ({ id, filtered }) => {
    const setsPath = '/js/views/sets.js';
    const { selectSet } = await import(setsPath);
    if (filtered) {
      const statePath = '/js/state.js';
      const { state } = await import(statePath);
      state.filters.type = 'image';
    }
    selectSet(id);
  }, { id: setId, filtered: view === 'filtered' });
  await expect.poll(() => page.locator('#media-grid .media-card').count()).toBeGreaterThanOrEqual(2);
}

// Both active render paths use media cards. The legacy .media-row selector has
// no renderer or list-mode toggle in the current UI.
for (const view of ['browse', 'filtered'] as const) {
  for (const priorSelection of [false, true]) {
    test(`${view} notes button opens and saves clicked item ${priorSelection ? 'after another selection' : 'without a selection'}`, async ({ page }) => {
      await openMedia(page, view);
      const cards = page.locator('#media-grid .media-card');
      const firstId = await cards.nth(0).getAttribute('data-id');
      const secondId = await cards.nth(1).getAttribute('data-id');
      expect(firstId).toBeTruthy();
      expect(secondId).toBeTruthy();

      const target = priorSelection ? cards.nth(1) : cards.nth(0);
      const targetId = priorSelection ? secondId : firstId;
      if (priorSelection) await cards.nth(0).click();

      const noteRequests: string[] = [];
      await page.route(/\/api\/media\/\d+\/notes$/, async route => {
        const id = new URL(route.request().url()).pathname.split('/')[3];
        noteRequests.push(`${route.request().method()} ${id}`);
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ content: `note for ${id}` }) });
      });

      await target.locator('[data-action="notes"]').click({ force: true });
      await expect(page.locator('#notes-modal')).toHaveClass(/open/);
      await expect(page.locator('#notes-textarea')).toHaveValue(`note for ${targetId}`);
      await expect(target).toHaveClass(/selected/);
      await page.locator('#notes-textarea').fill('updated note');
      await page.locator('#notes-save').click();
      await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
      expect(noteRequests).toEqual([`GET ${targetId}`, `POST ${targetId}`]);
    });
  }
}

test('failed note load does not expose an empty editor or overwrite the original on retry', async ({ page }) => {
  await openMedia(page, 'browse');
  const noteButton = page.locator('#media-grid .media-card').first().locator('[data-action="notes"]');
  let failLoad = true;
  const writes: string[] = [];
  await page.route(/\/api\/media\/\d+\/notes$/, async route => {
    const request = route.request();
    if (request.method() === 'GET' && failLoad) {
      await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"read failed"}' });
      return;
    }
    if (request.method() === 'POST') {
      writes.push((request.postDataJSON() as { content: string }).content);
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"content":"Original note"}' });
  });

  await noteButton.click({ force: true });
  await expect(page.locator('#toast')).toContainText('Could not load note. Try again.');
  await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
  expect(writes).toEqual([]);

  failLoad = false;
  await noteButton.click({ force: true });
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(page.locator('#notes-textarea')).toHaveValue('Original note');
  await page.locator('#notes-save').click();
  expect(writes).toEqual(['Original note']);
});

test('a confirmed missing note opens an empty editor', async ({ page }) => {
  await openMedia(page, 'browse');
  await page.route(/\/api\/media\/\d+\/notes$/, route =>
    route.fulfill({ status: 204, body: '' }));
  await page.locator('#media-grid .media-card').first().locator('[data-action="notes"]').click({ force: true });
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(page.locator('#notes-textarea')).toHaveValue('');
});

test('a malformed 200 null note does not open an empty editor', async ({ page }) => {
  await openMedia(page, 'browse');
  await page.route(/\/api\/media\/\d+\/notes$/, route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: 'null' }));
  await page.locator('#media-grid .media-card').first().locator('[data-action="notes"]').click({ force: true });
  await expect(page.locator('#toast')).toContainText('Could not load note. Try again.');
  await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
});

test('an older note load cannot replace a newer dirty note', async ({ page }) => {
  await openMedia(page, 'browse');
  const cards = page.locator('#media-grid .media-card');
  const firstId = await cards.nth(0).getAttribute('data-id');
  const secondId = await cards.nth(1).getAttribute('data-id');
  expect(firstId).toBeTruthy();
  expect(secondId).toBeTruthy();
  let firstStarted!: () => void;
  const started = new Promise<void>(resolve => { firstStarted = resolve; });
  let releaseFirst!: () => void;
  const firstResponse = new Promise<void>(resolve => { releaseFirst = resolve; });
  await page.route(/\/api\/media\/\d+\/notes$/, async route => {
    const id = new URL(route.request().url()).pathname.split('/')[3];
    if (id === firstId) {
      firstStarted();
      await firstResponse;
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ content: `Saved ${id}` }) });
  });
  let prompts = 0;
  page.on('dialog', async dialog => {
    prompts += 1;
    await dialog.dismiss();
  });

  await cards.nth(0).locator('[data-action="notes"]').click({ force: true });
  await started;
  await cards.nth(1).locator('[data-action="notes"]').click({ force: true });
  await expect(page.locator('#notes-textarea')).toHaveValue(`Saved ${secondId}`);
  await page.locator('#notes-textarea').fill('Second draft');
  releaseFirst();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(page.locator('#notes-textarea')).toHaveValue('Second draft');
  expect(prompts).toBe(0);
});

test('unsaved notes require explicit discard; save and delete close without a prompt', async ({ page }) => {
  await openMedia(page, 'browse');
  const noteButton = page.locator('#media-grid .media-card').first().locator('[data-action="notes"]');
  let storedContent = 'Saved note';
  const methods: string[] = [];
  await page.route(/\/api\/media\/\d+\/notes$/, async route => {
    const request = route.request();
    methods.push(request.method());
    if (request.method() === 'POST') {
      storedContent = (request.postDataJSON() as { content: string }).content;
    } else if (request.method() === 'DELETE') {
      storedContent = '';
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ content: storedContent }) });
  });

  const modal = page.locator('#notes-modal');
  const area = page.locator('#notes-textarea');
  const prompts: string[] = [];
  let discard = false;
  page.on('dialog', async dialog => {
    prompts.push(dialog.message());
    if (discard) await dialog.accept();
    else await dialog.dismiss();
  });

  await noteButton.click({ force: true });
  await expect(area).toHaveValue('Saved note');
  await area.fill('Unsaved draft');
  await page.locator('#notes-close').click();
  await expect(modal).toHaveClass(/open/);
  await expect(area).toHaveValue('Unsaved draft');
  await modal.click({ position: { x: 5, y: 5 } });
  await expect(modal).toHaveClass(/open/);
  await area.press('Escape');
  await expect(modal).toHaveClass(/open/);
  expect(prompts).toHaveLength(3);

  discard = true;
  await modal.click({ position: { x: 5, y: 5 } });
  await expect(modal).toBeHidden();
  await noteButton.dispatchEvent('click');
  await expect(modal).toHaveClass(/open/);
  await expect(area).toHaveValue('Saved note');
  await area.fill('Updated note');
  await page.locator('#notes-save').click();
  await expect(modal).toBeHidden();
  await noteButton.dispatchEvent('click');
  await expect(modal).toHaveClass(/open/);
  await expect(area).toHaveValue('Updated note');
  await page.locator('#notes-delete').click();
  await expect(modal).toBeHidden();
  await noteButton.dispatchEvent('click');
  await expect(modal).toHaveClass(/open/);
  await expect(area).toHaveValue('');
  expect(methods).toEqual(['GET', 'GET', 'POST', 'GET', 'DELETE', 'GET']);
  expect(prompts).toHaveLength(4);
});

for (const operation of ['save', 'delete'] as const) {
  test(`late ${operation} response leaves a newer note draft open`, async ({ page }) => {
    await openMedia(page, 'browse');
    const method = operation === 'save' ? 'POST' : 'DELETE';
    let requestStarted!: () => void;
    const started = new Promise<void>(resolve => { requestStarted = resolve; });
    let releaseResponse!: () => void;
    const responseGate = new Promise<void>(resolve => { releaseResponse = resolve; });
    await page.route(/\/api\/media\/1\/notes$/, async route => {
      if (route.request().method() === method) {
        requestStarted();
        await responseGate;
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{"content":"First saved"}' });
    });
    const openNote = (id: string, content: string) => page.evaluate(async args => {
      const modulePath = '/js/notes.js';
      const notes = await import(modulePath);
      notes.open(args.id, args.content);
    }, { id, content });
    let discard = false;
    let prompts = 0;
    page.on('dialog', async dialog => {
      prompts += 1;
      if (discard) await dialog.accept();
      else await dialog.dismiss();
    });

    await openNote('1', 'First saved');
    const modal = page.locator('#notes-modal');
    const area = page.locator('#notes-textarea');
    await area.fill('First edited');
    await page.locator(`#notes-${operation}`).click();
    await started;

    await openNote('1', 'Stale server content');
    await expect(area).toHaveValue('First edited');
    expect(prompts).toBe(0);
    await openNote('2', 'Second saved');
    await expect(area).toHaveValue('First edited');
    expect(prompts).toBe(1);
    discard = true;
    await openNote('2', 'Second saved');
    await expect(area).toHaveValue('Second saved');
    await area.fill('Second draft');

    releaseResponse();
    await expect.poll(() => page.locator('#toast').textContent()).toContain('Note saved');
    await expect(modal).toHaveClass(/open/);
    await expect(area).toHaveValue('Second draft');
  });
}

test('a second note write waits for the first response', async ({ page }) => {
  await openMedia(page, 'browse');
  let releaseFirst!: () => void;
  const firstResponse = new Promise<void>(resolve => { releaseFirst = resolve; });
  let firstStarted!: () => void;
  const started = new Promise<void>(resolve => { firstStarted = resolve; });
  const saved: string[] = [];
  let deletes = 0;
  await page.route(/\/api\/media\/1\/notes$/, async route => {
    const request = route.request();
    if (request.method() === 'POST') {
      saved.push((request.postDataJSON() as { content: string }).content);
      if (saved.length === 1) {
        firstStarted();
        await firstResponse;
      }
    } else if (request.method() === 'DELETE') {
      deletes += 1;
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });
  await page.evaluate(async () => {
    const modulePath = '/js/notes.js';
    (await import(modulePath)).open('1', 'Original');
  });
  const area = page.locator('#notes-textarea');
  const save = page.locator('#notes-save');
  const del = page.locator('#notes-delete');
  await area.fill('A');
  await save.click();
  await started;
  await area.fill('B');
  await expect(save).toBeDisabled();
  await expect(del).toBeDisabled();
  await save.dispatchEvent('click');
  await del.dispatchEvent('click');
  expect(saved).toEqual(['A']);
  expect(deletes).toBe(0);

  releaseFirst();
  await expect(save).toBeEnabled();
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(area).toHaveValue('B');
  await save.click();
  await expect(page.locator('#notes-modal')).toBeHidden();
  expect(saved).toEqual(['A', 'B']);
  expect(deletes).toBe(0);
});
