import { test, expect } from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';
const { genuineTouchTap } = require('./helpers/genuine-touch.cjs');

async function holdThumbnail(page, locator) {
  const box = await locator.boundingBox();
  const x = Math.round(box.x + box.width / 2), y = Math.round(box.y + box.height / 2);
  if (page.context().browser().browserType().name() === 'chromium') {
    const session = await page.context().newCDPSession(page);
    try {
      await session.send('Input.dispatchTouchEvent', {type:'touchStart', touchPoints:[{x,y,id:0}]});
      await page.waitForTimeout(650);
      await session.send('Input.dispatchTouchEvent', {type:'touchEnd', touchPoints:[]});
    } finally { await session.detach(); }
  } else {
    const delegate = page._connection?.toImpl?.(page)?.delegate;
    const session = delegate?._session;
    if (delegate?.constructor?.name !== 'BidiPage' || !session?.sessionId) throw new Error('Owned Firefox BiDi required');
    try {
      await session.send('input.performActions', {context:session.sessionId, actions:[{
        type:'pointer', id:'bulk_hold', parameters:{pointerType:'touch'}, actions:[
          {type:'pointerMove',origin:'viewport',x,y,duration:0}, {type:'pointerDown',button:0},
          {type:'pause',duration:650}, {type:'pointerUp',button:0}]}]});
    } finally { await session.send('input.releaseActions', {context:session.sessionId}); }
  }
}

// Production selection and global keyboard/touch handlers on an isolated page.
test('thumbnail hold, scrolling cancellation, menu selection and partial failure retry', async ({ page }) => {
  await page.route('http://selection.test/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname === '/') return route.fulfill({ contentType: 'text/html', body: '<main id="media-grid"></main>' });
    const root = path.resolve(__dirname, '../../../web');
    try { return route.fulfill({ contentType: pathname.endsWith('.css') ? 'text/css' : 'text/javascript', body: await fs.readFile(path.join(root, pathname.slice(1)), 'utf8') }); }
    catch { return route.fulfill({ status: 404, body: '' }); }
  });
  await page.goto('http://selection.test/');
  await page.evaluate(async () => {
    const { bindMediaMultiselect } = await import('/js/views/media-multiselect.js');
    const { initKeyboardAccess } = await import('/js/keyboard-access.js');
    const { initKeyboard } = await import('/js/keyboard.js');
    const selection = await import('/js/selection.js');
    const { API } = await import('/js/api.js');
    const { state } = await import('/js/state.js');
    (window as any).calls = [];
    (window as any).reject = true;
    API.progressStatus = async (id: string) => {
      if (id === '2' && (window as any).reject) throw new Error('Forbidden');
      (window as any).calls.push(id);
    };
    state.media = [{id:1}, {id:2}];
    const grid = document.querySelector('#media-grid')!;
    grid.innerHTML = [1,2].map(id => `<div class="media-card" data-id="${id}" aria-label="Image ${id}" style="position:relative;width:220px;height:160px"><button class="media-open" style="width:140px;height:80px">Image ${id}</button><div class="card-actions"></div></div>`).join('');
    // Keep the production document-capture registration order.
    initKeyboardAccess();
    initKeyboard({navRight: selection.navRight, navLeft: selection.navLeft,
      navDown: selection.navDown, navUp: selection.navUp,
      enter: () => { throw new Error('Selection Enter must not open playback'); }});
    selection.initSelection();
    bindMediaMultiselect(grid);
    (window as any).trustedTouches = [];
    grid.addEventListener('pointerdown', event => {
      if ((event as PointerEvent).pointerType === 'touch') (window as any).trustedTouches.push(event.isTrusted);
    });
  });
  const first = page.locator('[data-id="1"] .media-open');
  const second = page.locator('[data-id="2"] .media-open');
  await first.hover();
  await page.mouse.down();
  await page.mouse.move(400, 400);
  await page.waitForTimeout(650);
  await page.mouse.up();
  await expect(page.locator('.media-selection')).toBeHidden();
  await holdThumbnail(page, first);
  await expect(page.locator('[popover]:popover-open')).toHaveCount(0);
  await expect(page.locator('[data-count]')).toHaveText('1 selected');
  await first.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('[data-id="2"]')).toHaveClass(/selected/);
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-count]')).toHaveText('2 selected');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-count]')).toHaveText('1 selected');
  await genuineTouchTap(page, second);
  await expect(page.locator('[popover]:popover-open')).toHaveCount(0);
  await expect(page.locator('[data-count]')).toHaveText('2 selected');
  await page.locator('[data-operation]').selectOption('finished');
  await page.getByRole('button', {name:'Apply',exact:true}).click();
  await expect(page.locator('[data-count]')).toHaveText('1 selected');
  await expect.poll(() => page.evaluate(() => (window as any).calls)).toEqual(['1']);
  await page.evaluate(() => { (window as any).reject = false; });
  await page.getByRole('button', {name:'Apply',exact:true}).click();
  await expect(page.locator('[data-count]')).toHaveText('0 selected');
  await expect.poll(() => page.evaluate(() => (window as any).calls)).toEqual(['1','2']);
  await page.getByRole('button', {name:'Cancel selection'}).click();
  await expect(page.locator('.media-selection')).toBeHidden();
  await page.locator('[data-id="1"] [data-action="select-items"]').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-count]')).toHaveText('1 selected');
  await first.focus();
  await page.keyboard.press('Escape');
  await expect(page.locator('.media-selection')).toBeHidden();
  await expect.poll(() => page.evaluate(() => (window as any).trustedTouches)).toEqual([true, true]);
  // Outside selection, ordinary touch retains the existing More menu path.
  await genuineTouchTap(page, first);
  await expect(page.locator('[popover]:popover-open')).toHaveCount(1);
});
