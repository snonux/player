import { initKeyboard } from '../keyboard.js';
import { enable, isOn, revision, toggle } from '../shuffle.js';
import { state } from '../state.js';
import { initMediaGrid, loadMedia } from '../views/media-grid.js';

const failures = [];
const requests = [];
let keydownHandler = null;

const button = {
  active: false,
  classList: {
    toggle(name, enabled) {
      if (name === 'active') button.active = enabled;
    },
  },
};
const grid = mockElement();
const emptyHint = mockElement();
const breadcrumb = mockElement();
const resultCount = mockElement();

globalThis.document = {
  activeElement: null,
  fullscreenElement: null,
  addEventListener(type, handler) {
    if (type === 'keydown') keydownHandler = handler;
  },
  getElementById(id) {
    if (id === 'shuffle-toggle') return button;
    if (id === 'media-grid') return grid;
    if (id === 'empty-hint') return emptyHint;
    if (id === 'breadcrumb-bar') return breadcrumb;
    if (id === 'result-count') return resultCount;
    return null;
  },
};
globalThis.location = { pathname: '/index.html', href: '' };
globalThis.fetch = async (url) => {
  requests.push(String(url));
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    json: async () => [],
  };
};

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

function mockElement() {
  return {
    classList: {
      add() {},
      remove() {},
      toggle() {},
      contains() { return false; },
    },
    innerHTML: '',
    textContent: '',
    addEventListener() {},
    querySelectorAll() { return []; },
    querySelector() { return null; },
  };
}

function testEnableTurnsShuffleOn() {
  enable();
  assert(isOn(), 'enable should turn shuffle on');
  assert(button.active, 'enable should update the shuffle button active state');
}

function testRepeatedEnableKeepsShuffleOnAndAdvancesRevision() {
  const before = revision();
  enable();
  assert(isOn(), 'repeated enable should keep shuffle on');
  assert(revision() > before, 'repeated enable should advance revision for a fresh random load');
}

function testToggleCanStillTurnShuffleOff() {
  toggle();
  assert(!isOn(), 'toggle should still turn shuffle off for the toolbar button');
  assert(!button.active, 'toggle should update the shuffle button inactive state');
}

async function testKeyboardReshuffleRequestsRandomMediaWithNewRevision() {
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.folderPath = '';
  state.mediaPage = 0;
  Object.assign(state.filters, {
    type: '',
    search: '',
    favorites: false,
    tags: '',
    sort: '',
    minDuration: '',
    maxDuration: '',
    minFileSize: '',
    maxFileSize: '',
  });
  requests.length = 0;

  initMediaGrid({
    isShuffle: isOn,
    shuffleRevision: revision,
  });
  initKeyboard({
    shuffle: () => {
      enable();
      loadMedia();
    },
  });

  pressKey('r');
  await waitForRequests(1);
  pressKey('r');
  await waitForRequests(2);

  const first = new URL(requests[0], 'http://player.test');
  const second = new URL(requests[1], 'http://player.test');
  assert(first.pathname === '/api/media', 'keyboard shuffle should request media');
  assert(first.searchParams.get('sort') === 'random', 'keyboard shuffle should request random sort');
  assert(second.searchParams.get('sort') === 'random', 'keyboard reshuffle should keep random sort');
  assert(first.searchParams.get('shuffle_revision'), 'keyboard shuffle should include shuffle revision');
  assert(second.searchParams.get('shuffle_revision'), 'keyboard reshuffle should include shuffle revision');
  assert(
    second.searchParams.get('shuffle_revision') !== first.searchParams.get('shuffle_revision'),
    'keyboard reshuffle should change shuffle revision in the media request',
  );
}

function pressKey(key) {
  keydownHandler?.({
    key,
    code: `Key${key.toUpperCase()}`,
    target: { tagName: 'BODY', isContentEditable: false },
    preventDefault() {},
  });
}

async function waitForRequests(count) {
  for (let i = 0; i < 10; i += 1) {
    if (requests.length >= count) return;
    await Promise.resolve();
  }
  assert(false, `expected ${count} request(s), got ${requests.length}`);
}

console.log('Running shuffle tests...');
testEnableTurnsShuffleOn();
testRepeatedEnableKeepsShuffleOnAndAdvancesRevision();
testToggleCanStillTurnShuffleOff();
await testKeyboardReshuffleRequestsRandomMediaWithNewRevision();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All shuffle tests passed.');
  process.exit(0);
}
