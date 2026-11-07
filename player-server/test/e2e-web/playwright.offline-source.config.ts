import { defineConfig, devices } from '@playwright/test';

// Serve actual source modules without requiring accounts or a media database.
export default defineConfig({
  testDir: './tests',
  testMatch: 'offline-source-identity.test.ts',
  workers: 1,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:18794' },
  webServer: {
    command: 'python3 -m http.server 18794 --bind 127.0.0.1 --directory ../../web',
    url: 'http://127.0.0.1:18794/js/offline-pin-library.js',
    reuseExistingServer: false,
  },
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
