import { defineConfig } from '@playwright/test';
import webConfig from './playwright.config';

export default defineConfig({
  ...webConfig,
  testIgnore: [],
  testMatch: /scenario-S\d+\.test\.ts$/,
});
