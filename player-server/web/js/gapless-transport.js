// Schedule decoded songs on one AudioContext clock. No second HTML player or
// network-cache timing is used as a substitute for a continuous decoder path.
export const GAPLESS_MAX_ENCODED_BYTES = 32 * 1024 * 1024;
export const GAPLESS_MAX_PCM_BYTES = 128 * 1024 * 1024;
export const pcmBytes = buffer => buffer.length * buffer.numberOfChannels * 4;

export async function decodeGaplessSong(context, url, { signal, fetchImpl = fetch, remaining = GAPLESS_MAX_PCM_BYTES } = {}) {
  const response = await fetchImpl(url, { signal, credentials: 'same-origin' });
  if (!response.ok) throw Error(`Server answered ${response.status}`);
  if (Number(response.headers.get('Content-Length')) > GAPLESS_MAX_ENCODED_BYTES) {
    await response.body?.cancel();
    throw Error('Song exceeds the 32 MB gapless download limit');
  }
  const reader = response.body?.getReader();
  if (!reader) throw Error('Bounded downloads are unavailable');
  const chunks = []; let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > GAPLESS_MAX_ENCODED_BYTES) throw Error('Song exceeds the 32 MB gapless download limit');
      chunks.push(value);
    }
  } finally { await reader.cancel().catch(() => {}); }
  if (signal?.aborted) throw new DOMException('Cancelled', 'AbortError');
  const bytes = new Uint8Array(size); let at = 0;
  for (const chunk of chunks) { bytes.set(chunk, at); at += chunk.byteLength; }
  const buffer = await context.decodeAudioData(bytes.buffer);
  if (signal?.aborted) throw new DOMException('Cancelled', 'AbortError');
  if (pcmBytes(buffer) > remaining) throw Error('Songs exceed the 128 MB decoded gapless memory limit');
  return buffer;
}

export class GaplessTransport extends EventTarget {
  constructor({ context, input, buffer, url, boundary, changed = () => {}, status = () => {}, scheduleGain = () => {}, resetGain = () => {} }) {
    super();
    Object.assign(this, { context, input, buffer, src: url, currentSrc: url, boundary, changed, status, scheduleGain, resetGain });
    this.offset = 0; this.started = 0; this.node = null; this.next = null;
    this._paused = true; this._ended = false; this._volume = 1; this._muted = false;
    this.playCycle = 0; this.disposed = false;
    this.volumeNode = context.createGain(); this.volumeNode.connect(input);
    this.timer = setInterval(() => { this.promote(); if (!this.paused) this.emit('timeupdate'); }, 50);
  }
  emit(name) { this.dispatchEvent(new Event(name)); }
  get duration() { return this.buffer.duration; }
  get paused() { return this._paused; }
  get ended() { return this._ended; }
  get readyState() { return 4; }
  get networkState() { return 1; }
  get error() { return null; }
  get buffered() { return { length: 1, start: () => 0, end: () => this.duration }; }
  getAttribute(name) { return name === 'src' ? this.src : null; }
  get currentTime() { return Math.min(this.duration, this.offset + (this.paused ? 0 : Math.max(0, this.context.currentTime - this.started))); }
  set currentTime(value) {
    if (!Number.isFinite(value)) return;
    const playing = !this.paused;
    this.emit('seeking'); this.halt(); this.offset = Math.max(0, Math.min(this.duration, value)); this._ended = false;
    if (playing) this.start();
    this.emit('seeked'); this.emit('timeupdate');
  }
  get volume() { return this._volume; }
  set volume(value) { this._volume = value; this.updateVolume(); }
  get muted() { return this._muted; }
  set muted(value) { this._muted = value; this.updateVolume(); }
  updateVolume() { this.volumeNode.gain.setValueAtTime(this._muted ? 0 : this._volume, this.context.currentTime); this.emit('volumechange'); }
  get playbackRate() { return 1; }
  set playbackRate(value) { if (value !== 1) this.changed('Speed changes use normal streaming to preserve pitch.'); }
  async play() {
    const cycle = ++this.playCycle;
    await this.context.resume();
    if (this.disposed || cycle !== this.playCycle) return;
    if (!this.paused) return;
    if (this.ended) this.offset = 0;
    this._ended = false; this.start(); this.emit('play'); this.emit('playing');
  }
  pause() {
    this.playCycle++;
    if (this.paused) return;
    this.promote(); this.offset = this.currentTime; this.halt(); this._paused = true; this.emit('pause');
  }
  start() {
    this._paused = false; this.started = this.context.currentTime;
    this.node = this.makeNode(this.buffer, this.started, this.offset);
    this.scheduleNext();
  }
  makeNode(buffer, when, offset = 0) {
    const node = this.context.createBufferSource(); node.buffer = buffer; node.connect(this.volumeNode);
    node.onended = () => {
      if (this.node !== node || this.paused) return;
      if (this.promote()) return;
      this.offset = this.duration; this._paused = true; this._ended = true;
      this.emit('timeupdate'); this.emit('ended');
    };
    node.start(when, offset); return node;
  }
  prepareNext(next) {
    this.cancelNext(); this.next = next; this.scheduleNext();
    return !!this.next;
  }
  scheduleNext() {
    if (!this.next || this.paused) return;
    const when = this.started + this.duration - this.offset;
    // A missed deadline falls back to ordinary advance rather than overlapping
    // a late song with the current one or claiming a seamless transition.
    if (when <= this.context.currentTime + 0.02) {
      this.cancelNext(); this.status('Next song uses normal advance · preparation missed the boundary.'); return;
    }
    this.next.when = when;
    this.next.node = this.makeNode(this.next.buffer, when);
    this.scheduleGain(this.next.media, when);
  }
  promote() {
    const next = this.next;
    if (!next?.node || this.context.currentTime < next.when) return false;
    this.next = null;
    const old = { buffer: this.buffer, url: this.src };
    this.node?.disconnect();
    this.buffer = next.buffer; this.src = this.currentSrc = next.url;
    this.node = next.node; this.started = next.when; this.offset = 0;
    if (!this.boundary(next.token, next.media, old.buffer.duration)) {
      this.pause(); this.changed('Queue changed. Choose a track to continue.'); return true;
    }
    this.emit('loadedmetadata'); this.emit('timeupdate'); this.emit('playing');
    return true;
  }
  cancelNext() {
    if (this.next?.node) { this.next.node.onended = null; this.next.node.stop(); this.next.node.disconnect(); }
    this.next = null; this.resetGain();
  }
  halt() {
    if (this.node) { this.node.onended = null; this.node.stop(); this.node.disconnect(); this.node = null; }
    if (this.next?.node) { this.next.node.onended = null; this.next.node.stop(); this.next.node.disconnect(); this.next.node = null; }
    this.resetGain();
  }
  dispose() { this.disposed = true; this.playCycle++; this.halt(); this.next = null; this._paused = true; clearInterval(this.timer); this.volumeNode.disconnect(); }
}
