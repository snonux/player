import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';
import { loadFixture } from './tests/helpers/music-fixture';

// Music suites (music-*.test.ts) against the disposable music fixture from
// player-android/integration_test/support/music_fixture.py. The origin comes
// from the fixture (PLAYER_MUSIC_FIXTURE), never from a deployed instance.
//
// Both projects drive installed browsers, as the music acceptance rules
// require: Google Chrome (codec-capable, unlike Playwright's Chromium) and
// the system Mozilla Firefox through Playwright's moz-firefox channel
// (PLAYER_FIREFOX overrides the binary). Playwright's bundled Firefox build
// does not count as Firefox verification.
//
// Both browsers keep their default autoplay policies: every play starts from
// trusted keyboard or pointer input, as it does for a user, so Firefox's
// suspended-AudioContext handling in audioVisuals.js is exercised, not
// switched off.
//
// PLAYER_E2E_PULSE_SINK routes the browsers' sound to a PulseAudio/PipeWire
// sink (e.g. a null sink) so a run does not play tones through the laptop
// speakers. The tests measure audio inside the page (helpers/audio-tap.ts),
// so the sink does not affect results.
const fixture = process.env.PLAYER_MUSIC_FIXTURE ? loadFixture() : null;
const sink = process.env.PLAYER_E2E_PULSE_SINK;
const env = sink ? { ...process.env, PULSE_SINK: sink } as Record<string, string> : undefined;

export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: /music-.*\.test\.ts$/,
  timeout: 120_000,
  reporter: [['list']],
  use: {
    ...base.use,
    baseURL: fixture?.origin,
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chrome',
      use: {
        ...devices['Desktop Chrome'],
        channel: 'chrome',
        launchOptions: { env },
      },
    },
    {
      name: 'firefox',
      use: {
        ...devices['Desktop Firefox'],
        channel: 'moz-firefox',
        launchOptions: {
          executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox',
          env,
        },
      },
    },
  ],
});
