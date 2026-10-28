import { API } from './api.js';
import { state } from './state.js';
import { escapeHtml, toast } from './utils.js';
import { applyTranslations } from './i18n.js';

// Account › API Tokens lives in Settings (matches Android Settings Account entry
// and the admin Subsonic note pointing at Settings › API Tokens). Focus trap and
// Escape hierarchy stay on the existing settings modal.

function shortDate(iso) {
  if (!iso || typeof iso !== 'string') return '';
  return iso.length >= 10 ? iso.slice(0, 10) : iso;
}

function expiresLabel(iso) {
  const day = shortDate(iso);
  return day ? `Expires: ${day}` : 'Expires: No expiry';
}

function daysUntil(dateValue) {
  if (!dateValue) return null;
  const parts = String(dateValue).split('-').map(Number);
  if (parts.length !== 3 || parts.some((n) => !Number.isFinite(n))) return null;
  const [y, m, d] = parts;
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const target = new Date(y, m - 1, d);
  const days = Math.round((target - today) / 86400000);
  if (days < 1) return null;
  return Math.min(days, 36500);
}

// Create responses omit expires_at; keep the chosen calendar date for display.
function expiresAtFromDate(dateValue) {
  if (!dateValue || daysUntil(dateValue) == null) return null;
  return `${String(dateValue).slice(0, 10)}T00:00:00Z`;
}

