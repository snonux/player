import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';
export default defineConfig({
  ...base, testMatch: /podcast-continuation\.test\.ts$/, testIgnore: [],
  timeout: 90_000, reporter: [['list']],
  use: { ...base.use, baseURL: process.env.PLAYER_URL },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
