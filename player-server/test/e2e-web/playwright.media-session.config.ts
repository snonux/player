import { defineConfig } from '@playwright/test';
import music from './playwright.music.config';

// MPRIS requires a real OS session bus and installed headful browsers. Serial
// execution makes each launched process/profile and exported session unambiguous.
export default defineConfig({
  ...music,
  testMatch: /music-media-session\.test\.ts$/,
  workers: 1,
  fullyParallel: false,
  timeout: 180_000,
  use: { ...music.use, headless: false },
});
