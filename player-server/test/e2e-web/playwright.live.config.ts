import { defineConfig, devices } from '@playwright/test';
import webConfig from './playwright.config';

// Live-deployment suite: runs against an already bootstrapped server (e.g. an
// f3s instance) with existing accounts supplied through the environment. It
// never calls /auth/bootstrap and uses no fixed passwords, unlike the default
// suite, so it is safe to point at an internet-facing instance.
//
// Google Chrome (not the bundled Chromium) is used on purpose: the bundled
// build ships without H.264/AAC/MP3 decoders, so it cannot tell which formats
// a real user's browser plays.
export default defineConfig({
  ...webConfig,
  testIgnore: [],
  testMatch: /.*\.live\.ts$/,
  timeout: 120_000,
  reporter: [['list'], ['json', { outputFile: process.env.LIVE_REPORT || 'live-results.json' }]],
  use: {
    ...webConfig.use,
    baseURL: process.env.PLAYER_URL,
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chrome',
      use: {
        ...devices['Desktop Chrome'],
        channel: 'chrome',
        launchOptions: { args: ['--autoplay-policy=no-user-gesture-required'] },
      },
    },
  ],
});
