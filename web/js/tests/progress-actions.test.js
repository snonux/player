import { API } from '../api.js';
import { state } from '../state.js';
import { initMediaGrid, loadMedia } from '../views/media-grid.js';

const failures = [];
const requests = [];

const grid = mockElement();
const emptyHint = mockElement();
const breadcrumb = mockElement();
const resultCount = mockElement();

globalThis.location = { pathname: '/index.html', href: '' };
globalThis.document = {
  getElementById(id) {
    if (id === 'media-grid') return grid;
    if (id === 'empty-hint') return emptyHint;
    if (id === 'breadcrumb-bar') return breadcrumb;
    if (id === 'result-count') return resultCount;
    return null;
  },
  querySelectorAll() { return []; },
};

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url: String(url), options });
  if (String(url) === '/api/in-progress') {
    return jsonResponse([
      {
        id: 7,
        file_name: 'resume.mp4',
        type: 'video',
        duration: 125,
        file_size_bytes: 2048,
        thumbnail_path: 'thumbs/resume.jpg',
      },
    ]);
  }
  return jsonResponse({ status: 'ok' });
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

function jsonResponse(body) {
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    json: async () => body,
  };
}

async function testProgressStatusAPIWrapper() {
  requests.length = 0;
  await API.progressStatus(7, 'finished');
  const req = requests[0];
  assert(req?.url === '/api/progress/status', 'progressStatus should call the progress status endpoint');
  assert(req?.options?.method === 'POST', 'progressStatus should POST');
  assert(
    req?.options?.body === JSON.stringify({ media_id: 7, status: 'finished' }),
    'progressStatus should send media_id and status',
  );
}

async function testInProgressAPIWrapper() {
  requests.length = 0;
  const list = await API.inProgress();
  assert(requests[0]?.url === '/api/in-progress', 'inProgress should call the in-progress endpoint');
  assert(Array.isArray(list), 'inProgress should return the decoded list');
}

async function testInProgressVirtualGrid() {
  requests.length = 0;
  state.virtualSet = 'in-progress';
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.folderPath = '';
  state.mediaPage = 0;
  initMediaGrid();

  await loadMedia();

  assert(requests[0]?.url === '/api/in-progress', 'virtual set should load via API.inProgress');
  assert(state.media.length === 1 && state.media[0].id === 7, 'virtual set should store flat media results');
  assert(grid.innerHTML.includes('data-action="mark-finished"'), 'cards should render a finished action');
  assert(grid.innerHTML.includes('data-action="mark-not-started"'), 'cards should render a not-started action');
  assert(resultCount.textContent === '1 items', 'result count should reflect in-progress media');
}

console.log('Running progress action tests...');
await testProgressStatusAPIWrapper();
await testInProgressAPIWrapper();
await testInProgressVirtualGrid();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All progress action tests passed.');
  process.exit(0);
}
