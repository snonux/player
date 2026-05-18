/**
 * oracle.ts — Screenshot oracle for LLM e2e visual checks (Layer 5).
 *
 * Sends a PNG screenshot to an Ollama vision model via the OpenAI-compatible
 * chat completions API and returns true when the model answers "yes" to a
 * yes/no question about the image content.
 *
 * Model: llama3.2-vision (Meta, 11B parameters). This is the strongest vision
 * model available in the Ollama ecosystem for screenshot/UI analysis tasks.
 *
 * Configuration (environment variables):
 *   OLLAMA_BASE_URL     Base URL of the Ollama cloud API (default: https://ollama.com).
 *                       Override to point at a local Ollama instance or a different
 *                       hosted endpoint.
 *   OLLAMA_API_KEY      Bearer token from ollama.com. Required when using the cloud
 *                       endpoint; optional for local unauthenticated Ollama instances.
 *   OLLAMA_MODEL        Vision model name (default: llama3.2-vision). Override to use
 *                       a different model available on the target Ollama endpoint.
 *   LLM_E2E_SCREENSHOTS Set to "true" to enable visual checks. When absent the
 *                       function returns true immediately so CI runs that omit
 *                       this flag skip visual checks silently rather than failing.
 *
 * Used only for S03 (upload-verify-web) and S04 (share-link-round-trip).
 */

import * as fs from 'fs';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Default vision model on the Ollama cloud (ollama.com). qwen3-vl is Alibaba's
// instruction-tuned vision-language model — the strongest option available for
// screenshot and UI analysis tasks. Override with OLLAMA_MODEL for a different
// model (e.g. a locally-pulled model when using a local Ollama instance).
const OLLAMA_MODEL = process.env['OLLAMA_MODEL'] ?? 'qwen3-vl:235b-instruct';

// Maximum tokens for the yes/no answer plus a one-sentence reason.
const MAX_TOKENS = 64;

// Ollama cloud API endpoint. Override with OLLAMA_BASE_URL for local instances.
const DEFAULT_BASE_URL = 'https://ollama.com';

const SYSTEM_PROMPT =
  'You are a visual test oracle. Answer every question with a single word: yes or no. ' +
  'Optionally add one short sentence of reasoning after the answer.';

// ---------------------------------------------------------------------------
// Ollama API types (OpenAI-compatible subset used here)
// ---------------------------------------------------------------------------

interface OllamaChoice {
  message: { content: string };
}

interface OllamaResponse {
  choices: OllamaChoice[];
}

// ---------------------------------------------------------------------------
// API call
// ---------------------------------------------------------------------------

/**
 * callOllamaVision posts a base64-encoded PNG and a question to the Ollama
 * OpenAI-compatible chat completions endpoint and returns the raw text reply.
 *
 * Throws on HTTP errors or when the response does not contain a text answer
 * so callers surface failures clearly rather than treating them as "yes".
 */
async function callOllamaVision(imageData: string, question: string): Promise<string> {
  const baseURL = (process.env['OLLAMA_BASE_URL'] ?? DEFAULT_BASE_URL).replace(/\/+$/, '');
  const apiKey = process.env['OLLAMA_API_KEY'] ?? '';

  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  // Include Authorization only when a key is configured; local Ollama does not
  // require authentication and will reject unknown headers on some builds.
  if (apiKey) {
    headers['Authorization'] = `Bearer ${apiKey}`;
  }

  const res = await fetch(`${baseURL}/v1/chat/completions`, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      model: OLLAMA_MODEL,
      max_tokens: MAX_TOKENS,
      messages: [
        { role: 'system', content: SYSTEM_PROMPT },
        {
          role: 'user',
          content: [
            // Ollama vision models accept images as OpenAI-style image_url
            // blocks with a data URI carrying the base64-encoded PNG.
            { type: 'image_url', image_url: { url: `data:image/png;base64,${imageData}` } },
            { type: 'text', text: `Does this screenshot show ${question}? Answer yes or no.` },
          ],
        },
      ],
    }),
  });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`[oracle] Ollama API error ${res.status}: ${body}`);
  }

  const data = (await res.json()) as OllamaResponse;
  const content = data.choices?.[0]?.message?.content;
  if (!content) {
    throw new Error('[oracle] unexpected Ollama response: missing choices[0].message.content');
  }
  return content;
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * checkScreenshot reads a PNG file, sends it to the Ollama vision model with
 * a yes/no question, and returns true when the model answers "yes".
 *
 * Returns true immediately (without calling Ollama) when LLM_E2E_SCREENSHOTS
 * is not set to "true" — this makes the oracle a no-op in environments that
 * don't opt in, with no API calls and no cost.
 *
 * @param pngPath  Absolute or relative path to the PNG screenshot file.
 * @param question A yes/no question about the screenshot, e.g.
 *                 "Is there a media card visible in the grid?"
 */
export async function checkScreenshot(pngPath: string, question: string): Promise<boolean> {
  if (process.env['LLM_E2E_SCREENSHOTS'] !== 'true') {
    console.log(`[oracle] screenshots disabled — skipping visual check: "${question}"`);
    return true;
  }

  const imageData = fs.readFileSync(pngPath).toString('base64');
  console.log(`[oracle] checking screenshot "${pngPath}" with ${OLLAMA_MODEL}: "${question}"`);

  const answer = await callOllamaVision(imageData, question);
  const passed = answer.trim().toLowerCase().startsWith('yes');

  console.log(`[oracle] answer: "${answer.trim()}" → ${passed ? 'PASS' : 'FAIL'}`);
  return passed;
}
