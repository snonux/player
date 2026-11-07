import { readFileSync } from 'node:fs';
import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';
const fixture = process.env.PLAYER_PODCAST_FIXTURE ? JSON.parse(readFileSync(`${process.env.PLAYER_PODCAST_FIXTURE}/state.json`, 'utf8')) : {};
export default defineConfig({ ...base, testMatch: /podcast-retention\.test\.ts$/, testIgnore: [], reporter: [['list']], use: { ...base.use, baseURL: process.env.PLAYER_URL || fixture.origin || 'http://127.0.0.1:18834' }, projects: [
 { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
 { name: 'firefox', use: { ...devices['Desktop Firefox'], channel: 'moz-firefox', launchOptions: { executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox' } } },
] });
