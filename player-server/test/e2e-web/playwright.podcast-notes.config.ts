import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
  testDir: './tests', testMatch: 'podcast-player-notes.test.ts', workers: 1, reporter: 'list', timeout: 60000,
  use: { baseURL: 'http://127.0.0.1:18392', serviceWorkers: 'block', screenshot: 'only-on-failure' },
  webServer: {command: 'python3 -m http.server 18392 --bind 127.0.0.1 --directory ../../web', url: 'http://127.0.0.1:18392/index.html', reuseExistingServer: false},
  projects: [
    {name:'chrome',use:{...devices['Desktop Chrome'],channel:'chrome'}},
    {name:'firefox',use:{...devices['Desktop Firefox']}},
  ],
});
