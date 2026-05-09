import { initKeyboard } from '../keyboard.js';
import { renderScanProgress, triggerRescan } from '../views/admin-status.js';
import { readFileSync } from 'node:fs';

const failures = [];
const requests = [];
let keydownHandler = null;
let rescanStatus = 200;

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

const indicator = mockElement();
const indicatorText = mockElement();
const toastEl = {
  className: '',
  textContent: '',
  classList: { remove() {} },
};

globalThis.document = {
  addEventListener(type, handler) {
    if (type === 'keydown') keydownHandler = handler;
  },
  getElementById(id) {
    if (id === 'scan-indicator') return indicator;
    if (id === 'scan-indicator-text') return indicatorText;
    if (id === 'toast') return toastEl;
    return null;
  },
};
globalThis.location = { pathname: '/', href: '' };
globalThis.setTimeout = () => 0;

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url: String(url), method: options.method || 'GET' });
  if (String(url) === '/api/admin/rescan') {
    if (rescanStatus !== 200) {
      return jsonResponse({ error: 'admin only' }, rescanStatus);
    }
    return jsonResponse({ status: 'ok' });
  }
  if (String(url) === '/api/admin/scan-progress') {
    return jsonResponse({
      running: true,
      current_set: 'movies',
      sets_total: 2,
      sets_done: 1,
      files_total: 9,
      files_done: 4,
    });
  }
  return jsonResponse({});
};

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function mockElement() {
  return {
    textContent: '',
    hidden: true,
    classList: {
      add(name) {
        if (name === 'hidden') this.hidden = true;
      },
      remove(name) {
        if (name === 'hidden') this.hidden = false;
      },
      hidden: true,
    },
  };
}

function pressKey(key) {
  let prevented = false;
  keydownHandler?.({
    key,
    code: `Key${key.toUpperCase()}`,
    target: { tagName: 'BODY', isContentEditable: false },
    preventDefault() { prevented = true; },
  });
  return prevented;
}

function testKeyboardRescanHandler() {
  let rescans = 0;
  initKeyboard({ rescanMedia: () => { rescans += 1; } });

  const prevented = pressKey('M');

  assert(prevented, 'M should prevent default browser handling');
  assert(rescans === 1, 'M should call the rescan handler');
}

function testRenderRunningProgress() {
  renderScanProgress({
    running: true,
    current_set: 'music',
    sets_total: 3,
    sets_done: 2,
    files_total: 20,
    files_done: 7,
  });

  assert(indicator.classList.hidden === false, 'running progress should show the indicator');
  assert(indicatorText.textContent === 'Scanning music 2/3 sets, 7/20 files', 'running progress text should include set and file counts');
}

function testRenderIdleProgressHidesIndicator() {
  renderScanProgress({ running: false });

  assert(indicator.classList.hidden === true, 'idle progress should hide the indicator');
}

function testScanIndicatorOutsideHeader() {
  const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
  const header = html.slice(html.indexOf('<header'), html.indexOf('</header>'));
  const afterHeader = html.slice(html.indexOf('</header>'));

  assert(!header.includes('id="scan-indicator"'), 'scan indicator should not be inside the collapsible header');
  assert(afterHeader.includes('id="scan-indicator"'), 'scan indicator should remain in the document after the header');
}

function testScanIndicatorAboveModalOverlay() {
  const css = readFileSync(new URL('../../css/layout.css', import.meta.url), 'utf8');
  const scanRule = css.match(/\.scan-indicator\s*\{[^}]*z-index:\s*(\d+)/);
  const modalRule = css.match(/\.modal-overlay\s*\{[^}]*z-index:\s*(\d+)/);

  assert(scanRule, 'scan indicator should define a z-index');
  assert(modalRule, 'modal overlay should define a z-index');
  assert(Number(scanRule?.[1]) > Number(modalRule?.[1]), 'scan indicator should render above open admin modal overlay');
}

async function testTriggerRescanRefreshesProgress() {
  requests.length = 0;
  rescanStatus = 200;
  await triggerRescan();

  assert(requests[0]?.url === '/api/admin/rescan', 'trigger should call rescan endpoint first');
  assert(requests[0]?.method === 'POST', 'trigger should post to rescan endpoint');
  assert(requests[1]?.url === '/api/admin/scan-progress', 'trigger should refresh progress after starting scan');
  assert(indicator.classList.hidden === false, 'trigger should show refreshed running progress');
}

async function testTriggerRescanErrorDoesNotPollProgress() {
  requests.length = 0;
  toastEl.textContent = '';
  rescanStatus = 403;

  await triggerRescan();

  assert(requests.length === 1, 'failed trigger should not poll scan progress');
  assert(toastEl.textContent === 'admin only', 'failed trigger should show the API error');
}

console.log('Running admin rescan frontend tests...');
testKeyboardRescanHandler();
testRenderRunningProgress();
testRenderIdleProgressHidesIndicator();
testScanIndicatorOutsideHeader();
testScanIndicatorAboveModalOverlay();
await testTriggerRescanRefreshesProgress();
await testTriggerRescanErrorDoesNotPollProgress();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All admin rescan frontend tests passed.');
  process.exit(0);
}
