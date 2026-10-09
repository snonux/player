// Picks the URL a media item is played from and waits for the server's
// compatibility stream (a transcoded MP4/M4A rendition of formats browsers
// cannot decode) to become ready.
//
// A <video>/<audio> element cannot see HTTP status codes, so the compat URL is
// never assigned to `src` blindly: the first request of a not yet transcoded
// file answers 503 + Retry-After, which the element would report as an opaque
// "format not supported" error. waitForCompatStream() probes the URL with
// fetch() instead and only reports "ready" once it answers 200/206.

// Upper bound for the whole wait. Long videos can take minutes to transcode;
// beyond this the user is told to try again later instead of waiting forever.
export const COMPAT_MAX_WAIT_MS = 5 * 60 * 1000;

const DEFAULT_RETRY_SECONDS = 5;
const MAX_RETRY_SECONDS = 30;

// Reasons shown in the error toast for terminal compat answers. Statuses not
// listed fall back to the server's JSON "error" text or the bare status code.
const TERMINAL_REASONS = {
  401: 'you are not signed in',
  403: 'access denied',
  404: 'the file was not found on the server',
  410: 'the link is no longer valid',
  500: 'the server could not convert this file',
  507: 'the server has no space left to convert this file',
};

// mediaPlaybackUrl returns the URL to play for a library item. The choice
// relies only on the server's "transcoded" hint, never on the file extension.
export function mediaPlaybackUrl(media) {
  return `/api/media/${media.id}/${media.transcoded ? 'compat' : 'stream'}`;
}

// mediaErrorReason turns a MediaError (from a media element's `error` event)
// into the text shown in the error toast.
export function mediaErrorReason(error) {
  switch (error?.code) {
    case 1: return 'loading was aborted';
    case 2: return 'a network error interrupted loading';
    case 3: return 'the file is damaged or cannot be decoded';
    case 4: return 'the format is not supported or the file could not be loaded';
    default: return 'unknown playback error';
  }
}

// waitForCompatStream repeats a one-byte Range GET of the compat URL until the
// rendition exists. It resolves (never rejects) with one of:
//   { state: 'ready' }                 200/206: safe to assign the URL to src
//   { state: 'failed', reason }        terminal status, network error or cap hit
//   { state: 'cancelled' }             options.signal was aborted
// options.onPreparing(status) is called before every retry wait with the
// server's 503 status ("transcoding" or "busy"). fetchImpl, sleep and now are
// injectable for tests.
export async function waitForCompatStream(url, options = {}) {
  const { signal, onPreparing, fetchImpl = fetch, sleep = abortableSleep, now = Date.now, maxWaitMs = COMPAT_MAX_WAIT_MS } = options;
  const deadline = now() + maxWaitMs;
  for (;;) {
    const res = await probe(url, signal, fetchImpl);
    if (signal?.aborted) return { state: 'cancelled' };
    if (!res) return { state: 'failed', reason: 'the server could not be reached' };
    if (res.status === 200 || res.status === 206) {
      // Only the status matters; do not download the rendition here.
      res.body?.cancel?.().catch?.(() => {});
      return { state: 'ready' };
    }
    const body = await readJson(res);
    if (res.status !== 503) return { state: 'failed', reason: terminalReason(res.status, body) };
    const waitMs = retryDelayMs(res, body);
    if (now() + waitMs > deadline) {
      return { state: 'failed', reason: 'the server is still preparing this file, try again later' };
    }
    onPreparing?.(body?.status || 'transcoding');
    await sleep(waitMs, signal);
    if (signal?.aborted) return { state: 'cancelled' };
  }
}

// probe returns the response, or null when the request itself failed
// (network error or abort; the caller tells them apart through the signal).
async function probe(url, signal, fetchImpl) {
  try {
    return await fetchImpl(url, { headers: { Range: 'bytes=0-0' }, cache: 'no-store', signal });
  } catch {
    return null;
  }
}

async function readJson(res) {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

function terminalReason(status, body) {
  return TERMINAL_REASONS[status] || body?.error || `the server answered with status ${status}`;
}

// retryDelayMs prefers the Retry-After header, then the JSON hint, and clamps
// the result so a bogus value can neither busy-loop nor stall the player.
function retryDelayMs(res, body) {
  const header = Number(res.headers?.get?.('Retry-After'));
  const hinted = header > 0 ? header : Number(body?.retry_after_seconds);
  const seconds = hinted > 0 ? hinted : DEFAULT_RETRY_SECONDS;
  return Math.min(MAX_RETRY_SECONDS, Math.max(1, seconds)) * 1000;
}

// abortableSleep resolves after ms, or immediately once the signal aborts, so
// a cancelled wait does not keep a timer alive.
function abortableSleep(ms, signal) {
  return new Promise((resolve) => {
    if (signal?.aborted) { resolve(); return; }
    const timer = setTimeout(done, ms);
    function done() {
      clearTimeout(timer);
      signal?.removeEventListener('abort', done);
      resolve();
    }
    signal?.addEventListener('abort', done, { once: true });
  });
}
