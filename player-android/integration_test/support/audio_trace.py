#!/usr/bin/env python3
"""Analyse captured audio output for timing, level and spectrum checks.

One analyser serves all three platforms: the web runner writes the samples
it tapped from the browser's Web Audio output to a WAV file, and the Android
runner records the emulator's output from a host PulseAudio/PipeWire null
sink. Both then call analyze() (or this script) and get the same JSON.

What it reports, per non-silent segment of the capture:

- start/end time, so a gap between two segments (track change) is measured
  in milliseconds - the basis for gapless checks (M21);
- RMS and peak level in dBFS, so two captures can be compared for gain
  (ReplayGain, M24);
- the tone frequency (zero-crossing estimate, for the single-tone fixture
  tracks) to identify *which* fixture track was audible;
- optional Goertzel band levels at given frequencies, for equalizer checks
  against the fixture's three-tone probe (M25).

Only the standard library is used so the analyser runs wherever the runners
do. Input must be 16-bit PCM WAV; stereo is downmixed to mono.

Usage: audio_trace.py FILE.wav [--bands 100,1000,8000] [--threshold -50]
"""

import argparse
from array import array
import json
import math
import sys
import wave

FRAME_MS = 10
# Below this RMS level a frame counts as silence. Most fixture tones are at
# -15 dBFS RMS; the quietest are the EQ probe (-26 dBFS RMS) and the quiet
# ReplayGain track (-24 dBFS RMS). -50 dBFS leaves room for both while staying
# far above dither and codec noise.
SILENCE_DB = -50.0
# Ignore blips shorter than this (codec priming, click at a seek).
MIN_SEGMENT_MS = 30
# Edges are skipped when measuring tone and level: fades and decoder
# start-up would bias both.
EDGE_MS = 50
MEASURE_MAX_S = 2.0
FLOOR_DB = -120.0


def read_wav(path):
    """Return (sample_rate, mono samples as floats in [-1, 1])."""
    with wave.open(str(path), 'rb') as file:
        if file.getsampwidth() != 2:
            raise ValueError(f'{path}: only 16-bit PCM WAV is supported')
        channels, rate = file.getnchannels(), file.getframerate()
        raw = array('h')
        raw.frombytes(file.readframes(file.getnframes()))
    if sys.byteorder == 'big':
        raw.byteswap()
    if channels == 1:
        return rate, [v / 32768.0 for v in raw]
    return rate, [sum(raw[i:i + channels]) / (32768.0 * channels)
                  for i in range(0, len(raw) - channels + 1, channels)]


def to_db(value):
    return max(FLOOR_DB, 20 * math.log10(value)) if value > 0 else FLOOR_DB


def frame_levels(samples, rate, frame_ms=FRAME_MS):
    """RMS level in dBFS of each consecutive frame."""
    size = max(1, rate * frame_ms // 1000)
    return [to_db(math.sqrt(sum(x * x for x in samples[i:i + size]) / size))
            for i in range(0, len(samples) - size + 1, size)]


def find_segments(levels, frame_ms=FRAME_MS, threshold=SILENCE_DB, min_ms=MIN_SEGMENT_MS):
    """Runs of frames above threshold as [(first_frame, end_frame)]."""
    runs, start = [], None
    for index, level in enumerate(levels + [FLOOR_DB]):
        if level > threshold and start is None:
            start = index
        elif level <= threshold and start is not None:
            if (index - start) * frame_ms >= min_ms:
                runs.append((start, index))
            start = None
    return runs


def zero_crossing_hz(samples, rate):
    """Frequency of a single sine from its positive-going zero crossings.

    Linear interpolation between samples keeps the estimate within a fraction
    of a hertz for the fixture's pure tones; meaningless for mixtures.
    """
    crossings = []
    for i in range(1, len(samples)):
        a, b = samples[i - 1], samples[i]
        if a < 0 <= b and b != a:
            crossings.append(i - 1 + (-a / (b - a)))
    if len(crossings) < 3:
        return None
    return (len(crossings) - 1) * rate / (crossings[-1] - crossings[0])


def goertzel_db(samples, rate, frequency):
    """Level in dBFS (sine peak amplitude) of one frequency component."""
    n = len(samples)
    if n == 0:
        return FLOOR_DB
    coefficient = 2 * math.cos(2 * math.pi * frequency / rate)
    previous = before = 0.0
    for x in samples:
        previous, before = x + coefficient * previous - before, previous
    power = previous * previous + before * before - coefficient * previous * before
    return to_db(2 * math.sqrt(max(power, 0.0)) / n)


def describe_segment(samples, rate, run, bands, frame_ms=FRAME_MS):
    first, end = run
    size = rate * frame_ms // 1000
    start, stop = first * size, end * size
    edge = rate * EDGE_MS // 1000
    body = samples[start + edge:stop - edge] or samples[start:stop]
    middle = len(body) // 2
    half = int(min(len(body), rate * MEASURE_MAX_S) // 2)
    window = body[middle - half:middle + half]
    rms = math.sqrt(sum(x * x for x in window) / len(window)) if window else 0.0
    tone = zero_crossing_hz(window, rate)
    result = {
        'start_s': round(start / rate, 3), 'end_s': round(stop / rate, 3),
        'duration_s': round((stop - start) / rate, 3),
        'rms_dbfs': round(to_db(rms), 2),
        'peak_dbfs': round(to_db(max((abs(x) for x in window), default=0.0)), 2),
        'tone_hz': round(tone, 1) if tone else None,
    }
    if bands:
        result['bands_dbfs'] = {str(f): round(goertzel_db(window, rate, f), 2) for f in bands}
    return result


def analyze_samples(samples, rate, bands=None, threshold=SILENCE_DB):
    levels = frame_levels(samples, rate)
    runs = find_segments(levels, threshold=threshold)
    segments = [describe_segment(samples, rate, run, bands) for run in runs]
    gaps = [round((b['start_s'] - a['end_s']) * 1000) for a, b in zip(segments, segments[1:])]
    return {
        'sample_rate': rate, 'duration_s': round(len(samples) / rate, 3),
        'silence_threshold_dbfs': threshold, 'frame_ms': FRAME_MS,
        'segments': segments, 'gaps_ms': gaps,
        'audible_s': round(sum(s['duration_s'] for s in segments), 3),
    }


def analyze(path, bands=None, threshold=SILENCE_DB):
    rate, samples = read_wav(path)
    return analyze_samples(samples, rate, bands, threshold)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    parser.add_argument('wav')
    parser.add_argument('--bands', default='', help='comma-separated frequencies in Hz')
    parser.add_argument('--threshold', type=float, default=SILENCE_DB)
    args = parser.parse_args(argv)
    bands = [float(b) for b in args.bands.split(',') if b.strip()]
    json.dump(analyze(args.wav, bands, args.threshold), sys.stdout, indent=1)
    print()


if __name__ == '__main__':
    main()
