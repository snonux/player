// radio-core.js — DOM-free web radio logic: query building, response
// validation and the auto explorer. Kept separate from radio.js so the Node
// unit tests can drive it without a browser.

export const RADIO_ORDERS = ['popular', 'votes', 'name', 'random'];
export const RADIO_DWELL_CHOICES = [15, 30, 60];
export const RADIO_PAGE_SIZE = 50;
// How long a station may take to start before the explorer skips it.
export const RADIO_START_TIMEOUT_SECONDS = 10;
// Stations fetched per auto explore batch.
export const RADIO_EXPLORE_BATCH = 20;

// radioSearchQuery builds the query string for GET /api/radio/stations.
// Empty values are left out so the server applies its defaults.
export function radioSearchQuery({ q = '', tag = '', country = '', order = '', limit, offset } = {}) {
  const params = new URLSearchParams();
  if (String(q).trim()) params.set('q', String(q).trim());
  if (tag) params.set('tag', tag);
  if (country) params.set('country', country);
  if (order && RADIO_ORDERS.includes(order)) params.set('order', order);
  if (Number.isSafeInteger(limit) && limit > 0) params.set('limit', String(Math.min(limit, 100)));
  if (Number.isSafeInteger(offset) && offset > 0) params.set('offset', String(offset));
  const s = params.toString();
  return s ? `?${s}` : '';
}

const text = (value) => (typeof value === 'string' ? value : '');
const count = (value) => (Number.isFinite(value) && value > 0 ? Math.floor(value) : 0);

// normalizeStation checks one Station object from the server and returns a
// copy with every field in its expected type, or null when the station
// cannot be shown or played (no uuid, name or stream URL).
export function normalizeStation(value) {
  if (!value || typeof value !== 'object') return null;
  const uuid = text(value.uuid).trim();
  const streamUrl = text(value.stream_url);
  if (!uuid || !streamUrl.startsWith('/')) return null;
  const favicon = text(value.favicon_url);
  return {
    uuid,
    name: text(value.name).trim() || 'Unnamed station',
    homepage: text(value.homepage),
    favicon_url: favicon.startsWith('/') ? favicon : '',
    stream_url: streamUrl,
    country: text(value.country),
    country_code: text(value.country_code),
    language: text(value.language),
    tags: Array.isArray(value.tags) ? value.tags.filter((t) => typeof t === 'string' && t.trim()).map((t) => t.trim()) : [],
    codec: text(value.codec),
    bitrate: count(value.bitrate),
    votes: count(value.votes),
    bookmarked: value.bookmarked === true,
    bookmarked_at: text(value.bookmarked_at),
  };
}

// normalizeStations validates a station list response. Entries that cannot
// be played are dropped, duplicates (same uuid) keep their first position.
export function normalizeStations(value) {
  if (!Array.isArray(value)) throw new Error('Invalid station list');
  const seen = new Set();
  const stations = [];
  for (const entry of value) {
    const station = normalizeStation(entry);
    if (!station || seen.has(station.uuid)) continue;
    seen.add(station.uuid);
    stations.push(station);
  }
  return stations;
}

// normalizeTags and normalizeCountries validate the filter option lists.
export function normalizeTags(value) {
  if (!Array.isArray(value)) throw new Error('Invalid tag list');
  return value.filter((t) => t && typeof t.name === 'string' && t.name.trim())
    .map((t) => ({ name: t.name.trim(), station_count: count(t.station_count) }));
}

export function normalizeCountries(value) {
  if (!Array.isArray(value)) throw new Error('Invalid country list');
  return value.filter((c) => c && typeof c.code === 'string' && /^[A-Za-z]{2}$/.test(c.code))
    .map((c) => ({ code: c.code.toUpperCase(), name: text(c.name).trim() || c.code.toUpperCase(), station_count: count(c.station_count) }));
}

// stationDetails is the one-line summary shown under a station name.
export function stationDetails(station) {
  const parts = [];
  if (station.country) parts.push(station.country);
  const format = [station.codec, station.bitrate ? `${station.bitrate} kbit/s` : ''].filter(Boolean).join(' ');
  if (format) parts.push(format);
  if (station.tags.length) parts.push(station.tags.slice(0, 3).join(', '));
  return parts.join(' · ');
}

