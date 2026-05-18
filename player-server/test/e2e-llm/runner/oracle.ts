/**
 * oracle.ts — Screenshot oracle for LLM e2e visual checks (Layer 5).
 *
 * Exports checkScreenshot(pngPath, question) which sends a PNG to Claude
 * Haiku with a yes/no question and returns true when the answer starts with
 * "yes" (case-insensitive).
 *
 * The oracle is gated behind the LLM_E2E_SCREENSHOTS=true env var.  When
 * that variable is absent or set to any other value the function always
 * returns true so CI runs that don't set the variable skip visual checks
 * silently rather than failing or burning API credits.
 *
 * When screenshots are enabled, ANTHROPIC_API_KEY must be present in the
 * environment or the function throws immediately.
 *
 * The system prompt is cache-controlled so that repeated calls within the
 * same run benefit from prompt-caching (≥1024 tokens threshold on Haiku 4.5;
 * the system block here is short, but the cache_control marker is cheap to
 * add and costs nothing when the threshold isn't reached).
 *
 * Used only for S03 (upload-verify-web) and S04 (share-link-round-trip).
 * Estimated cost: ~$0.003 per call at Haiku 4.5 pricing.
 */

import * as fs from 'fs';
import Anthropic from '@anthropic-ai/sdk';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// The model used for visual checks. Haiku is chosen for cost efficiency.
const HAIKU_MODEL = 'claude-haiku-4-5';

// Maximum tokens for the yes/no answer (plus a one-sentence reason).
const MAX_TOKENS = 64;

// System prompt shared across all oracle calls within a run.
// Marked as ephemeral so repeated calls can read it from the cache.
const SYSTEM_PROMPT = 'You are a visual test oracle. Answer every question with a single word: yes or no. Optionally add one short sentence of reasoning after the answer.';

// ---------------------------------------------------------------------------
// Singleton Anthropic client (created lazily when screenshots are enabled)
// ---------------------------------------------------------------------------

let _client: Anthropic | null = null;

/**
 * getClient returns the Anthropic SDK client, constructing it on first use.
 * Throws if ANTHROPIC_API_KEY is not set, so callers learn immediately
 * rather than receiving a cryptic 401 later.
 */
function getClient(): Anthropic {
  if (_client) return _client;

  const apiKey = process.env['ANTHROPIC_API_KEY'];
  if (!apiKey) {
    throw new Error(
      '[oracle] ANTHROPIC_API_KEY is not set. ' +
      'Set it before enabling LLM_E2E_SCREENSHOTS=true.',
    );
  }

  _client = new Anthropic({ apiKey });
  return _client;
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * checkScreenshot sends a PNG file to Claude Haiku with a yes/no question
 * and returns true when the model answers "yes" (case-insensitive prefix
 * match).
 *
 * Returns true immediately (without calling the API) when LLM_E2E_SCREENSHOTS
 * is not "true", so the oracle is effectively a no-op in environments that
 * don't opt in.
 *
 * @param pngPath  Absolute or relative path to the PNG screenshot file.
 * @param question A yes/no question about the screenshot content, e.g.
 *                 "Is there a media card visible in the grid?"
 * @returns        true when Haiku answers yes or when screenshots are disabled.
 */
export async function checkScreenshot(
  pngPath: string,
  question: string,
): Promise<boolean> {
  // Gate: skip visual check when the feature flag is not enabled.
  if (process.env['LLM_E2E_SCREENSHOTS'] !== 'true') {
    console.log(`[oracle] screenshots disabled — skipping visual check: "${question}"`);
    return true;
  }

  const client = getClient();

  // Read the PNG and base64-encode it for the API.
  const imageBytes = fs.readFileSync(pngPath);
  const imageData = imageBytes.toString('base64');

  console.log(`[oracle] checking screenshot "${pngPath}": "${question}"`);

  const response = await client.messages.create({
    model: HAIKU_MODEL,
    max_tokens: MAX_TOKENS,
    // Cache the system prompt so repeated calls within the same run
    // benefit from prompt-caching (no cost penalty when threshold not met).
    system: [
      {
        type: 'text',
        text: SYSTEM_PROMPT,
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'image',
            source: {
              type: 'base64',
              media_type: 'image/png',
              data: imageData,
            },
          },
          {
            type: 'text',
            text: `Does this screenshot show ${question}? Answer yes or no.`,
          },
        ],
      },
    ],
  });

  // Extract the text from the first content block.
  const firstBlock = response.content[0];
  if (!firstBlock || firstBlock.type !== 'text') {
    console.warn('[oracle] unexpected response structure — treating as failure');
    return false;
  }

  const answer = firstBlock.text.trim().toLowerCase();
  const passed = answer.startsWith('yes');

  console.log(`[oracle] answer: "${firstBlock.text.trim()}" → ${passed ? 'PASS' : 'FAIL'}`);
  return passed;
}
