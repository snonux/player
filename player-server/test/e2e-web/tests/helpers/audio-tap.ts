/**
 * audio-tap.ts — capture what the page actually sends to its audio output.
 *
 * installAudioTap() runs before any page script (page.addInitScript). It
 * wraps AudioNode.prototype.connect so that every node the page connects to
 * an AudioContext's destination is also connected to an AudioWorklet "tap"
 * in the same context. The tap records the mixed signal that reaches the
 * speakers, after every gain/EQ node the page inserts — which is what
 * ReplayGain (M24) and equalizer (M25) checks must measure. The tap writes
 * nothing to its own output, so it adds silence and never changes what is
 * heard.
 *
 * Why a worklet: it runs on the audio rendering thread and sees every render
 * quantum, stamped with the context's frame counter. (A ScriptProcessor runs
 * on the main thread; under load it lost whole 4096-sample blocks, which
 * looked exactly like playback gaps.) Each posted chunk carries its first
 * frame, so any discontinuity in the stream is detected and reported as a
 * dropout. A capture with dropouts is invalid: callers must not read gaps
 * from it (see InvalidCapture in music-ui.ts).
 *
 * The tap also stamps media element events (loadstart, playing, ended) with
 * the same clock, so a gap can be located relative to a track change.
 *
 * Scope and limits:
 * - Player routes audio through Web Audio when audio visuals are on (the
 *   default) and the page has user activation, i.e. after a click or key
 *   press. With visuals Off the element plays directly and nothing is
 *   tapped: export() then reports routed=false and the caller must treat
 *   the capture as a gap rather than as silence.
 *
 * writeWav() and analyze() hand the samples to the shared Python analyser
 * (player-android/integration_test/support/audio_trace.py) so browser and
 * Android captures are measured identically.
 */
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { SUPPORT_DIR } from './music-fixture';

export interface MediaEvent { type: string; src: string; offset_s: number }

export interface Capture {
  sampleRate: number; routed: boolean; dropouts: number; samples: string; events: MediaEvent[];
}

export interface Segment {
  start_s: number; end_s: number; duration_s: number; rms_dbfs: number; peak_dbfs: number;
  tone_hz: number | null; bands_dbfs?: Record<string, number>;
}

export interface Gap { start_s: number; duration_ms: number; kind: 'silence' | 'quiet' }

export interface Analysis {
  sample_rate: number; duration_s: number; segments: Segment[]; gaps: Gap[]; gaps_ms: number[];
  audible_s: number; discontinuities?: { time_s: number; size: number }[];
}

/**
 * Browser-side tap; serialised into the page by addInitScript. It must stay
 * one self-contained function: Playwright sends only its source text, so it
 * cannot call helpers defined outside it.
 */
