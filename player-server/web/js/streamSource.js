// Picks the URL a media item is played from and waits for the server's
// compatibility stream (a transcoded MP4/M4A rendition of formats browsers
// cannot decode) to become ready.
//
// A <video>/<audio> element cannot see HTTP status codes, so the compat URL is
// never assigned to `src` blindly: the first request of a not yet transcoded
// file answers 503 + Retry-After, which the element would report as an opaque
// "format not supported" error. waitForCompatStream() probes the URL with a
// HEAD request instead and only reports "ready" once it answers 200/206.

// Upper bound for the whole wait. Long videos can take minutes to transcode;
// beyond this the user is told to try again later instead of waiting forever.
export const COMPAT_MAX_WAIT_MS = 5 * 60 * 1000;

// One probe may legitimately take a while (the server holds it up to 20s
// while a transcode runs), but a connection that never answers must not keep
// the "preparing" state alive past the cap.
export const PROBE_TIMEOUT_MS = 45 * 1000;

// A 503 without the server's X-Transcode-Status header does not come from the
// compat endpoint (reverse proxy, restarting pod). It may be a short blip, so
// it is retried, but only this many times in a row instead of for the whole
// cap.
export const MAX_UNRECOGNISED_503 = 6;

const DEFAULT_RETRY_SECONDS = 5;
const MAX_RETRY_SECONDS = 30;

// Reasons shown in the error toast for terminal compat answers. The probe is
// a HEAD request, so there is no JSON error body to quote; statuses not
// listed are reported by number.
const TERMINAL_REASONS = {
  400: 'this file has no converted version',
  401: 'you are not signed in',
  403: 'access denied',
  404: 'the file was not found on the server',
  410: 'the link is no longer valid',
  415: 'this kind of file cannot be converted',
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

// waitForCompatStream repeats a HEAD request of the compat URL until the
// rendition exists. HEAD starts/observes the transcode like GET, and the
// probe itself consumes no use of a share link (the media element's own GET
// requests still do). It resolves (never rejects) with one of:
//   { state: 'ready' }                  200/206: safe to assign the URL to src
//   { state: 'failed', reason, status } terminal status (status 0: no answer)
//   { state: 'cancelled' }              options.signal was aborted
// Any 503 means "preparing": the wait is the Retry-After header (seconds) and
// options.onPreparing(status) is called with the X-Transcode-Status header
// ("transcoding" or "busy"; '' when absent). fetchImpl, sleep, now, maxWaitMs
// and timeoutMs are injectable for tests.
export async function waitForCompatStream(url, options = {}) {
  const { signal, onPreparing, sleep = abortableSleep, now = Date.now, maxWaitMs = COMPAT_MAX_WAIT_MS } = options;
  const deadline = now() + maxWaitMs;
  let unrecognised = 0;
  for (;;) {
    const res = await probe(url, options);
    if (signal?.aborted) return { state: 'cancelled' };
    if (!res) return failed(0, 'the server did not answer');
    if (res.status === 200 || res.status === 206) return { state: 'ready' };
    if (res.status !== 503) return failed(res.status, terminalReason(res.status));
    const status = res.headers?.get?.('X-Transcode-Status') || '';
    unrecognised = status ? 0 : unrecognised + 1;
    if (unrecognised > MAX_UNRECOGNISED_503) return failed(503, 'the server is unavailable, try again later');
    const waitMs = retryDelayMs(res);
    if (now() + waitMs > deadline) return failed(503, 'the server is still preparing this file, try again later');
    onPreparing?.(status);
    await sleep(waitMs, signal);
    if (signal?.aborted) return { state: 'cancelled' };
  }
}

function failed(status, reason) {
  return { state: 'failed', status, reason };
}

// probe sends one HEAD request and returns the response, or null when there
// was none: network error, per-request timeout, or abort (the caller tells
// the latter apart through the signal).
async function probe(url, { signal, fetchImpl = fetch, timeoutMs = PROBE_TIMEOUT_MS }) {
  const request = new AbortController();
  const abort = () => request.abort();
  const timer = setTimeout(abort, timeoutMs);
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  try {
    return await fetchImpl(url, { method: 'HEAD', cache: 'no-store', signal: request.signal });
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener('abort', abort);
  }
}

function terminalReason(status) {
  return TERMINAL_REASONS[status] || `the server answered with status ${status}`;
}

// retryDelayMs reads the Retry-After header (seconds) and clamps it so a
// bogus value can neither busy-loop nor stall the player.
function retryDelayMs(res) {
  const hinted = Number(res.headers?.get?.('Retry-After'));
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
