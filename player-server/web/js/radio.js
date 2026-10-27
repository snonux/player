// radio.js — web radio dialog: station explorer, auto explore, bookmarks
// and the live player. Station data and the relayed streams come from the
// server's /api/radio routes; see radio-core.js for the DOM-free parts.
import { API } from './api.js';
import { toast } from './utils.js';
import {
  AutoExplorer,
  RADIO_EXPLORE_BATCH,
  RADIO_PAGE_SIZE,
  RADIO_START_TIMEOUT_SECONDS,
  normalizeCountries,
  normalizeStations,
  normalizeStation,
  normalizeTags,
  stationDetails,
} from './radio-core.js';

const byId = (id) => document.getElementById(id);
const LIBRARY_PLAYERS = '#media-audio, #media-video';

// safeHomepage allows only http(s) links, so a station's homepage cannot
// become a javascript: URL.
export function safeHomepage(value) {
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : '';
  } catch {
    return '';
  }
}

export function initRadio() {
  const modal = byId('radio-modal');
  const audio = byId('radio-audio');
  if (!modal || !audio) return null;

  const el = {
    results: byId('radio-results'),
    resultsStatus: byId('radio-results-status'),
    more: byId('radio-more'),
    bookmarks: byId('radio-bookmarks'),
    bookmarksStatus: byId('radio-bookmarks-status'),
    form: byId('radio-search-form'),
    q: byId('radio-q'),
    tag: byId('radio-tag'),
    country: byId('radio-country'),
    order: byId('radio-order'),
    dwell: byId('radio-dwell'),
    now: byId('radio-now'),
    nowIcon: byId('radio-now-icon'),
    nowName: byId('radio-now-name'),
    nowDetails: byId('radio-now-details'),
    nowHomepage: byId('radio-now-homepage'),
    nowBookmark: byId('radio-now-bookmark'),
    status: byId('radio-status'),
    togglePlay: byId('radio-toggle-play'),
    exploreControls: byId('radio-explore-controls'),
    countdown: byId('radio-countdown'),
    explore: byId('radio-explore'),
    bar: byId('radio-bar'),
    barTitle: byId('radio-bar-title'),
    barStatus: byId('radio-bar-status'),
    barToggle: byId('radio-bar-toggle'),
  };

  let station = null; // the station loaded into the audio element
  let playing = false;
  let results = [];
  let offset = 0;
  let searchEpoch = 0;
  let facetsLoaded = false;
  const bookmarked = new Map(); // uuid -> station
  let bookmarksLoaded = false;

  const explorer = new AutoExplorer({
    fetchBatch: async (filters) => normalizeStations(await API.radioStations({
      ...filters, order: 'random', limit: RADIO_EXPLORE_BATCH,
    })),
    play: (next) => start(next, { exploring: true }),
    dwellSeconds: Number(el.dwell?.value) || 30,
    onTick: (remaining) => { el.countdown.textContent = `Next station in ${remaining} s`; },
    onChange: (exp, err) => {
      el.exploreControls.classList.toggle('hidden', !exp.active);
      el.explore.textContent = exp.active ? 'Restart auto explore' : 'Auto explore';
      if (exp.active) el.countdown.textContent = 'Tuning in…';
      if (err) {
        setStatus(err.message || 'Auto explore stopped');
        toast(err.message || 'Auto explore stopped', 'error');
      }
      render();
    },
  });

  // --- playback -----------------------------------------------------------

  let startTimer = null;
  function start(next, { exploring = false } = {}) {
    if (!exploring && explorer.active) explorer.keep();
    station = next;
    pauseLibrary();
    clearTimeout(startTimer);
    audio.src = next.stream_url;
    setStatus('Connecting…');
    playing = true;
    render();
    const attempt = audio.play();
    attempt?.catch?.((err) => {
      // A newer station replaced this one; its own play() reports.
      if (station !== next || err?.name === 'AbortError') return;
      failed(next, err?.name === 'NotAllowedError' ? 'Playback was blocked by the browser. Press Play.' : 'Station could not be played');
    });
    // Manual plays get the same start timeout as the explorer, so a dead
    // station does not leave "Connecting…" forever.
    if (!exploring) {
      startTimer = setTimeout(() => {
        if (station === next && audio.readyState < 3 && !audio.error) failed(next, 'Station did not respond');
      }, RADIO_START_TIMEOUT_SECONDS * 1000);
    }
  }

  function failed(which, message) {
    if (which !== station) return;
    if (explorer.active) {
      setStatus(`${which.name} is unavailable, skipping…`);
      explorer.failed(which.uuid);
      return;
    }
    stopAudio();
    setStatus(message);
    toast(`${which.name}: ${message}`, 'error');
  }

  // stopAudio ends the stream. A live stream cannot be paused meaningfully,
  // and dropping the source closes the server relay.
  function stopAudio() {
    clearTimeout(startTimer);
    playing = false;
    audio.pause();
    audio.removeAttribute('src');
    audio.load();
    render();
  }

  function stop() {
    if (explorer.active) explorer.keep();
    stopAudio();
    setStatus('Stopped');
  }

  function togglePlayback() {
    if (playing) stop();
    else if (station) start(station);
  }

  function pauseLibrary() {
    document.querySelectorAll(LIBRARY_PLAYERS).forEach((m) => { if (!m.paused) m.pause(); });
  }

  audio.addEventListener('playing', () => {
    if (!station) return;
    clearTimeout(startTimer);
    setStatus(explorer.active ? 'Exploring' : 'Playing');
    explorer.playing(station.uuid);
  });
  audio.addEventListener('waiting', () => { if (playing) setStatus('Buffering…'); });
  audio.addEventListener('error', () => {
    // Removing the source on stop fires an error without a station change.
    if (!playing || !station || !audio.getAttribute('src')) return;
    failed(station, 'Station could not be played');
  });
  // Library playback (audio or video) takes over: stop the radio.
  document.addEventListener('play', (e) => {
    if (e.target !== audio && e.target.matches?.(LIBRARY_PLAYERS) && playing) stop();
  }, true);

  // --- rendering ----------------------------------------------------------

  function setStatus(message) {
    el.status.textContent = message;
    el.barStatus.textContent = message;
  }

  function icon(target, s) {
    target.replaceChildren();
    if (s?.favicon_url) {
      const img = document.createElement('img');
      img.alt = '';
      img.loading = 'lazy';
      img.src = s.favicon_url;
      img.addEventListener('error', () => { img.replaceWith(placeholder()); });
      target.appendChild(img);
    } else {
      target.appendChild(placeholder());
    }
  }

  function placeholder() {
    const span = document.createElement('span');
    span.className = 'radio-icon-placeholder';
    span.textContent = '📻︎';
    return span;
  }

  function isBookmarked(s) {
    return bookmarked.has(s.uuid) || (!bookmarksLoaded && s.bookmarked);
  }

  function row(s) {
    const item = document.createElement('li');
    item.className = 'radio-row';
    item.dataset.uuid = s.uuid;
    const current = station?.uuid === s.uuid && playing;
    item.classList.toggle('playing', current);

    const play = document.createElement('button');
    play.type = 'button';
    play.className = 'btn btn-ghost radio-play';
    play.setAttribute('aria-label', `${current ? 'Playing' : 'Play'} ${s.name}`);
    if (current) play.setAttribute('aria-current', 'true');
    const iconWrap = document.createElement('span');
    iconWrap.className = 'radio-icon';
    iconWrap.setAttribute('aria-hidden', 'true');
    icon(iconWrap, s);
    const textWrap = document.createElement('span');
    textWrap.className = 'radio-row-text';
    const name = document.createElement('span');
    name.className = 'radio-row-name';
    name.textContent = s.name;
    const details = document.createElement('span');
    details.className = 'radio-row-details text-muted text-xs';
    details.textContent = stationDetails(s);
    textWrap.append(name, details);
    play.append(iconWrap, textWrap);
    play.addEventListener('click', () => start(s));

    const mark = document.createElement('button');
    mark.type = 'button';
    mark.className = 'icon-btn radio-bookmark';
    const on = isBookmarked(s);
    mark.classList.toggle('active', on);
    mark.textContent = on ? '★' : '☆';
    mark.setAttribute('aria-pressed', String(on));
    mark.setAttribute('aria-label', `${on ? 'Remove bookmark for' : 'Bookmark'} ${s.name}`);
    mark.addEventListener('click', () => toggleBookmark(s));

    item.append(play, mark);
    return item;
  }

  function renderList(list, stations) {
    // Keep the keyboard position when a list is redrawn.
    const focused = document.activeElement?.closest?.('.radio-row');
    const focusUuid = list.contains(focused) ? focused.dataset.uuid : null;
    const focusBookmark = document.activeElement?.classList?.contains('radio-bookmark');
    list.replaceChildren(...stations.map(row));
    if (focusUuid) {
      const again = [...list.children].find((li) => li.dataset.uuid === focusUuid);
      again?.querySelector(focusBookmark ? '.radio-bookmark' : '.radio-play')?.focus();
    }
  }

  function render() {
    renderList(el.results, results);
    renderList(el.bookmarks, [...bookmarked.values()]);
    if (bookmarksLoaded) {
      el.bookmarksStatus.textContent = bookmarked.size ? '' : 'No bookmarked stations yet. Use ☆ next to a station to add one.';
    }
    const has = Boolean(station);
    el.now.classList.toggle('hidden', !has);
    el.bar.classList.toggle('hidden', !has || !playing);
    document.body.classList.toggle('radio-active', has && playing);
    if (!has) return;
    icon(el.nowIcon, station);
    el.nowName.textContent = station.name;
    el.nowDetails.textContent = stationDetails(station);
    const homepage = safeHomepage(station.homepage);
    el.nowHomepage.classList.toggle('hidden', !homepage);
    if (homepage) el.nowHomepage.href = homepage;
    else el.nowHomepage.removeAttribute('href');
    const on = isBookmarked(station);
    el.nowBookmark.textContent = on ? '★ Bookmarked' : '☆ Bookmark';
    el.nowBookmark.setAttribute('aria-pressed', String(on));
    el.togglePlay.textContent = playing ? 'Stop' : 'Play';
    el.barToggle.textContent = playing ? 'Stop' : 'Play';
    el.barTitle.textContent = station.name;
  }

  // --- data ---------------------------------------------------------------

  function filters() {
    return {
      q: el.q.value,
      tag: el.tag.value,
      country: el.country.value,
      order: el.order.value,
    };
  }

  async function search({ append = false } = {}) {
    const epoch = ++searchEpoch;
    if (!append) offset = 0;
    el.resultsStatus.textContent = 'Loading stations…';
    el.more.classList.add('hidden');
    try {
      const page = normalizeStations(await API.radioStations({ ...filters(), limit: RADIO_PAGE_SIZE, offset }));
      if (epoch !== searchEpoch) return;
      const known = new Set(append ? results.map((s) => s.uuid) : []);
      results = append ? results.concat(page.filter((s) => !known.has(s.uuid))) : page;
      offset += RADIO_PAGE_SIZE;
      el.resultsStatus.textContent = results.length ? `${results.length} stations` : 'No stations match these filters.';
      el.more.classList.toggle('hidden', page.length < RADIO_PAGE_SIZE);
      render();
    } catch (err) {
      if (epoch !== searchEpoch) return;
      el.resultsStatus.textContent = `Could not load stations: ${err.message || 'error'}`;
    }
  }

  async function loadFacets() {
    if (facetsLoaded) return;
    facetsLoaded = true;
    const [tags, countries] = await Promise.allSettled([API.radioTags(), API.radioCountries()]);
    if (tags.status === 'fulfilled') {
      try {
        el.tag.append(...normalizeTags(tags.value).map((t) => option(t.name, `${t.name} (${t.station_count})`)));
      } catch { /* keep "Any genre" */ }
    }
    if (countries.status === 'fulfilled') {
      try {
        el.country.append(...normalizeCountries(countries.value).map((c) => option(c.code, `${c.name} (${c.station_count})`)));
      } catch { /* keep "Any country" */ }
    }
    if (tags.status === 'rejected' && countries.status === 'rejected') facetsLoaded = false;
  }

  function option(value, label) {
    const o = document.createElement('option');
    o.value = value;
    o.textContent = label;
    return o;
  }

  async function loadBookmarks() {
    el.bookmarksStatus.textContent = 'Loading bookmarks…';
    try {
      const list = normalizeStations(await API.radioBookmarks());
      bookmarked.clear();
      list.forEach((s) => bookmarked.set(s.uuid, { ...s, bookmarked: true }));
      bookmarksLoaded = true;
      el.bookmarksStatus.textContent = '';
      render();
    } catch (err) {
      el.bookmarksStatus.textContent = `Could not load bookmarks: ${err.message || 'error'}`;
    }
  }

  const pendingBookmarks = new Set();
  async function toggleBookmark(s) {
    if (pendingBookmarks.has(s.uuid)) return;
    pendingBookmarks.add(s.uuid);
    const on = isBookmarked(s);
    try {
      if (on) {
        await API.unbookmarkStation(s.uuid);
        bookmarked.delete(s.uuid);
        toast(`Removed bookmark: ${s.name}`);
      } else {
        const saved = normalizeStation(await API.bookmarkStation(s.uuid)) || s;
        // Newest first, like the server's list.
        const rest = [...bookmarked.values()];
        bookmarked.clear();
        bookmarked.set(saved.uuid, { ...saved, bookmarked: true });
        rest.forEach((b) => bookmarked.set(b.uuid, b));
        toast(`Bookmarked: ${s.name}`);
      }
      s.bookmarked = !on;
      results.forEach((r) => { if (r.uuid === s.uuid) r.bookmarked = !on; });
      if (station?.uuid === s.uuid) station.bookmarked = !on;
      bookmarksLoaded = true;
      render();
    } catch (err) {
      toast(err.message || 'Bookmark failed', 'error');
    } finally {
      pendingBookmarks.delete(s.uuid);
    }
  }

  // --- dialog -------------------------------------------------------------

  function showTab(name) {
    for (const tab of ['explore', 'bookmarks']) {
      const selected = tab === name;
      byId(`radio-tab-${tab}`).classList.toggle('active', selected);
      byId(`radio-tab-${tab}`).setAttribute('aria-selected', String(selected));
      byId(`radio-panel-${tab}`).classList.toggle('hidden', !selected);
    }
    if (name === 'bookmarks') loadBookmarks();
  }

  let opened = false;
  function open() {
    modal.classList.add('open');
    if (!opened) {
      opened = true;
      loadFacets();
      search();
      loadBookmarks();
    }
  }
  function close() { modal.classList.remove('open'); }

  byId('radio-toggle')?.addEventListener('click', open);
  byId('radio-bar-open')?.addEventListener('click', open);
  byId('radio-close')?.addEventListener('click', close);
  modal.addEventListener('click', (e) => { if (e.target === modal) close(); });
  byId('radio-tab-explore')?.addEventListener('click', () => showTab('explore'));
  byId('radio-tab-bookmarks')?.addEventListener('click', () => showTab('bookmarks'));
  el.form.addEventListener('submit', (e) => { e.preventDefault(); search(); });
  for (const select of [el.tag, el.country, el.order]) select.addEventListener('change', () => search());
  el.more.addEventListener('click', () => search({ append: true }));
  el.togglePlay.addEventListener('click', togglePlayback);
  el.barToggle.addEventListener('click', togglePlayback);
  el.nowBookmark.addEventListener('click', () => { if (station) toggleBookmark(station); });
  el.explore.addEventListener('click', () => {
    setStatus('Starting auto explore…');
    explorer.setDwell(Number(el.dwell.value));
    explorer.start(filters());
  });
  el.dwell.addEventListener('change', () => explorer.setDwell(Number(el.dwell.value)));
  byId('radio-explore-keep')?.addEventListener('click', () => {
    explorer.keep();
    setStatus(playing ? 'Playing' : 'Stopped');
  });
  byId('radio-explore-next')?.addEventListener('click', () => explorer.next());
  byId('radio-explore-stop')?.addEventListener('click', stop);

  // j/k and the arrow keys move between station rows; the dialog otherwise
  // owns letter keys through keyboard-access.js, which leaves j/k alone.
  modal.addEventListener('keydown', (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
    const delta = { j: 1, ArrowDown: 1, k: -1, ArrowUp: -1 }[e.key];
    if (!delta) return;
    const current = e.target.closest?.('.radio-row');
    if (!current) {
      if (e.target.matches?.('input, select, textarea') || (e.key !== 'j' && e.key !== 'k')) return;
      const list = el.bookmarks.closest('.radio-panel').classList.contains('hidden') ? el.results : el.bookmarks;
      const first = list.querySelector('.radio-play');
      if (first) { e.preventDefault(); first.focus(); }
      return;
    }
    const rows = [...current.parentElement.children];
    const next = rows[rows.indexOf(current) + delta];
    e.preventDefault();
    next?.querySelector(e.target.classList.contains('radio-bookmark') ? '.radio-bookmark' : '.radio-play')?.focus();
  });

  return {
    open,
    toggle: () => (modal.classList.contains('open') ? close() : open()),
    stop,
    isPlaying: () => playing,
    explorer,
  };
}
