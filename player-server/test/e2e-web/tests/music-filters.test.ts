import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin } from './helpers/music-ui';
import { settleMusicViewport } from './helpers/music-viewport';
const { completedFilterResult } = require('./helpers/file-filter-oracle.cjs');

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires marked F03-augmented disposable fixture');
let fixture: MusicFixture, evidence: Evidence;
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!fixture.mediaIds['Music/F03/f03-a.wav']) throw new Error('Run music_filters.py on this disposable fixture first');
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await keyboardLogin(page, fixture.user);
  await page.goto(`${fixture.origin}/?set=${fixture.setIds.Music}`);
});
function ids(names: string[], other = false) {
  return names.map(name => fixture.mediaIds[`${other ? 'F03 Other' : 'Music'}/F03/${name}`]).sort((a,b)=>a-b);
}
function mediaResponse(page: Page) {
  return page.waitForResponse(response => {
    const url = new URL(response.url());
    return /^\/api(?:\/v1)?\/media$/.test(url.pathname) &&
      url.searchParams.get('search') === 'f03-' && response.request().method() === 'GET';
  });
}
async function rows(page: Page, expected: number[], freshResponse = expected.length > 0) {
  await expect.poll(async () => {
    const state = await page.locator('#media-grid').evaluate(grid => ({
      ids: Array.from(grid.querySelectorAll<HTMLElement>('[data-id]')).map(node => Number(node.dataset.id)),
      loading: Array.from(grid.querySelectorAll('p')).some(node => node.textContent?.trim() === 'Loading...'),
      error: Boolean(grid.querySelector('.error-message')),
      emptyText: Array.from(grid.querySelectorAll('p')).map(node => node.textContent?.trim()).find(text => text === 'No results.') || '',
      resultCount: document.getElementById('result-count')?.textContent?.trim() || '',
    }));
    return completedFilterResult(state, expected, freshResponse);
  }).toBe(true);
}
async function query(page: Page, syntax: string, expected: number[]) {
  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
  const response = mediaResponse(page); // Subscribe before typing/Enter starts this query.
  await page.locator('#search-input').fill(`sort:name f03- ${syntax}`);
  await page.keyboard.press('Enter');
  expect((await response).ok()).toBe(true);
  await rows(page, expected, true);
}