export function installAudioTap(): void {
  const w = window as unknown as { __playerAudioTap?: unknown };
  if (w.__playerAudioTap || typeof AudioNode === 'undefined' || typeof AudioWorkletNode === 'undefined') return;
  // Runs on the audio thread: copies each render quantum (mono mix) into a
  // buffer and posts it with the frame number of its first sample.
  const processor = `class PlayerTap extends AudioWorkletProcessor {
    constructor() {
      super(); this.on = false; this.buffer = new Float32Array(8192); this.used = 0; this.first = 0;
      this.port.onmessage = (event) => {
        this.flush();
        if (event.data === 'start') this.on = true;
        if (event.data === 'stop') { this.on = false; this.port.postMessage({ stopped: true }); }
      };
    }
    flush() {
      if (this.used) this.port.postMessage({ frame: this.first, data: this.buffer.slice(0, this.used) });
      this.used = 0;
    }
    process(inputs) {
      if (!this.on) return true;
      const channels = inputs[0] || [];
      const length = channels.length ? channels[0].length : 128;
      if (this.used === 0) this.first = currentFrame;
      for (let i = 0; i < length; i++) {
        let sum = 0;
        for (const channel of channels) sum += channel[i];
        this.buffer[this.used++] = channels.length ? sum / channels.length : 0;
      }
      if (this.used + length > this.buffer.length) this.flush();
      return true;
    }
  }
  registerProcessor('player-tap', PlayerTap);`;
  const moduleURL = URL.createObjectURL(new Blob([processor], { type: 'application/javascript' }));
  const taps = new Map<BaseAudioContext, Promise<AudioWorkletNode>>();
  const chunks: { frame: number; data: Float32Array }[] = [];
  const events: { type: string; src: string; frame: number }[] = [];
  let tapNode: AudioWorkletNode | null = null;
  let tapContext: BaseAudioContext | null = null;
  let stopped: (() => void) | null = null;
  const nativeConnect = AudioNode.prototype.connect as (...args: unknown[]) => unknown;

  function tapFor(context: BaseAudioContext): Promise<AudioWorkletNode> {
    let tap = taps.get(context);
    if (!tap) {
      tap = context.audioWorklet.addModule(moduleURL).then(() => {
        const node = new AudioWorkletNode(context, 'player-tap');
        node.port.onmessage = (event) => {
          if (event.data.stopped) { stopped?.(); return; }
          chunks.push(event.data);
        };
        // A worklet node is rendered while it is connected towards the destination.
        nativeConnect.call(node, context.destination);
        tapNode = node;
        tapContext = context;
        return node;
      });
      taps.set(context, tap);
    }
    return tap;
  }

  AudioNode.prototype.connect = function (this: AudioNode, destination: unknown, ...rest: unknown[]) {
    const result = nativeConnect.call(this, destination, ...rest);
    if (destination instanceof AudioDestinationNode) {
      tapFor(this.context).then((node) => nativeConnect.call(this, node)).catch(() => {});
    }
    return result;
  } as typeof AudioNode.prototype.connect;

  // Media events do not bubble; listen in the capture phase on the document.
  for (const type of ['loadstart', 'playing', 'ended']) {
    document.addEventListener(type, (event) => {
      const element = event.target as HTMLMediaElement;
      if (!tapContext || !(element instanceof HTMLMediaElement)) return;
      events.push({ type, src: (element.currentSrc || element.src || '').replace(location.origin, ''),
        frame: Math.round(tapContext.currentTime * tapContext.sampleRate) });
    }, true);
  }

  w.__playerAudioTap = {
    start() { chunks.length = 0; events.length = 0; tapNode?.port.postMessage('start'); return Boolean(tapNode); },
    routed() { return Boolean(tapNode); },
    async export() {
      if (!tapNode || !tapContext) return { sampleRate: 0, routed: false, dropouts: 0, samples: '', events: [] };
      await new Promise<void>((resolve) => { stopped = resolve; tapNode!.port.postMessage('stop'); });
      const rate = tapContext.sampleRate;
      if (!chunks.length) return { sampleRate: rate, routed: true, dropouts: 0, samples: '', events: [] };
      const first = chunks[0].frame;
      const last = chunks[chunks.length - 1];
      const pcm = new Int16Array(last.frame - first + last.data.length);
      let expected = first;
      let dropouts = 0;
      for (const chunk of chunks) {
        if (chunk.frame !== expected) dropouts++;
        const offset = chunk.frame - first;
        for (let i = 0; i < chunk.data.length; i++) {
          pcm[offset + i] = Math.max(-32768, Math.min(32767, Math.round(chunk.data[i] * 32767)));
        }
        expected = chunk.frame + chunk.data.length;
      }
      const bytes = new Uint8Array(pcm.buffer);
      let binary = '';
      for (let i = 0; i < bytes.length; i += 0x8000) {
        binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
      }
      return { sampleRate: rate, routed: true, dropouts, samples: btoa(binary),
        events: events.map((e) => ({ type: e.type, src: e.src, offset_s: (e.frame - first) / rate })) };
    },
  };
}

/** Start recording from now on (clears earlier samples). False when nothing is routed yet. */
export async function startCapture(page: Page): Promise<boolean> {
  return page.evaluate(() => (window as any).__playerAudioTap.start());
}

/** Stop recording and fetch the samples and media events. */
export async function takeCapture(page: Page): Promise<Capture> {
  return page.evaluate(() => (window as any).__playerAudioTap.export());
}

/** Write a capture as 16-bit mono PCM WAV; returns the path. */
export function writeWav(capture: Capture, path: string): string {
  const data = Buffer.from(capture.samples, 'base64');
  const header = Buffer.alloc(44);
  header.write('RIFF', 0);
  header.writeUInt32LE(36 + data.length, 4);
  header.write('WAVEfmt ', 8);
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20); // PCM
  header.writeUInt16LE(1, 22); // mono
  header.writeUInt32LE(capture.sampleRate, 24);
  header.writeUInt32LE(capture.sampleRate * 2, 28);
  header.writeUInt16LE(2, 32);
  header.writeUInt16LE(16, 34);
  header.write('data', 36);
  header.writeUInt32LE(data.length, 40);
  writeFileSync(path, Buffer.concat([header, data]));
  return path;
}

/** Run audio_trace.py on a WAV file; bands are frequencies in Hz, tone a pure tone to check. */
export function analyze(wav: string, bands: number[] = [], tone?: number): Analysis {
  const args = [join(SUPPORT_DIR, 'audio_trace.py'), wav];
  if (bands.length) args.push('--bands', bands.join(','));
  if (tone) args.push('--tone', String(tone));
  return JSON.parse(execFileSync('python3', args, { encoding: 'utf8' }));
}

/** Fail unless independently analysed audio on each side has the expected tone. */
export function analyzeTransition(wav: string, endedAt: number, tone: number): Record<'before' | 'after', Segment> {
  return JSON.parse(execFileSync('python3', [join(SUPPORT_DIR, 'audio_trace.py'), wav,
    '--transition-at', String(endedAt), '--tone', String(tone)], { encoding: 'utf8' }));
}
