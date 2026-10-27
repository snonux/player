import { defineConfig, devices } from '@playwright/test';

// Web radio e2e: starts a fake Radio Browser (with live MP3 streams) and its
// own Player server, so it needs neither the network nor a running server.
//   npx playwright test -c playwright.radio.config.ts
// CHROMIUM_PATH points Chromium at a preinstalled browser binary when the
// Playwright download is unavailable.
const fakePort = 18190;
const playerPort = 18191;
const work = process.env.RADIO_E2E_DIR || '/tmp/player-radio-e2e';

export default defineConfig({
  testDir: './tests',
  testMatch: 'radio.test.ts',
  workers: 1,
  fullyParallel: false,
  reporter: 'list',
  timeout: 90_000,
  use: {
    baseURL: `http://127.0.0.1:${playerPort}`,
    serviceWorkers: 'block',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    actionTimeout: 10_000,
  },
  webServer: [
    {
      command: `node tests/helpers/fake-radio-browser.mjs ${fakePort}`,
      url: `http://127.0.0.1:${fakePort}/healthz`,
      reuseExistingServer: false,
    },
    {
      // Built and started from player-server/, which holds web/.
      command: `rm -rf ${work} && mkdir -p ${work}/media && go build -o ${work}/player ./cmd/player && ` +
        `PORT=${playerPort} MEDIA_ROOT=${work}/media DB_PATH=${work}/player.db SECURE_COOKIES=false ` +
        `RADIO_BROWSER_URL=http://127.0.0.1:${fakePort} RADIO_ALLOW_PRIVATE_NETWORKS=true ${work}/player`,
      cwd: '../..',
      url: `http://127.0.0.1:${playerPort}/healthz`,
      timeout: 180_000,
      reuseExistingServer: false,
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {},
      },
    },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
