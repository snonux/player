import assert from 'node:assert/strict';
import { GaplessTransport, decodeGaplessSong, GAPLESS_MAX_ENCODED_BYTES } from '../gapless-transport.js';
import { createAudioTransport } from '../audio-transport.js';

function context() {
  return { currentTime: 10, nodes: [], resume: async () => {},
    createGain: () => ({ gain: { setValueAtTime() {} }, connect() {}, disconnect() {} }),
    createBufferSource() {
      const node = { connect() {}, disconnect() {}, start(...args) { this.startArgs = args; }, stop() { this.stopped = true; } };
      this.nodes.push(node); return node;
    } };
}
const buffer = duration => ({ duration, length: duration * 48000, numberOfChannels: 2 });
function engine(ctx, boundary = () => true) {
  return new GaplessTransport({ context: ctx, input: {}, buffer: buffer(8), url: '/first', boundary });
}

{
  const ctx = context(), boundaries = [];
  const audio = engine(ctx, (...args) => { boundaries.push(args); return true; });
  audio.prepareNext({ buffer: buffer(7), url: '/second', media: { id: 2 }, token: 'occurrence-2' });
  await audio.play();
  assert.deepEqual(ctx.nodes.map(n => n.startArgs), [[10, 0], [18, 0]], 'successor starts at exact decoded boundary');
  ctx.currentTime = 18.125; ctx.nodes[0].onended();
  assert.equal(audio.currentTime, 0.125); assert.equal(audio.duration, 7);
  assert.deepEqual(boundaries, [['occurrence-2', { id: 2 }, 8]]);
  assert.equal(audio.paused, false); assert.equal(audio.src, '/second');
  audio.dispose();
}
{
  const ctx = context(), audio = engine(ctx);
  audio.prepareNext({ buffer: buffer(7), url: '/second' });
  await audio.play(); ctx.currentTime = 11;
  audio.currentTime = 6;
  assert.deepEqual(ctx.nodes.slice(-2).map(n => n.startArgs), [[11, 6], [13, 0]], 'seek replaces both scheduled nodes');
  assert(ctx.nodes.slice(0, 2).every(n => n.stopped));
  ctx.currentTime = 11.5; audio.pause();
  assert.equal(audio.currentTime, 6.5);
  ctx.currentTime = 20; await audio.play();
  assert.deepEqual(ctx.nodes.slice(-2).map(n => n.startArgs), [[20, 6.5], [21.5, 0]]);
  audio.cancelNext(); assert(ctx.nodes.at(-1).stopped, 'queue edit cancels scheduled successor');
  audio.dispose();
}
{
  const ctx = context(), audio = engine(ctx, () => false);
  audio.prepareNext({ buffer: buffer(7), url: '/stale' }); await audio.play();
  ctx.currentTime = 18; audio.promote();
  assert(audio.paused, 'stale ownership never continues prepared audio'); audio.dispose();
}
{
  const ctx = context(); let resume;
  ctx.resume = () => new Promise(resolve => { resume = resolve; });
  const audio = engine(ctx), pending = audio.play();
  audio.pause(); resume(); await pending;
  assert.equal(ctx.nodes.length, 0, 'pause cancels delayed autoplay'); audio.dispose();
}
{
  let cancelled = false;
  const response = { ok: true, headers: new Headers({ 'Content-Length': String(GAPLESS_MAX_ENCODED_BYTES + 1) }),
    body: { async cancel() { cancelled = true; } } };
  await assert.rejects(decodeGaplessSong({}, '/large', { fetchImpl: async () => response }), /32 MB/);
  assert(cancelled);
  await assert.rejects(decodeGaplessSong({ decodeAudioData: async () => buffer(100) }, '/pcm', {
    remaining: 10, fetchImpl: async () => new Response(new Uint8Array([1, 2])),
  }), /128 MB/);
}
{
  class Native extends EventTarget {
    volume = 0.6; muted = false; currentTime = 3; paused = true;
    getAttribute() { return '/native'; }
    pause() { this.paused = true; this.dispatchEvent(new Event('pause')); }
    async play() { this.paused = false; this.dispatchEvent(new Event('play')); }
  }
  const native = new Native(), router = createAudioTransport(native), ctx = context(), audio = engine(ctx);
  let plays = 0; router.addEventListener('play', () => plays++);
  await router.play(); assert.equal(plays, 1);
  router.use(audio); await router.play();
  assert.equal(plays, 2); assert.equal(router.volume, 0.6); assert.equal(native.currentTime, 3);
  assert.equal(router.getAttribute('src'), '/first');
  native.dispatchEvent(new Event('play')); assert.equal(plays, 2, 'idle native events cannot own decoded playback');
  router.use(null); assert.equal(router.currentTime, 3); assert(audio.disposed);
}
console.log('Gapless scheduling, seek, cancellation, bounds and transport checks passed.');
