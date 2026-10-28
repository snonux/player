import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';

const directory = process.env.PLAYER_PODCAST_FIXTURE;
if (!directory || !existsSync(join(directory, '.player-podcast-fixture'))) {
  throw new Error('Set PLAYER_PODCAST_FIXTURE to the marked podcast fixture');
}
const fixture = JSON.parse(readFileSync(join(directory, 'state.json'), 'utf8'));
const sink = process.env.PLAYER_E2E_PULSE_SINK;
const env = sink ? { ...process.env, PULSE_SINK: sink } as Record<string, string> : undefined;
export default defineConfig({
  ...base, testMatch: /podcast-(?:sync|discovery)\.test\.ts$/, testIgnore: [],
  workers: 1, fullyParallel: false, timeout: 120_000, reporter: [['list']],
  use: { ...base.use, baseURL: fixture.origin, screenshot: 'only-on-failure' },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome', launchOptions: { env } } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'], channel: 'moz-firefox',
      launchOptions: { executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox', env } } },
  ],
});