export function initAPITokens() {
  const host = document.getElementById('settings-modal')?.querySelector('.modal');
  if (!host || host.querySelector('[data-account-tokens]')) return;

  const section = document.createElement('section');
  section.dataset.accountTokens = '';
  section.className = 'account-tokens';
  section.innerHTML = `
    <h4 data-i18n="Account">Account</h4>
    <p class="text-muted text-sm" data-account-label>Signed in on this browser.</p>
    <details class="api-tokens">
      <summary data-i18n="API Tokens">API Tokens</summary>
      <p class="text-muted text-sm">When Subsonic is enabled, point DSub, Tempo, or another client at this Player URL (path /rest). Prefer a token from this list as the apiKey (or as the password with your username). Revoke a token here to disconnect that client. Passwords and token secrets are never shown in Admin.</p>
      <p data-status role="status"></p>
      <div class="api-tokens-actions">
        <button type="button" class="btn btn-ghost" data-refresh>Refresh</button>
        <button type="button" class="btn" data-create>Create token</button>
      </div>
      <ul data-list class="api-token-list"></ul>
      <form data-create-form class="api-token-create" hidden>
        <label>Token name <input name="name" data-name required maxlength="200" autocomplete="off" placeholder="e.g. android-client"></label>
        <label>Expiry date <input name="expires" data-expires type="date"> <button type="button" class="btn btn-ghost" data-expiry-clear hidden>Clear expiry</button></label>
        <p class="text-muted text-sm">Leave the date empty for no expiry.</p>
        <div class="api-tokens-actions">
          <button type="button" class="btn btn-ghost" data-create-cancel>Cancel</button>
          <button type="submit" class="btn">Create</button>
        </div>
      </form>
      <div data-plaintext class="api-token-plaintext" hidden>
        <p><strong>Copy this token now — it will not be shown again.</strong></p>
        <code data-plaintext-value class="api-token-secret"></code>
        <div class="api-tokens-actions">
          <button type="button" class="btn" data-copy>Copy</button>
          <button type="button" class="btn btn-ghost" data-plaintext-done>Done</button>
        </div>
      </div>
      <div data-revoke-confirm class="api-token-revoke" hidden>
        <p data-revoke-question></p>
        <div class="api-tokens-actions">
          <button type="button" class="btn btn-ghost" data-revoke-cancel>Cancel</button>
          <button type="button" class="btn btn-danger" data-revoke-go>Revoke</button>
        </div>
      </div>
    </details>`;
  // Place Account under the Settings header so it matches Android's Account-first
  // settings entry and stays discoverable above device preference panels.
  const header = host.querySelector('.modal-header');
  if (header?.nextSibling) host.insertBefore(section, header.nextSibling);
  else host.appendChild(section);
  applyTranslations(section);

  const panel = section.querySelector('.api-tokens');
  const q = (sel) => panel.querySelector(sel);
  const status = (msg) => { q('[data-status]').textContent = msg; };
  let tokens = null;
  let busy = false;
  let loaded = false;
  let pendingRevoke = null;
  let plaintext = '';

  function accountLabel() {
    const id = state.user?.id;
    section.querySelector('[data-account-label]').textContent = id
      ? `Signed in · account ${id}. Create and revoke Bearer API tokens for this account.`
      : 'Sign in to manage API tokens for this account.';
  }

  function setBusy(value) {
    busy = value;
    const signedIn = !!state.user;
    const allow = new Set();
    if (plaintext) {
      allow.add(q('[data-copy]'));
      allow.add(q('[data-plaintext-done]'));
    }
    if (pendingRevoke) {
      allow.add(q('[data-revoke-cancel]'));
      allow.add(q('[data-revoke-go]'));
    }
    const createBtn = q('[data-create]');
    for (const el of panel.querySelectorAll('button, input')) {
      if (allow.has(el)) {
        el.disabled = false;
        continue;
      }
      el.disabled = busy || !!plaintext || (el === createBtn && !signedIn);
    }
  }

  function hideCreate() {
    q('[data-create-form]').hidden = true;
    q('[data-name]').value = '';
    q('[data-expires]').value = '';
    q('[data-expiry-clear]').hidden = true;
  }

  function hidePlaintext() {
    plaintext = '';
    q('[data-plaintext]').hidden = true;
    q('[data-plaintext-value]').textContent = '';
  }

  function cancelRevoke() {
    const opener = pendingRevoke?.opener;
    pendingRevoke = null;
    q('[data-revoke-confirm]').hidden = true;
    opener?.focus();
  }

  function renderList() {
    const list = q('[data-list]');
    list.replaceChildren();
    if (!tokens || !tokens.length) {
      const empty = document.createElement('li');
      empty.className = 'text-muted';
      empty.dataset.empty = '';
      empty.textContent = 'No API tokens. Create one for Subsonic clients or automation.';
      list.append(empty);
      return;
    }
    for (const token of tokens) {
      const li = document.createElement('li');
      li.dataset.tokenId = String(token.id);
      const created = shortDate(token.created_at);
      const meta = [created ? `Created: ${created}` : '', expiresLabel(token.expires_at)].filter(Boolean).join('  ·  ');
      li.innerHTML = `<div><strong>${escapeHtml(token.name || 'Token')}</strong><div class="text-muted text-sm">${escapeHtml(meta)}</div></div>`;
      const revoke = document.createElement('button');
      revoke.type = 'button';
      revoke.className = 'btn btn-danger btn-sm';
      revoke.dataset.revoke = String(token.id);
      revoke.setAttribute('aria-label', `Revoke token ${token.name || token.id}`);
      revoke.textContent = 'Revoke';
      revoke.disabled = busy || token.id === 0 || !!plaintext;
      revoke.addEventListener('click', () => askRevoke(token, revoke));
      li.append(revoke);
      list.append(li);
    }
  }

  async function load() {
    if (busy) return;
    const owner = state.user;
    if (!owner) {
      tokens = null;
      loaded = false;
      renderList();
      status('Sign in to load API tokens.');
      return;
    }
    busy = true;
    setBusy(true);
    status('Loading API tokens…');
    try {
      const rows = await API.listAPITokens();
      if (state.user !== owner) return;
      tokens = Array.isArray(rows) ? rows : [];
      loaded = true;
      renderList();
      status(tokens.length ? `${tokens.length} token${tokens.length === 1 ? '' : 's'}.` : 'No API tokens yet.');
    } catch {
      if (state.user !== owner) return;
      status('Could not load API tokens. Connect to the server and retry.');
    } finally {
      busy = false;
      setBusy(false);
    }
  }

  function askRevoke(token, opener) {
    if (busy || plaintext || token.id === 0) return;
    pendingRevoke = { token, opener };
    q('[data-revoke-question]').textContent = `Revoke "${token.name || 'token'}"? It will stop working immediately.`;
    q('[data-revoke-confirm]').hidden = false;
    q('[data-revoke-go]').focus();
  }

  async function confirmRevoke() {
    const pending = pendingRevoke;
    cancelRevoke();
    if (!pending || busy) return;
    const { token } = pending;
    const owner = state.user;
    const previous = tokens;
    tokens = (tokens || []).filter((t) => t.id !== token.id);
    renderList();
    busy = true;
    setBusy(true);
    try {
      await API.revokeAPIToken(token.id);
      if (state.user !== owner) return;
      toast(`Token "${token.name || 'token'}" revoked.`);
      status(tokens.length ? `${tokens.length} token${tokens.length === 1 ? '' : 's'}.` : 'No API tokens yet.');
    } catch {
      if (state.user !== owner) return;
      tokens = previous;
      renderList();
      status('Could not revoke token. Connect to the server and retry.');
      toast('Could not revoke token.', 'error');
    } finally {
      busy = false;
      setBusy(false);
    }
  }

  function showCreate() {
    if (busy || plaintext || !state.user) return;
    cancelRevoke();
    const expires = q('[data-expires]');
    const min = new Date();
    min.setDate(min.getDate() + 1);
    expires.min = `${min.getFullYear()}-${String(min.getMonth() + 1).padStart(2, '0')}-${String(min.getDate()).padStart(2, '0')}`;
    q('[data-create-form]').hidden = false;
    q('[data-name]').focus();
  }

  async function submitCreate(event) {
    event.preventDefault();
    if (busy || plaintext || !state.user) return;
    const name = q('[data-name]').value.trim();
    if (!name) {
      status('Token name is required.');
      q('[data-name]').focus();
      return;
    }
    const expiresValue = q('[data-expires]').value;
    const expiresInDays = daysUntil(expiresValue);
    if (expiresValue && expiresInDays == null) {
      status('Expiry date must be tomorrow or later.');
      q('[data-expires]').focus();
      return;
    }
    const owner = state.user;
    const synthesizedExpiry = expiresAtFromDate(expiresValue);
    const placeholder = { id: 0, name, created_at: '', expires_at: synthesizedExpiry };
    tokens = [...(tokens || []), placeholder];
    renderList();
    hideCreate();
    busy = true;
    setBusy(true);
    status('Creating token…');
    try {
      const created = await API.createAPIToken(name, expiresInDays);
      if (state.user !== owner) return;
      const row = {
        id: created.id,
        name: created.name || name,
        created_at: created.created_at || new Date().toISOString(),
        // Create response has no expires_at; keep the date the user chose.
        expires_at: created.expires_at ?? synthesizedExpiry,
        last_used_at: null,
      };
      tokens = (tokens || []).filter((t) => t.id !== 0).concat(row);
      renderList();
      const secret = typeof created.token === 'string' ? created.token : '';
      if (secret) {
        plaintext = secret;
        q('[data-plaintext-value]').textContent = secret;
        q('[data-plaintext]').hidden = false;
        q('[data-copy]').focus();
        status('Copy the new token now. It will not be shown again.');
      } else {
        status('Token created.');
      }
    } catch {
      if (state.user !== owner) return;
      tokens = (tokens || []).filter((t) => t.id !== 0);
      renderList();
      status('Could not create token. Check the name and try again.');
      toast('Could not create token.', 'error');
    } finally {
      busy = false;
      setBusy(false);
    }
  }

  async function copyPlaintext() {
    if (!plaintext) return;
    try {
      await navigator.clipboard.writeText(plaintext);
      toast('Token copied to clipboard.');
    } catch {
      // Selection fallback: keep the secret visible for manual copy.
      const range = document.createRange();
      range.selectNodeContents(q('[data-plaintext-value]'));
      const sel = window.getSelection();
      sel?.removeAllRanges();
      sel?.addRange(range);
      toast('Select and copy the token manually.', 'error');
    }
  }

  q('[data-refresh]').addEventListener('click', () => load());
  q('[data-create]').addEventListener('click', showCreate);
  q('[data-create-cancel]').addEventListener('click', hideCreate);
  q('[data-create-form]').addEventListener('submit', submitCreate);
  q('[data-expires]').addEventListener('change', () => {
    q('[data-expiry-clear]').hidden = !q('[data-expires]').value;
  });
  q('[data-expiry-clear]').addEventListener('click', () => {
    q('[data-expires]').value = '';
    q('[data-expiry-clear]').hidden = true;
  });
  q('[data-copy]').addEventListener('click', () => void copyPlaintext());
  q('[data-plaintext-done]').addEventListener('click', () => {
    hidePlaintext();
    setBusy(false);
    status(tokens?.length ? `${tokens.length} token${tokens.length === 1 ? '' : 's'}.` : 'No API tokens yet.');
    q('[data-create]').focus();
  });
  q('[data-revoke-cancel]').addEventListener('click', cancelRevoke);
  q('[data-revoke-go]').addEventListener('click', () => void confirmRevoke());
  panel.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    // One-time secret disclosure is non-dismissible (Android barrierDismissible:
    // false); only Done clears it. Escape still cancels revoke / create drafts.
    if (plaintext) {
      event.preventDefault();
      event.stopImmediatePropagation();
      q('[data-plaintext-done]').focus();
      return;
    }
    if (pendingRevoke) {
      event.preventDefault();
      event.stopImmediatePropagation();
      cancelRevoke();
      return;
    }
    if (!q('[data-create-form]').hidden) {
      event.preventDefault();
      event.stopImmediatePropagation();
      hideCreate();
      q('[data-create]').focus();
    }
  }, true);
  panel.addEventListener('toggle', () => {
    accountLabel();
    if (panel.open && !loaded) void load();
  });

  accountLabel();
  setBusy(false);
  document.addEventListener('listening-progress:account', () => {
    loaded = false;
    tokens = null;
    hideCreate();
    hidePlaintext();
    cancelRevoke();
    renderList();
    accountLabel();
    setBusy(false);
    if (!state.user) {
      status('Sign in to load API tokens.');
      return;
    }
    status('Refresh API tokens for this account.');
    if (panel.open) void load();
  });
}

// Exported for unit tests.
export const apiTokenHelpers = { shortDate, expiresLabel, daysUntil, expiresAtFromDate };
