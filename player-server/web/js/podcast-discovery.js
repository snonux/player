import { escapeHtml } from './utils.js';

export async function searchPodcasts(query, { signal, country = 'US', popular = false } = {}) {
  const response = await fetch(popular ? `/api/podcast-discovery?popular=true&country=${encodeURIComponent(country)}` : `/api/podcast-discovery?q=${encodeURIComponent(query.trim())}`, {
    signal, credentials: 'omit', referrerPolicy: 'no-referrer',
  });
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || 'Podcast directory unavailable. Retry or add a feed URL.');
  return payload;
}

// One focused journey for account and device subscriptions. No directory or
// artwork requests occur until the listener explicitly submits a search.
export function openPodcastDiscovery({ subscribe, refreshFeed, onSubscribed = () => {} }) {
  const opener = document.activeElement;
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  const dialog = document.createElement('dialog');
  dialog.className = 'modal modal-wide podcast-discovery-dialog';
  dialog.setAttribute('aria-label', 'Explore podcasts');
  dialog.innerHTML = `<div class="modal-header"><h2>Explore podcasts</h2><div class="spacer"></div><button type="button" class="icon-btn" data-close aria-label="Close explore podcasts">✕</button></div>
    <p class="text-muted text-sm">Search Apple Podcasts by title, author or topic. Search terms and the selected chart country are sent to Apple through this Player server. Subscribing contacts the podcast publisher. No search artwork is loaded.</p>
    <form data-popular class="mt-2"><label>Chart country <select name="country"><option value="US">United States</option><option value="GB">United Kingdom</option><option value="DE">Germany</option><option value="BG">Bulgaria</option><option value="FR">France</option><option value="ES">Spain</option><option value="IT">Italy</option><option value="CA">Canada</option><option value="AU">Australia</option><option value="IN">India</option><option value="JP">Japan</option><option value="BR">Brazil</option><option value="NL">Netherlands</option></select></label> <button class="btn" type="button" data-run-popular>Load popular podcasts</button></form>
    <form data-search><label>Search podcasts <input name="query" type="search" minlength="2" maxlength="100" required autocomplete="off"></label> <button class="btn btn-primary" type="button" data-run-search>Search</button></form>
    <button type="button" class="btn mt-2" data-toggle-manual aria-expanded="false" aria-controls="podcast-discovery-url-form">Add by feed URL</button><form data-manual id="podcast-discovery-url-form" hidden><label>Feed URL <input name="url" type="url" required placeholder="https://example.com/feed.xml"></label> <button class="btn" type="button" data-run-manual>Subscribe</button></form>
    <button type="button" class="btn" data-retry-refresh hidden>Retry fetching episodes</button>
    <p role="status" aria-live="polite" data-status>Enter a topic or podcast name to explore.</p><div data-results></div>`;
  overlay.append(dialog);
  document.body.append(overlay);
  let controller;
  let actionRevision = 0;
  const status = dialog.querySelector('[data-status]');
  const results = dialog.querySelector('[data-results]');
  const close = () => { controller?.abort(); dialog.close(); overlay.remove(); opener?.focus(); };
  dialog.querySelector('[data-close]').onclick = close;
  dialog.addEventListener('cancel', event => { event.preventDefault(); close(); });
  overlay.addEventListener('click', event => { if (event.target === overlay) close(); });
  dialog.addEventListener('keydown', event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
  });
  dialog.showModal();
  async function add(url, title, button, manual = false) {
    const own = ++actionRevision;
    dialog.querySelector('[data-retry-refresh]').hidden = true;
    button.disabled = true;
    status.textContent = 'Subscribing…';
    try {
      const subscription = await subscribe(url, title);
      status.textContent = `Subscribed to ${title || url}.`;
      if (manual) button.disabled = false;
      else button.textContent = 'Subscribed';
      await onSubscribed();
      if (refreshFeed) void refresh(subscription, own);
    } catch (error) {
      button.disabled = false;
      status.textContent = error.message || 'Could not subscribe. Retry or add the feed URL.';
    }
  }
  async function refresh(subscription, own) {
    const retry = dialog.querySelector('[data-retry-refresh]');
    const current = () => overlay.isConnected && own === actionRevision;
    if (current()) { status.textContent = 'Subscribed. Fetching episodes…'; retry.hidden = true; }
    try {
      await refreshFeed(subscription);
      if (current()) status.textContent = 'Subscribed. Episodes refreshed.';
      await onSubscribed();
    } catch (error) {
      if (!current()) return;
      status.textContent = 'Subscription saved. Could not fetch episodes. Retry here or refresh the feed from its episode list.';
      retry.hidden = false;
      retry.onclick = () => { void refresh(subscription, own); };
    }
  }
  const manualForm = dialog.querySelector('[data-manual]');
  const manualToggle = dialog.querySelector('[data-toggle-manual]');
  manualToggle.onclick = () => {
    manualForm.hidden = !manualForm.hidden;
    manualToggle.setAttribute('aria-expanded', String(!manualForm.hidden));
    if (!manualForm.hidden) manualForm.elements.url.focus();
  };
  function subscribeManual() {
    if (manualForm.reportValidity()) void add(manualForm.elements.url.value.trim(), '', manualForm.querySelector('button'), true);
  }
  manualForm.onsubmit = event => { event.preventDefault(); subscribeManual(); };
  dialog.querySelector('[data-run-manual]').onclick = subscribeManual;
  async function load(form, popular = false) {
    if (!form.reportValidity()) return;
    actionRevision++;
    dialog.querySelector('[data-retry-refresh]').hidden = true;
    controller?.abort(); controller = new AbortController();
    const own = controller;
    results.replaceChildren(); status.textContent = popular ? 'Loading popular podcasts…' : 'Searching Apple Podcasts…';
    try {
      const rows = await searchPodcasts(popular ? '' : form.elements.query.value, { signal: own.signal, popular, country: popular ? form.elements.country.value : 'US' });
      if (controller !== own || !overlay.isConnected) return;
      status.textContent = rows.length ? `${rows.length} podcasts found.` : 'No podcasts found. Try another topic or add a feed URL.';
      for (const row of rows) {
        const item = document.createElement('div');
        item.className = 'podcast-feed-row py-1 border-b';
        item.innerHTML = `<span class="flex-1"><strong>${escapeHtml(row.title)}</strong><br><span class="text-muted text-sm">${escapeHtml(row.author)}</span></span><button type="button" class="btn">Subscribe</button>`;
        const button = item.querySelector('button');
        button.setAttribute('aria-label', `Subscribe to ${row.title}`);
        button.onclick = () => add(row.feed_url, row.title, button);
        results.append(item);
      }
    } catch (error) {
      if (own.signal.aborted) return;
      status.textContent = error.message || 'Search failed. Check your connection and retry.';
    }
  }
  const searchForm = dialog.querySelector('[data-search]');
  const popularForm = dialog.querySelector('[data-popular]');
  searchForm.onsubmit = event => { event.preventDefault(); void load(searchForm); };
  popularForm.onsubmit = event => { event.preventDefault(); void load(popularForm, true); };
  dialog.querySelector('[data-run-search]').onclick = () => { void load(searchForm); };
  dialog.querySelector('[data-run-popular]').onclick = () => { void load(popularForm, true); };
}
