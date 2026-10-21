/**
 * audio-tap.ts — capture what the page actually sends to its audio output.
 *
 * installAudioTap() runs before any page script (page.addInitScript). It
 * wraps AudioNode.prototype.connect so that every node the page connects to
 * an AudioContext's destination is also connected to a ScriptProcessor "tap"
 * in the same context. The tap records the mixed signal that reaches the
 * speakers, after every gain/EQ node the page inserts — which is what
 * ReplayGain (M24) and equalizer (M25) checks must measure. The tap writes
 * nothing to its own output, so it adds silence and never changes what is
 * heard.
 *
 * Scope and limits:
 * - Player routes audio through Web Audio when audio visuals are on (the
 *   default) and the page has user activation, i.e. after a click or key
 *   press. With visuals Off the element plays directly and nothing is
 *   tapped: export() then reports routed=false and the caller must treat
 *   the capture as a gap rather than as silence.
 * - Timing comes from each block's AudioContext playbackTime, so blocks the
 *   main thread delivered late are placed correctly; blocks that never
 *   arrived are counted as dropouts and left as zeros.
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

export interface Capture { sampleRate: number; routed: boolean; dropouts: number; samples: string }

export interface Segment {
  start_s: number; end_s: number; duration_s: number; rms_dbfs: number; peak_dbfs: number;
  tone_hz: number | null; bands_dbfs?: Record<string, number>;
}

export interface Analysis { sample_rate: number; duration_s: number; segments: Segment[]; gaps_ms: number[]; audible_s: number }

/**
 * Browser-side tap; serialised into the page by addInitScript. It must stay
 * one self-contained function: Playwright sends only its source text, so it
 * cannot call helpers defined outside it.
 */
export function installAudioTap(): void {
  const w = window as unknown as { __playerAudioTap?: unknown };
  if (w.__playerAudioTap || typeof AudioNode === 'undefined') return;
  const taps = new Map<BaseAudioContext, ScriptProcessorNode>();
  const blocks: { t: number; data: Float32Array }[] = [];
  let recording = false;
  let sampleRate = 0;
  const nativeConnect = AudioNode.prototype.connect as (...args: unknown[]) => unknown;

  function tapFor(context: BaseAudioContext): ScriptProcessorNode {
    let tap = taps.get(context);
    if (tap) return tap;
    tap = context.createScriptProcessor(4096, 2, 1);
    tap.onaudioprocess = (event) => {
      if (!recording) return;
      const input = event.inputBuffer;
      const mono = new Float32Array(input.length);
      for (let c = 0; c < input.numberOfChannels; c++) {
        const channel = input.getChannelData(c);
        for (let i = 0; i < channel.length; i++) mono[i] += channel[i] / input.numberOfChannels;
      }
      sampleRate = input.sampleRate;
      blocks.push({ t: event.playbackTime, data: mono });
    };
    // A ScriptProcessor only runs while connected towards the destination.
    nativeConnect.call(tap, context.destination);
    taps.set(context, tap);
    return tap;
  }

  AudioNode.prototype.connect = function (this: AudioNode, destination: unknown, ...rest: unknown[]) {
    const result = nativeConnect.call(this, destination, ...rest);
    if (destination instanceof AudioDestinationNode) nativeConnect.call(this, tapFor(this.context));
    return result;
  } as typeof AudioNode.prototype.connect;

  w.__playerAudioTap = {
    start() { blocks.length = 0; recording = true; },
    stop() { recording = false; },
    routed() { return taps.size > 0; },
    export() {
      recording = false;
      if (!blocks.length) return { sampleRate, routed: taps.size > 0, dropouts: 0, samples: '' };
      const t0 = blocks[0].t;
      const last = blocks[blocks.length - 1];
      const total = Math.round((last.t - t0) * sampleRate) + last.data.length;
      const pcm = new Int16Array(total);
      let expected = 0;
      let dropouts = 0;
      for (const block of blocks) {
        const offset = Math.round((block.t - t0) * sampleRate);
        if (offset > expected + 1) dropouts++;
        for (let i = 0; i < block.data.length && offset + i < total; i++) {
          pcm[offset + i] = Math.max(-32768, Math.min(32767, Math.round(block.data[i] * 32767)));
        }
        expected = offset + block.data.length;
      }
      const bytes = new Uint8Array(pcm.buffer);
      let binary = '';
      for (let i = 0; i < bytes.length; i += 0x8000) {
        binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
      }
      return { sampleRate, routed: true, dropouts, samples: btoa(binary) };
    },
  };
}

/** Start recording from now on (clears earlier samples). */
export async function startCapture(page: Page): Promise<void> {
  await page.evaluate(() => (window as any).__playerAudioTap.start());
}

/** Stop recording and fetch the samples. */
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

/** Run audio_trace.py on a WAV file; bands are frequencies in Hz. */
export function analyze(wav: string, bands: number[] = []): Analysis {
  const args = [join(SUPPORT_DIR, 'audio_trace.py'), wav];
  if (bands.length) args.push('--bands', bands.join(','));
  return JSON.parse(execFileSync('python3', args, { encoding: 'utf8' }));
}
