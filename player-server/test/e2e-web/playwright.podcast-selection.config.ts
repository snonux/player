import { defineConfig, devices } from '@playwright/test';
import base from './playwright.config';
export default defineConfig({
  ...base, testMatch: /podcast-multiselect\.test\.ts$/, testIgnore: [], reporter: [['list']],
  projects: [
    {name:'chrome', use:{...devices['Desktop Chrome'], channel:'chrome'}},
    {name:'firefox', use:{...devices['Desktop Firefox'], channel:'moz-firefox', launchOptions:{executablePath:process.env.PLAYER_FIREFOX || '/usr/lib64/firefox/firefox'}}},
  ],
});
