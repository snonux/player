import assert from 'node:assert/strict';
import { mountGlobalSyncView } from '../global-sync-view.js';
import { staleSyncMS } from '../global-sync.js';
const element = () => ({ textContent: '', disabled: false, hidden: false, clicks: 0,
  classList: { toggle(name, value) { this[name] = value; } },
  addEventListener(name, fn) { this[name] = fn; }, click() { this.clicks++; this.click?.handler; } });
const text = element(), button = element(), link = element(), globalButton = element(), host = element();
host.querySelector = selector => ({ '[data-sync-status]': text, button, a: link })[selector];
let state = { account: 'a', lastFullSuccessMs: 1 }, retries = 0;
const render = mountGlobalSyncView(host, globalButton, () => retries++, () => state);
render(state, staleSyncMS); assert.equal(button.textContent, 'Sync now');
render(state, staleSyncMS + 1); assert.equal(button.textContent, 'Retry');
button.click(); globalButton.click(); assert.equal(retries, 2, 'both real click handlers request global sync');
state = { ...state, errorKind: 'auth' }; render(state);
assert.equal(button.classList.hidden, true); assert.equal(link.classList.hidden, false);
globalButton.click(); assert.equal(link.clicks, 1); assert.equal(retries, 2, 'auth leads to sign-in, not ineffective retry');
state = { ...state, active: true }; render(state); assert.equal(globalButton.disabled, true);
state = { account: null }; render(state); assert.equal(host.classList.hidden, true);
console.log('Global native-button events, auth link, quiet status and clock view checks passed.');
