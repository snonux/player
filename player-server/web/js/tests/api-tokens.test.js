import assert from 'node:assert/strict';
import { apiTokenHelpers, initAPITokens } from '../api-tokens.js';
import { API } from '../api.js';
import { setAuthenticatedUser } from '../state.js';

const { shortDate, expiresLabel, daysUntil, expiresAtFromDate } = apiTokenHelpers;

assert.equal(shortDate('2026-05-17T09:00:00Z'), '2026-05-17');
assert.equal(shortDate(''), '');
assert.equal(expiresLabel(null), 'Expires: No expiry');
assert.equal(expiresLabel('2027-05-17T10:00:00Z'), 'Expires: 2027-05-17');
assert.equal(daysUntil(''), null);
assert.equal(daysUntil('bogus'), null);
assert.equal(expiresAtFromDate(''), null);
assert.equal(expiresAtFromDate('bogus'), null);
{
  const d = new Date();
  d.setDate(d.getDate() + 10);
  const stamp = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  assert.equal(daysUntil(stamp), 10);
  assert.equal(expiresAtFromDate(stamp), `${stamp}T00:00:00Z`);
  assert.equal(expiresLabel(expiresAtFromDate(stamp)), `Expires: ${stamp}`);
}

// --- API client contract (session cookie; plaintext only on create) ---
const requests = [];
globalThis.fetch = async (url, options = {}) => {
  requests.push({ url: String(url), method: options.method || 'GET', body: options.body });
  if (String(url).endsWith('/api/v1/auth/tokens') && (options.method || 'GET') === 'GET') {
    return new Response(JSON.stringify([{ id: 1, name: 'a', created_at: '2026-01-01T00:00:00Z', expires_at: null, last_used_at: null }]), {
      status: 200, headers: { 'content-type': 'application/json' },
    });
  }
  if (String(url).endsWith('/api/v1/auth/tokens') && options.method === 'POST') {
    return new Response(JSON.stringify({ id: 2, name: 'ci', token: 'pt_once' }), {
      status: 200, headers: { 'content-type': 'application/json' },
    });
  }
  if (String(url).includes('/api/v1/auth/tokens/') && options.method === 'DELETE') {
    return new Response(null, { status: 204 });
  }
  return new Response('{}', { status: 500, headers: { 'content-type': 'application/json' } });
};

const listed = await API.listAPITokens();
assert.equal(listed[0].id, 1);
assert.equal(requests.at(-1).method, 'GET');
assert.match(requests.at(-1).url, /\/api\/v1\/auth\/tokens$/);

const created = await API.createAPIToken('ci', 30);
assert.equal(created.token, 'pt_once');
assert.equal(JSON.parse(requests.at(-1).body).expires_in_days, 30);
assert.equal(JSON.parse(requests.at(-1).body).name, 'ci');

const forever = await API.createAPIToken('forever');
assert.equal(JSON.parse(requests.at(-1).body).name, 'forever');
assert.equal('expires_in_days' in JSON.parse(requests.at(-1).body), false);
assert.equal(forever.token, 'pt_once');

await API.revokeAPIToken(9);
assert.equal(requests.at(-1).method, 'DELETE');
assert.match(requests.at(-1).url, /\/api\/v1\/auth\/tokens\/9$/);

