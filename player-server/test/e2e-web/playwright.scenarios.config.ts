import { defineConfig } from '@playwright/test';
import webConfig from './playwright.config';

export default defineConfig({
  ...webConfig,
  testIgnore: [],
  timeout: 90000,
  use: { ...webConfig.use, baseURL: process.env.PLAYER_URL || "http://127.0.0.1:18081" },
  testMatch: /scenario-S\d+\.test\.ts$/,
});
