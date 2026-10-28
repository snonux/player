import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';

const directory = process.env.PLAYER_DISCOVERY_FIXTURE;
if (!directory || !existsSync(join(directory, '.player-music-fixture'))) throw new Error('Set PLAYER_DISCOVERY_FIXTURE to a marked disposable music fixture');
const fixture = JSON.parse(readFileSync(join(directory, 'state.json'), 'utf8'));
export default defineConfig({
  ...base, testMatch: /image-gestures\.test\.ts$/, testIgnore: [], workers: 1,
  reporter: 'list', use: { ...base.use, baseURL: fixture.origin, hasTouch: true, viewport: { width: 390, height: 844 } },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], hasTouch: true, viewport: { width: 390, height: 844 }, channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'], hasTouch: true, viewport: { width: 390, height: 844 }, channel: 'moz-firefox', launchOptions: { executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox' } } },
  ],
});
