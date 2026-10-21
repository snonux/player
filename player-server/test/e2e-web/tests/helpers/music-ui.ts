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
import { analyze, startCapture, takeCapture, writeWav, type Analysis } from './audio-tap';

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

/** Record the page's audio output for ms milliseconds, save it and analyse it. */
export async function captureOutput(page: Page, ms: number, wavPath: string, bands: number[] = []):
  Promise<{ analysis: Analysis; routed: boolean; dropouts: number; wav: string }> {
  await startCapture(page);
  await page.waitForTimeout(ms);
  const capture = await takeCapture(page);
  if (!capture.routed || !capture.samples) {
    return { analysis: { sample_rate: 0, duration_s: 0, segments: [], gaps_ms: [], audible_s: 0 },
      routed: capture.routed, dropouts: capture.dropouts, wav: '' };
  }
  const wav = writeWav(capture, wavPath);
  return { analysis: analyze(wav, bands), routed: true, dropouts: capture.dropouts, wav };
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
