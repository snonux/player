import { defineConfig, devices } from '@playwright/test';

// Mutating trash suite for the isolated k8s/test-trash instance only. Both
// browsers run by default because every web feature must work in Chrome and
// Firefox; pick one with --project. A single worker keeps the projects serial,
// since they share the instance's admin account, set and trash.
export default defineConfig({
  testDir: './tests',
  testMatch: 'trash.live.ts',
  workers: 1,
  fullyParallel: false,
  reporter: 'list',
  use: {
    baseURL: process.env.PLAYER_URL || 'http://127.0.0.1:18083',
    serviceWorkers: 'block',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
