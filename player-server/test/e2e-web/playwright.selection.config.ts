import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
  testDir: './tests', testMatch: /media-multiselect\.test\.ts$/, workers: 1,
  reporter: 'list', use: { headless: true, hasTouch: true },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], hasTouch: true, channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'], hasTouch: true, channel: 'moz-firefox', launchOptions: { executablePath: process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox' } } },
  ],
});