test('F03 existing Sets filters individually, inclusive zero, AND combination, reload and Clear', async ({page}) => {
  const all = ids(['f03-a.wav','f03-b.wav','f03-c.wav','f03-zero.jpg']);
  const cases: [string, string[]][] = [
    ['', ['f03-a.wav','f03-b.wav','f03-c.wav','f03-zero.jpg']],
    ['tag:"F03 keep"', ['f03-a.wav','f03-b.wav','f03-zero.jpg']],
    ['tag:"F03 calm"', ['f03-a.wav','f03-c.wav','f03-zero.jpg']],
    ['tag:"F03 keep,F03 calm"', ['f03-a.wav','f03-zero.jpg']],
    ['min:1', ['f03-a.wav','f03-b.wav','f03-c.wav']],
    ['max:1', ['f03-a.wav','f03-c.wav','f03-zero.jpg']],
    ['min:1 max:1', ['f03-a.wav','f03-c.wav']],
    ['min:0 max:0', ['f03-zero.jpg']],
    ['minsize:1', ['f03-a.wav','f03-b.wav','f03-c.wav','f03-zero.jpg']],
    ['maxsize:1', ['f03-a.wav','f03-zero.jpg']],
    ['minsize:2 maxsize:2', ['f03-b.wav','f03-c.wav']],
    ['minsize:0 maxsize:0', []],
    ['type:audio', ['f03-a.wav','f03-b.wav','f03-c.wav']],
    ['type:image', ['f03-zero.jpg']],
    ['type:video', []],
    ['like:1', ['f03-a.wav']],
    ['type:audio like:1 tag:"F03 keep,F03 calm" min:1 max:1 minsize:1 maxsize:1', ['f03-a.wav']],
  ];
  try {
    for (const [syntax,names] of cases) await query(page, syntax, ids(names));
    await page.reload(); await rows(page, ids(['f03-a.wav']));
    await page.goBack(); // Same restored filter route stays usable after browser history movement.
    await query(page, 'set:"F03 Other"', ids(['f03-other.wav'], true));
    await query(page, 'set:Music', all);
    // Existing native sidebar selection adds the second set to the union.
    await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
    const other = page.locator(`#set-list .set-row[data-id="${fixture.setIds['F03 Other']}"]`);
    await other.focus(); await page.keyboard.press(' ');
    await rows(page, [...all,...ids(['f03-other.wav'], true)]);
    // No restricted contribution leaks to the viewer.
    expect(await page.locator(`[data-id="${fixture.mediaIds['Restricted/F03/f03-secret.wav']}"]`).count()).toBe(0);
    for (const sort of ['name','date','duration','play_count','random']) {
      await query(page, `sort:${sort}`, [...all,...ids(['f03-other.wav'], true)]);
    }
    await page.keyboard.press('/'); await page.locator('#search-input').fill('r/h/l/?');
    await expect(page.locator('#search-input')).toHaveValue('r/h/l/?');
    await page.locator('#search-clear').click();
    await page.waitForTimeout(450);
    await expect(page.locator('#search-input')).toHaveValue('');
    const filterKeys = ['search','type','favorites','tags','sort','minDuration','maxDuration','minFileSize','maxFileSize'];
    await expect.poll(()=>page.evaluate(keys => {
      const params = new URL(location.href).searchParams;
      return keys.filter(key => params.has(key));
    }, filterKeys)).toEqual([]);
    const retainedScope = [fixture.setIds.Music, fixture.setIds['F03 Other']];
    await expect.poll(()=>page.evaluate(()=>new URL(location.href).searchParams.get('set')))
      .toBe(retainedScope.join(','));
    const unfilteredResponse = await page.request.get(`${fixture.origin}/api/v1/media`, {
      params: {set_ids: retainedScope.join(','), sort: 'name', limit: '1000'},
    });
    expect(unfilteredResponse.ok()).toBe(true);
    const unfiltered = await unfilteredResponse.json();
    await rows(page, unfiltered.map((item: {id:number}) => item.id));
    evidence.record({feature:'F03',scenario:'all file filters, exact bounds/zero, conjunction, set union, URL reload and Clear',result:'pass',data:{cases:cases.map(c=>c[0])}});
  } catch(error) { evidence.record({feature:'F03',scenario:'filter parity',result:'fail',detail:String(error)});throw error; }
});

async function withMusicOwner(action: () => Promise<void>) {
  const admin = await request.newContext({baseURL: fixture.origin});
  let original: {set_id:number; user_id:number; role:string} | undefined;
  let target: {set_id:number; user_id:number} | undefined;
  let granted = false;
  try {
    expect((await admin.post('/api/v1/auth/login', {data: fixture.admin})).ok()).toBe(true);
    const matrixResponse = await admin.get('/api/admin/permissions');
    expect(matrixResponse.ok()).toBe(true);
    const matrix = await matrixResponse.json();
    const user = matrix.users.find((item: {username:string}) => item.username === fixture.user.username);
    expect(user).toBeTruthy();
    target = {set_id: fixture.setIds.Music, user_id: user.id};
    original = matrix.permissions.find((item: {set_id:number; user_id:number}) =>
      item.set_id === target!.set_id && item.user_id === target!.user_id);
    expect(original?.role).toBe('viewer');
    granted = true; // Restore even if the grant response is lost after the server applies it.
    expect((await admin.post('/api/admin/permissions', {data: {...target, role:'owner'}})).ok()).toBe(true);
    await action();
  } finally {
    try {
      if (granted && target) {
        const restored = original
          ? await admin.post('/api/admin/permissions', {data: {...target, role:original.role}})
          : await admin.delete('/api/admin/permissions', {data:target});
        expect(restored.ok()).toBe(true);
        const matrix = await (await admin.get('/api/admin/permissions')).json();
        const row = matrix.permissions.find((item: {set_id:number; user_id:number}) =>
          item.set_id === target!.set_id && item.user_id === target!.user_id);
        expect(row?.role).toBe(original?.role);
      }
    } finally { await admin.dispose(); }
  }
}

