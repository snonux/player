import { defineConfig, devices } from '@playwright/test';

// Base URL for the running player server. Override with the PLAYER_URL env var
// when testing against a non-default address or port.
const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';

export default defineConfig({
  // Look for test files only inside the tests/ subdirectory.
  testDir: './tests',

  // Run each test file in its own isolated context. Tests within the same
  // file share a browser context by default (serial execution per file).
  fullyParallel: false,

  // Fail the CI run on test.only left in source.
  forbidOnly: !!process.env.CI,

  // Retry once on CI to absorb transient timing flakes.
  retries: process.env.CI ? 1 : 0,

  // Single worker: the tests mutate shared server state (bootstrap, user
  // accounts) so parallelism across workers would cause races.
  workers: 1,

  reporter: [['list'], ['html', { open: 'never' }]],

  // Allow up to 120 s for the global beforeAll hook which triggers a media
  // rescan and waits for at least one set to appear in the database.
  globalTimeout: 120_000,

  use: {
    baseURL,

    // Headless by default; PWDEBUG=1 or --headed enables the browser window.
    headless: true,

    // Capture a screenshot on failure for easier CI debugging.
    screenshot: 'only-on-failure',

    // Record a trace on the first retry to capture the failure timeline.
    trace: 'on-first-retry',

    // Generous navigation timeout for the Go server to respond on CI.
    navigationTimeout: 15_000,
    actionTimeout: 10_000,
  },

  projects: [
    {
      // Run against Chromium only. The smoke suite exercises application
      // logic, not cross-browser compatibility. Add Firefox/WebKit when
      // cross-browser coverage is needed.
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
