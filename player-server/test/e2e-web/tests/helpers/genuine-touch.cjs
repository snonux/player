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
