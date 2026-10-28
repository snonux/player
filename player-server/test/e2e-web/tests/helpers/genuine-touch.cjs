// Installed Playwright's WebDriver BiDi RawTouchscreen.tap is empty. Use the
// existing owned Firefox session's standard input actions, never DOM dispatch.
async function boundedSend(session, method, params) {
  let timer;
  try {
    return await Promise.race([
      session.send(method, params),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`BiDi ${method} timed out`)), 10000); }),
    ]);
  } finally { clearTimeout(timer); }
}

async function genuineTouchTap(page, locator) {
  const browser = page.context().browser()?.browserType().name();
  if (browser === 'chromium') return locator.tap();
  if (browser !== 'firefox') throw new Error('Unknown browser touch transport; no input fallback permitted');
  // Preserve Playwright's visibility, stability, enabled and hit-target checks.
  await locator.tap({trial: true});
  const implementation = page._connection?.toImpl?.(page);
  const delegate = implementation?.delegate;
  const session = delegate?._session;
  if (delegate?.constructor?.name !== 'BidiPage' || typeof session?.send !== 'function' || typeof session.sessionId !== 'string' || !session.sessionId) {
    throw new Error('Owned Firefox BiDi touch transport unavailable; no input fallback permitted');
  }
  const box = await locator.boundingBox();
  if (!box || box.width <= 0 || box.height <= 0) throw new Error('Touch target has no visible bounds');
  const context = session.sessionId;
  const actions = [{type: 'pointer', id: 'player_m07_touch', parameters: {pointerType: 'touch'}, actions: [
    {type: 'pointerMove', origin: 'viewport', x: Math.round(box.x + box.width / 2), y: Math.round(box.y + box.height / 2), duration: 0},
    {type: 'pointerDown', button: 0}, {type: 'pause', duration: 60}, {type: 'pointerUp', button: 0},
  ]}];
  let failure;
  try { await boundedSend(session, 'input.performActions', {context, actions}); }
  catch (error) { failure = error; }
  try { await boundedSend(session, 'input.releaseActions', {context}); }
  catch (error) {
    if (failure) throw new AggregateError([failure, error], 'BiDi touch and release failed');
    throw error;
  }
  if (failure) throw failure;
}

module.exports = {genuineTouchTap};

// Paths contain equally many viewport points, one path per finger. Both
// transports send browser-owned input, so production listeners see trusted
// touch events. Never replace this with dispatchEvent or mouse emulation.
async function genuineTouchPaths(page, paths) {
  if (!paths.length || paths.some(path => path.length < 2 || path.length !== paths[0].length)) throw new Error('Touch paths must have matching start and end points');
  const browser = page.context().browser()?.browserType().name();
  if (browser === 'chromium') {
    const session = await page.context().newCDPSession(page);
    const points = index => paths.map((path, id) => ({ x: path[index].x, y: path[index].y, id, radiusX: 1, radiusY: 1, force: 1 }));
    try {
      await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: points(0) });
      for (let index = 1; index < paths[0].length; index++) await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: points(index) });
      await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    } finally { await session.detach(); }
    return;
  }
  if (browser !== 'firefox') throw new Error('Unknown browser touch transport; no input fallback permitted');
  const delegate = page._connection?.toImpl?.(page)?.delegate;
  const session = delegate?._session;
  if (delegate?.constructor?.name !== 'BidiPage' || typeof session?.send !== 'function' || !session.sessionId) throw new Error('Owned Firefox BiDi touch transport unavailable; no input fallback permitted');
  const context = session.sessionId;
  const actions = paths.map((path, id) => ({ type: 'pointer', id: `player_image_touch_${id}`, parameters: { pointerType: 'touch' }, actions: [
    { type: 'pointerMove', origin: 'viewport', x: Math.round(path[0].x), y: Math.round(path[0].y), duration: 0 },
    { type: 'pointerDown', button: 0 },
    ...path.slice(1).map(point => ({ type: 'pointerMove', origin: 'viewport', x: Math.round(point.x), y: Math.round(point.y), duration: 40 })),
    { type: 'pointerUp', button: 0 },
  ] }));
  try { await boundedSend(session, 'input.performActions', { context, actions }); }
  finally { await boundedSend(session, 'input.releaseActions', { context }); }
}
module.exports.genuineTouchPaths = genuineTouchPaths;
