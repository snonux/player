// Derive access from the explicit playback URL, including detached/share URLs.
// Unknown or cross-origin streams never trigger a library request by media ID.
export function lyricsUrl(streamUrl, base = location.href) {
  try {
    const url = new URL(streamUrl, base);
    if (url.origin !== new URL(base).origin ||
        !/^\/(?:api\/(?:v1\/)?media\/\d+|s\/[^/]+)\/(?:stream|compat)$/.test(url.pathname)) return null;
    url.pathname = url.pathname.replace(/\/(?:stream|compat)$/, '/lyrics');
    const view = url.searchParams.get('view');
    url.search = '';
    if (view) url.searchParams.set('view', view);
    return url.pathname + url.search;
  } catch { return null; }
}

export function normalizeLyrics(data) {
  if (!Array.isArray(data?.lines)) return { synced: false, lines: [] };
  const lines = data.lines.filter(line => typeof line?.text === 'string')
    .map(line => ({ text: line.text, start: line.start }));
  // Mixed or invalid timing stays readable as plain text.
  const synced = data.synced === true && lines.length > 0 && lines.every((line, i) =>
    typeof line.start === 'number' && Number.isFinite(line.start) && line.start >= 0 &&
    (i === 0 || line.start >= lines[i - 1].start));
  return { synced, lines: lines.some(line => line.text.trim()) ? lines : [] };
}

export function currentLyricIndex(lines, seconds) {
  if (!Number.isFinite(seconds)) return -1;
  // Last timestamp at or before playback, including repeated timestamps.
  let low = 0, high = lines.length;
  while (low < high) {
    const mid = (low + high) >>> 1;
    if (lines[mid].start <= seconds) low = mid + 1;
    else high = mid;
  }
  return low - 1;
}

export function initLyrics({ audio, player }) {
  const stage = player?.querySelector('.stage');
  const controls = player?.querySelector('.controls');
  if (!audio || !stage || !controls) return null;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'lyrics-toggle hidden';
  button.textContent = 'Lyrics';
  button.setAttribute('aria-controls', 'audio-lyrics');
  const panel = document.createElement('div');
  panel.id = 'audio-lyrics';
  panel.className = 'audio-lyrics hidden';
  panel.setAttribute('role', 'region');
  panel.setAttribute('aria-label', 'Lyrics');
  panel.tabIndex = 0;
  controls.appendChild(button);
  stage.appendChild(panel);
  let wanted = true;
  let data = { synced: false, lines: [] };
  let request = null;
  let index = -1;
  function visibility() {
    button.classList.toggle('hidden', !data.lines.length);
    panel.classList.toggle('hidden', !wanted || !data.lines.length);
    player.classList.toggle('has-lyrics', wanted && data.lines.length > 0);
    button.setAttribute('aria-pressed', String(wanted));
  }
  function update() {
    if (!data.synced) return;
    const next = currentLyricIndex(data.lines, audio.currentTime);
    if (index === next) return;
    if (index >= 0) panel.children[index]?.removeAttribute('aria-current');
    index = next;
    const active = panel.children[index];
    if (active) {
      active.setAttribute('aria-current', 'true');
      if (wanted) panel.scrollTop = active.offsetTop - panel.clientHeight / 2 + active.clientHeight / 2;
    } else panel.scrollTop = 0;
  }
  button.addEventListener('click', () => {
    wanted = !wanted;
    visibility();
    index = -1;
    // Remove a previous highlight before recomputing after a hidden seek.
    for (const line of panel.children) line.removeAttribute('aria-current');
    update();
  });
  // Reading keys belong to this scroll region. Stop the library's document
  // shortcuts while preserving native arrow/page/home/end scrolling; playback
  // shortcuts (Space/p, horizontal seek, Escape) still reach the player.
  panel.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End'].includes(event.key)) {
      event.stopPropagation();
    } else if (event.key === 'j' || event.key === 'k') {
      event.preventDefault();
      event.stopPropagation();
      const step = panel.firstElementChild?.clientHeight || 24;
      panel.scrollTop += event.key === 'j' ? step : -step;
    }
  });
  for (const event of ['timeupdate', 'seeking', 'seeked', 'loadedmetadata']) audio.addEventListener(event, update);
  return {
    setMedia(media, streamUrl) {
      request?.abort();
      request = null;
      data = { synced: false, lines: [] };
      index = -1;
      panel.replaceChildren();
      panel.scrollTop = 0;
      visibility();
      const url = media?.type === 'audio' ? lyricsUrl(streamUrl) : null;
      if (!url) return;
      const controller = new AbortController();
      request = controller;
      fetch(url, { signal: controller.signal, credentials: 'same-origin', cache: 'no-store' })
        .then(response => response.ok ? response.json() : null)
        .then(result => {
          if (request !== controller) return;
          data = normalizeLyrics(result);
          const fragment = document.createDocumentFragment();
          for (const line of data.lines) {
            const row = document.createElement('p');
            row.textContent = line.text;
            fragment.appendChild(row);
          }
          panel.replaceChildren(fragment);
          visibility();
          update();
        }).catch(() => {}); // Lyrics never interrupt playback or raise a toast.
    },
  };
}
