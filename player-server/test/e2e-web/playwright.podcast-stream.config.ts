import { defineConfig } from '@playwright/test';
import base from './playwright.podcast-selection.config';
export default defineConfig({...base,testMatch:/podcast-stream-download\.test\.ts$/,testIgnore:[],timeout:90000});
