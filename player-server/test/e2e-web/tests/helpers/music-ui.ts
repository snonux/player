/**
 * music-ui.ts — small, reusable steps of the music journeys.
 *
 * Steps find controls by their accessible names ("Set Music", "Folder …",
 * file-name card labels, "Search media") and read visible state (selection,
 * media element, dialogs), never screen coordinates, so a restyled page does
 * not break the suite while a renamed control does — as it should.
 */
import { expect, type Page } from '@playwright/test';
import type { Account } from './music-fixture';
import { analyze, startCapture, takeCapture, writeWav, type Analysis, type MediaEvent } from './audio-tap';

export interface AudioState { src: string; time: number; paused: boolean; volume: number; selected: string; active: string }

/** Media element, selection and focus as the user would perceive them. */
export async function audioState(page: Page): Promise<AudioState> {
  return page.evaluate(() => {
    const audio = document.getElementById('media-audio') as HTMLAudioElement;
    const selected = document.querySelector('#media-grid .selected') as HTMLElement | null;
    const active = document.activeElement as HTMLElement | null;
    return {
      src: (audio.currentSrc || '').replace(location.origin, ''),
      time: audio.currentTime,
      paused: audio.paused,
      volume: audio.volume,
      selected: selected?.getAttribute('aria-label') || selected?.dataset.name || '',
      active: active && active !== document.body
        ? `${active.tagName.toLowerCase()}${active.id ? '#' + active.id : ''} "${active.getAttribute('aria-label') || ''}"`
        : 'body',
    };
  });
}

/** Sign in through the login form by keyboard only (Tab between fields). */
export async function keyboardLogin(page: Page, account: Account): Promise<void> {
  await page.goto('/login.html');
  await expect(page.locator('#username')).toBeFocused();
  await page.keyboard.type(account.username);
  await page.keyboard.press('Tab');
  await page.keyboard.type(account.password);
  await page.keyboard.press('Enter');
  await page.waitForURL((url) => url.pathname === '/');
}

/** Move the grid selection with `l` until the card labelled name is selected. */
export async function selectByKeys(page: Page, name: string, maxSteps = 30): Promise<void> {
  for (let step = 0; step < maxSteps; step++) {
    if ((await audioState(page)).selected === name) return;
    await page.keyboard.press('l');
  }
  throw new Error(`Selection never reached "${name}"`);
}

/** Wait until the element plays the given media id and its time advances. */
export async function expectPlaying(page: Page, mediaId: number): Promise<AudioState> {
  await expect.poll(async () => (await audioState(page)).src, { timeout: 20_000 })
    .toMatch(new RegExp(`/media/${mediaId}/(stream|compat)`));
  const before = (await audioState(page)).time;
  await expect.poll(async () => (await audioState(page)).time, { timeout: 10_000 }).toBeGreaterThan(before + 0.5);
  const state = await audioState(page);
  expect(state.paused).toBe(false);
  return state;
}

/**
 * A capture that cannot be measured: nothing routed to the tap, or a
 * discontinuity in the captured frames. Journeys record it as a gap, never as
 * measured data (a lost block of samples looks exactly like a playback gap).
 */
export class InvalidCapture extends Error {}

export interface CaptureResult {
  analysis: Analysis; routed: boolean; dropouts: number; wav: string; events: MediaEvent[];
}

/**
 * Record the page's audio output for ms milliseconds, save and analyse it.
 * Waits briefly for the page to route audio through Web Audio first (the
 * app creates its graph once playback has started). tone enables the
 * analyser's pure-tone discontinuity check.
 */
export async function captureOutput(page: Page, ms: number, wavPath: string, bands: number[] = [], tone?: number):
  Promise<CaptureResult> {
  await expect.poll(() => page.evaluate(() => (window as any).__playerAudioTap.routed()), { timeout: 5_000 })
    .toBe(true).catch(() => {});
  await startCapture(page);
  await page.waitForTimeout(ms);
  const capture = await takeCapture(page);
  if (!capture.routed || !capture.samples) {
    return { analysis: { sample_rate: 0, duration_s: 0, segments: [], gaps: [], gaps_ms: [], audible_s: 0 },
      routed: capture.routed, dropouts: capture.dropouts, wav: '', events: [] };
  }
  const wav = writeWav(capture, wavPath);
  return { analysis: analyze(wav, bands, tone), routed: true, dropouts: capture.dropouts, wav, events: capture.events };
}

/** Throw InvalidCapture unless the capture is routed and free of dropouts. */
export function assertValidCapture(out: CaptureResult): void {
  if (!out.routed) throw new InvalidCapture('page audio was not routed through Web Audio; nothing captured');
  if (out.dropouts > 0) throw new InvalidCapture(`capture lost frames (${out.dropouts} discontinuities); gaps cannot be measured`);
}

/** The longest audible segment of a capture, or undefined when silent. */
export function mainSegment(analysis: Analysis) {
  return [...analysis.segments].sort((a, b) => b.duration_s - a.duration_s)[0];
}

/** Open a folder of a set directly by URL (harness navigation, not a journey). */
export async function openFolder(page: Page, setId: number, folder: string): Promise<void> {
  await page.goto(`/?set=${setId}&folder=${encodeURIComponent(folder)}`);
  await expect(page.locator('#media-grid [role="button"]').first()).toBeVisible();
}

/**
 * Forget saved progress so the item starts at 0:00. Player resumes audio
 * where the account left off; a short fixture track left near its end by an
 * earlier run would otherwise finish (and advance) before it is measured.
 */
export async function resetProgress(page: Page, mediaId: number): Promise<void> {
  const response = await page.request.post('/api/v1/progress/status', { data: { media_id: mediaId, status: 'not_started' } });
  expect(response.ok(), `reset progress of media ${mediaId}`).toBe(true);
}

/**
 * Play a card from 0:00 with the mouse: hover it and click its Play (▶)
 * button. (A double-click would also play, but Playwright's double-click
 * reaches system Firefox over WebDriver BiDi as two single clicks.)
 * The ▶ button is found by its data-action and title: inside a card with
 * role="button" the accessibility tree treats children as presentational,
 * so role queries cannot see it.
 */
export async function playCard(page: Page, fileName: string, mediaId: number): Promise<AudioState> {
  await resetProgress(page, mediaId);
  const card = page.getByRole('button', { name: fileName, exact: true });
  await card.hover();
  await card.locator('button[data-action="play"][title="Play"]').click();
  return expectPlaying(page, mediaId);
}