// --- Settings Account mount: parse innerHTML into queryable nodes ---
function parseTree(html, root) {
  const re = /<(\/)?([a-z0-9]+)([^>]*)>|([^<]+)/gi;
  const stack = [root];
  let match;
  while ((match = re.exec(html))) {
    if (match[4] !== undefined) {
      const text = match[4].trim();
      if (text) stack.at(-1).textContent += (stack.at(-1).textContent ? ' ' : '') + text;
      continue;
    }
    const closing = Boolean(match[1]);
    const tag = match[2].toLowerCase();
    const attrs = match[3] || '';
    if (closing) {
      if (stack.length > 1) stack.pop();
      continue;
    }
    const node = createNode(tag);
    for (const attr of attrs.matchAll(/([:@?a-z0-9-]+)(?:=(?:"([^"]*)"|'([^']*)'))?/gi)) {
      const name = attr[1];
      const value = attr[2] ?? attr[3] ?? '';
      if (name === 'class') node.className = value;
      else if (name.startsWith('data-')) {
        const key = name.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
        node.dataset[key] = value;
        node.attrs[name] = value;
      } else if (name === 'hidden') {
        node.hidden = true;
      } else {
        node.attrs[name] = value;
      }
    }
    stack.at(-1).appendChild(node);
    if (!/\/\s*$/.test(attrs) && !['input', 'br', 'hr', 'img'].includes(tag)) stack.push(node);
  }
}

function createNode(tag = 'div') {
  const node = {
    tagName: tag.toUpperCase(),
    className: '',
    attrs: {},
    dataset: {},
    children: [],
    textContent: '',
    value: '',
    hidden: false,
    disabled: false,
    open: false,
    focused: false,
    listeners: {},
    isConnected: true,
    focus() { node.focused = true; },
    append(...kids) { kids.forEach((k) => node.appendChild(typeof k === 'string' ? Object.assign(createNode('#text'), { textContent: k }) : k)); },
    appendChild(child) { node.children.push(child); child.parent = node; return child; },
    replaceChildren(...kids) { node.children = []; kids.forEach((k) => node.appendChild(k)); },
    setAttribute(name, value) {
      node.attrs[name] = value;
      if (name.startsWith('data-')) {
        const key = name.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
        node.dataset[key] = value;
      }
    },
    getAttribute(name) { return node.attrs[name] ?? null; },
    addEventListener(type, handler) { (node.listeners[type] = node.listeners[type] || []).push(handler); },
    dispatchEvent(event) {
      for (const handler of node.listeners[event.type] || []) handler(event);
      return true;
    },
    querySelector(sel) { return queryAll(node, sel)[0] || null; },
    querySelectorAll(sel) { return queryAll(node, sel); },
    matches(sel) { return matchSel(node, sel); },
  };
  Object.defineProperty(node, 'innerHTML', {
    get() { return node._html || ''; },
    set(value) {
      node._html = String(value);
      node.children = [];
      node.textContent = '';
      parseTree(node._html, node);
    },
  });
  return node;
}

function queryAll(root, sel) {
  const parts = sel.split(',').map((s) => s.trim());
  const out = [];
  const walk = (n) => {
    if (parts.some((part) => matchSel(n, part))) out.push(n);
    for (const c of n.children || []) walk(c);
  };
  walk(root);
  return out;
}

function matchSel(node, sel) {
  if (sel === 'button,input' || sel === 'button, input') return node.tagName === 'BUTTON' || node.tagName === 'INPUT';
  if (/^[a-z][a-z0-9]*$/i.test(sel)) return node.tagName === sel.toUpperCase();
  if (sel.startsWith('.')) return (node.className || '').split(/\s+/).includes(sel.slice(1));
  if (sel.startsWith('[') && sel.endsWith(']')) {
    const body = sel.slice(1, -1);
    const eq = body.indexOf('=');
    if (eq === -1) {
      if (body.startsWith('data-')) {
        const key = body.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
        return Object.prototype.hasOwnProperty.call(node.dataset, key);
      }
      return node.attrs[body] != null;
    }
    const name = body.slice(0, eq);
    const want = body.slice(eq + 1).replace(/^["']|["']$/g, '');
    if (name.startsWith('data-')) {
      const key = name.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
      return String(node.dataset[key] ?? '') === want;
    }
    return String(node.attrs[name] ?? '') === want;
  }
  return false;
}

const modal = createNode('div');
modal.className = 'modal';
const overlay = createNode('div');
overlay.querySelector = (sel) => (sel === '.modal' ? modal : null);
const toastEl = createNode('div');
Object.assign(toastEl, { className: '', classList: { remove() {}, add() {} } });

const docListeners = {};
globalThis.document = {
  documentElement: { lang: 'en' },
  getElementById: (id) => (id === 'settings-modal' ? overlay : id === 'toast' ? toastEl : null),
  createElement: (tag) => createNode(tag),
  createRange: () => ({ selectNodeContents() {} }),
  addEventListener(type, handler) { (docListeners[type] = docListeners[type] || []).push(handler); },
  dispatchEvent(event) {
    for (const handler of docListeners[event.type] || []) handler(event);
    return true;
  },
  querySelectorAll: () => [],
};
globalThis.localStorage = { getItem: () => null, setItem() {}, removeItem() {} };
globalThis.window = globalThis;
Object.defineProperty(globalThis, 'navigator', {
  value: { clipboard: { writeText: async () => {} } },
  configurable: true,
});
globalThis.getSelection = () => ({ removeAllRanges() {}, addRange() {} });
globalThis.Event = class { constructor(type) { this.type = type; } };

setAuthenticatedUser(7);
API.listAPITokens = async () => [{ id: 7, name: 'android-client', created_at: '2026-05-17T09:00:00Z', expires_at: null }];
API.createAPIToken = async (name, days) => ({ id: 8, name, token: 'pt_secret_once', expires_in_days: days });
API.revokeAPIToken = async () => {};

initAPITokens();
assert.equal(modal.children.length, 1);
initAPITokens();
assert.equal(modal.children.length, 1, 'mount is idempotent');

const section = modal.children[0];
assert.ok(section.dataset.accountTokens === '' || section.dataset.accountTokens != null);
assert.match(section.querySelector('h4').textContent, /Account/);
assert.match(section.querySelector('[data-account-label]').textContent, /account 7/);
const panel = section.querySelector('.api-tokens');
assert.ok(panel);
assert.match(panel.querySelector('summary').textContent, /API Tokens/);

panel.open = true;
for (const handler of panel.listeners.toggle || []) handler({ type: 'toggle' });
await new Promise((r) => setTimeout(r, 0));
assert.match(panel.querySelector('[data-status]').textContent, /1 token/);
assert.equal(panel.querySelectorAll('[data-revoke]').length, 1);

assert.equal(panel.querySelector('[data-create]').disabled, false);

panel.querySelector('[data-create]').listeners.click[0]();
assert.equal(panel.querySelector('[data-create-form]').hidden, false);
panel.querySelector('[data-name]').value = 'ci-token';
const expiry = new Date();
expiry.setDate(expiry.getDate() + 30);
const expiryStamp = `${expiry.getFullYear()}-${String(expiry.getMonth() + 1).padStart(2, '0')}-${String(expiry.getDate()).padStart(2, '0')}`;
panel.querySelector('[data-expires]').value = expiryStamp;
await panel.querySelector('[data-create-form]').listeners.submit[0]({ preventDefault() {} });
await new Promise((r) => setTimeout(r, 0));
assert.equal(panel.querySelector('[data-plaintext]').hidden, false);
assert.equal(panel.querySelector('[data-plaintext-value]').textContent, 'pt_secret_once');
const createdRow = panel.querySelector('[data-token-id="8"]');
assert.ok(createdRow);
assert.match(createdRow.querySelector('.text-muted').textContent, new RegExp(`Expires: ${expiryStamp}`), 'create shows synthesized expiry without expires_at in response');

for (const handler of panel.listeners.keydown || []) {
  handler({ type: 'keydown', key: 'Escape', preventDefault() {}, stopImmediatePropagation() {} });
}
assert.equal(panel.querySelector('[data-plaintext]').hidden, false, 'Escape does not dismiss one-time secret');
panel.querySelector('[data-plaintext-done]').listeners.click[0]();
assert.equal(panel.querySelector('[data-plaintext]').hidden, true);

const revoke = panel.querySelector('[data-revoke="7"]');
assert.ok(revoke);
revoke.listeners.click[0]();
assert.equal(panel.querySelector('[data-revoke-confirm]').hidden, false);
assert.match(panel.querySelector('[data-revoke-question]').textContent, /android-client/);
await panel.querySelector('[data-revoke-go]').listeners.click[0]();
await new Promise((r) => setTimeout(r, 0));
assert.match(panel.querySelector('[data-status]').textContent, /1 token/);
assert.equal(panel.querySelector('[data-revoke="7"]'), null);
assert.ok(panel.querySelector('[data-revoke="8"]'));

setAuthenticatedUser(null);
document.dispatchEvent(new Event('listening-progress:account'));
await new Promise((r) => setTimeout(r, 0));
assert.equal(panel.querySelector('[data-create]').disabled, true, 'create gated when signed out');
assert.match(panel.querySelector('[data-status]').textContent, /Sign in to load API tokens/);
assert.equal(panel.querySelector('[data-create-form]').hidden, true);
panel.querySelector('[data-create]').listeners.click[0]();
assert.equal(panel.querySelector('[data-create-form]').hidden, true, 'showCreate no-ops when signed out');

console.log('api-tokens: helpers, API client, Account entry, list/create/expiry/one-time copy/revoke/signed-out gate passed');
