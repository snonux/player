import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';

// These tests serve local production assets and synthetic audio through routes;
// they need neither a backend nor a media library.
export default defineConfig(base, {
  testMatch: 'audio-visuals.test.ts',
  reporter: 'list',
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
