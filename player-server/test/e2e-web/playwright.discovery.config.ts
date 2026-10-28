import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';

const directory = process.env.PLAYER_DISCOVERY_FIXTURE || process.env.PLAYER_PODCAST_FIXTURE;
if (!directory || !['.player-music-fixture', '.player-podcast-fixture'].some(marker => existsSync(join(directory, marker)))) {
  throw new Error('Set PLAYER_DISCOVERY_FIXTURE to a marked, disposable music or podcast fixture');
}
const fixture = JSON.parse(readFileSync(join(directory, 'state.json'), 'utf8'));
export default defineConfig({
  ...base, testMatch: /podcast-discovery\.test\.ts$/, testIgnore: [],
  workers: 1, fullyParallel: false, timeout: 120_000, reporter: [['list']],
  use: { ...base.use, baseURL: fixture.origin, screenshot: 'only-on-failure' },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'], channel: 'moz-firefox',
      launchOptions: { executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox' } } },
  ],
});