// AutoExplorer plays stations of one filter in random order, moving on
// after `dwellSeconds` of playback. The UI reports playback with playing()
// and failed(); a station that does not start within the start timeout is
// skipped. Every async step is tied to a generation, so stop() or a new
// start() makes late results of an earlier run harmless.
export class AutoExplorer {
  constructor({
    fetchBatch,
    play,
    onTick = () => {},
    onChange = () => {},
    dwellSeconds = 30,
    startTimeoutSeconds = RADIO_START_TIMEOUT_SECONDS,
    setTimer = (fn, ms) => setTimeout(fn, ms),
    clearTimer = (id) => clearTimeout(id),
  }) {
    this.fetchBatch = fetchBatch;
    this.play = play;
    this.onTick = onTick;
    this.onChange = onChange;
    this.dwellSeconds = dwellSeconds;
    this.startTimeoutSeconds = startTimeoutSeconds;
    this.setTimer = setTimer;
    this.clearTimer = clearTimer;
    this.active = false;
    this.current = null;
    this.remaining = 0;
    this.generation = 0;
    this.queue = [];
    this.seen = new Set();
    // Stations that failed to start in this run are not tried again.
    this.broken = new Set();
    this.timer = null;
    this.counting = false;
  }

  start(filters = {}) {
    this.halt();
    this.error = null;
    this.active = true;
    this.filters = { ...filters };
    this.queue = [];
    this.seen = new Set();
    this.broken = new Set();
    this.onChange(this);
    return this.next();
  }

  // next skips to the following station (the Next button, a failed start,
  // or the end of the dwell time).
  async next() {
    if (!this.active) return;
    const generation = ++this.generation;
    this.clearTimeout();
    this.counting = false;
    if (!this.queue.length) {
      let batch;
      try {
        batch = await this.fetchBatch(this.filters);
      } catch (err) {
        if (generation === this.generation) this.finish(err);
        return;
      }
      if (generation !== this.generation || !this.active) return;
      const playable = batch.filter((s) => !this.broken.has(s.uuid));
      let fresh = playable.filter((s) => !this.seen.has(s.uuid));
      if (!fresh.length) {
        if (!playable.length) {
          this.finish(new Error(batch.length ? 'No station of these filters could be played' : 'No stations match these filters'));
          return;
        }
        // Every station of the filter was heard: start over.
        this.seen.clear();
        fresh = playable.filter((s) => s.uuid !== this.current?.uuid);
        if (!fresh.length) fresh = playable;
      }
      this.queue = fresh;
    }
    const station = this.queue.shift();
    this.seen.add(station.uuid);
    this.current = station;
    this.remaining = this.dwellSeconds;
    this.onChange(this);
    this.timer = this.setTimer(() => {
      if (generation === this.generation) this.failed(station.uuid);
    }, this.startTimeoutSeconds * 1000);
    this.play(station);
  }

  // playing starts the dwell countdown once the current station sounds.
  playing(uuid) {
    if (!this.active || this.counting || uuid !== this.current?.uuid) return;
    this.clearTimeout();
    this.counting = true;
    this.remaining = this.dwellSeconds;
    this.onTick(this.remaining, this);
    this.schedule(this.generation);
  }

  schedule(generation) {
    this.timer = this.setTimer(() => {
      if (generation !== this.generation || !this.active) return;
      this.remaining -= 1;
      if (this.remaining <= 0) {
        this.next();
        return;
      }
      this.onTick(this.remaining, this);
      this.schedule(generation);
    }, 1000);
  }

  // failed skips the current station at once.
  failed(uuid) {
    if (!this.active || uuid !== this.current?.uuid) return;
    this.broken.add(uuid);
    this.next();
  }

  setDwell(seconds) {
    if (!RADIO_DWELL_CHOICES.includes(seconds)) return;
    this.dwellSeconds = seconds;
    if (this.counting && this.remaining > seconds) {
      this.remaining = seconds;
      this.onTick(this.remaining, this);
    }
  }

  // keep ends exploring; the current station keeps playing.
  keep() {
    if (!this.active) return;
    this.halt();
    this.onChange(this);
  }

  // halt stops timers and the run without notifying (used before restarts).
  halt() {
    this.active = false;
    this.generation++;
    this.clearTimeout();
    this.counting = false;
    this.queue = [];
  }

  finish(err) {
    this.halt();
    this.error = err;
    this.onChange(this, err);
  }

  clearTimeout() {
    if (this.timer !== null) this.clearTimer(this.timer);
    this.timer = null;
  }
}