test('F03 tag editing/focus, light-dark narrow, no-match and real offline retry', async ({page,context}) => {
  const a=ids(['f03-a.wav']);
  try {
    await query(page, 'tag:"F03 keep,F03 calm" type:audio', a);
    const card=page.locator(`[data-id="${a[0]}"]`);
    await card.focus(); await page.keyboard.press('t');
    await expect(page.locator('#tags-new')).toBeFocused();
    const forbidden = page.waitForResponse(response =>
      new URL(response.url()).pathname.endsWith(`/media/${a[0]}/tags`) && response.request().method() === 'POST');
    await page.locator('#tags-new').fill('F03 forbidden viewer edit');await page.keyboard.press('Enter');
    expect((await forbidden).status()).toBe(403);
    await expect(page.locator('#tags-list')).not.toContainText('F03 forbidden viewer edit');
    const unchanged = await (await page.request.get(`${fixture.origin}/api/v1/media/${a[0]}`)).json();
    expect(unchanged.tags.map((tag: {name:string}) => tag.name)).not.toContain('F03 forbidden viewer edit');
    await page.keyboard.press('Escape');await expect(card).toBeFocused();
    await withMusicOwner(async () => {
      await card.focus();await page.keyboard.press('t');
      await expect(page.locator('#tags-new')).toBeFocused();
      await expect(page.locator('#tags-list')).not.toContainText('F03 edited');
      const beforeEdit = await (await page.request.get(`${fixture.origin}/api/v1/media/${a[0]}`)).json();
      expect(beforeEdit.tags.map((tag: {name:string}) => tag.name)).not.toContain('F03 edited');
      await page.locator('#tags-new').fill('F03 edited');await page.keyboard.press('Enter');
      await expect(page.locator('#tags-list')).toContainText('F03 edited');
      await page.keyboard.press('Escape');await expect(card).toBeFocused();
      await query(page,'tag:"F03 edited"',a);
      await card.focus();await page.keyboard.press('t');
      const removedResponse = mediaResponse(page);
      await page.locator('#tags-list button[data-tag="F03 edited"]').click();
      expect((await removedResponse).ok()).toBe(true);
      await page.keyboard.press('Escape');await rows(page,[],true);
    });
    await query(page,'',ids(['f03-a.wav','f03-b.wav','f03-c.wav','f03-zero.jpg']));
    for (const [theme,width] of [['light',390],['dark',1366]] as const) {
      await settleMusicViewport(page,width);
      if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
      await query(page,'max:1 maxsize:1',ids(['f03-a.wav','f03-zero.jpg']));
      const path=evidence.path(`f03-${theme}-${width}.png`);await page.screenshot({path,fullPage:true});
      evidence.record({feature:'F03',scenario:`${theme} ${width} existing file controls`,result:'pass',artifacts:[path]});
    }
    await context.setOffline(true);
    await page.keyboard.press('/');await page.locator('#search-input').fill('type:audio f03- max:1');await page.keyboard.press('Enter');
    await expect(page.locator('#media-grid .error-message')).toBeVisible();
    await context.setOffline(false);
    await query(page,'type:audio max:1',ids(['f03-a.wav','f03-c.wav']));
    evidence.record({feature:'F03',scenario:'viewer tag edit denied 403; temporary Music owner add/remove preserves filtering/focus, original role restored, themes/offline recovery',result:'pass'});
  } catch(error) { evidence.record({feature:'F03',scenario:'editing and boundaries',result:'fail',detail:String(error)});throw error; }
  finally { await context.setOffline(false); }
});
