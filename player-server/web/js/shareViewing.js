// Opens the "viewing" of a public share page.
//
// A share link may be limited to a number of uses, and a use is one viewing:
// everything one visitor's browser requests for one sitting. Merely fetching
// the page must not count, because chat apps, mail scanners and prefetching
// browsers fetch every link they see and would spend a single-use link before
// its recipient opens it. So the page GET is free, and the page's script asks
// for the viewing explicitly, with a request such tools never send:
// POST /s/{token}/view. It consumes one use (none if this browser already has
// a viewing, e.g. on reload) and sets a cookie that the browser then sends
// with every media, thumbnail and download request of the share.
//
// The page must therefore not touch any media URL before this has succeeded:
// a media request without the cookie would open a viewing of its own, and
// cost a use, for each request.

// What the visitor is told when the share cannot be shown.
export const SHARE_GONE_MESSAGE = 'This share link is no longer valid.';
export const SHARE_FAILED_MESSAGE = 'This share could not be opened. Reload the page to try again.';
// The viewing is open but the page could not set up its player.
export const SHARE_DISPLAY_FAILED_MESSAGE = 'The shared file could not be displayed here.';

// How long to wait before asking once more after a 410; see openShareViewing.
export const GONE_RETRY_DELAY_MS = 1000;

// shareViewUrl returns the "open viewing" URL for the share page at pagePath
// (location.pathname: "/s/{token}", tolerating a trailing slash).
export function shareViewUrl(pagePath) {
  return `${pagePath.replace(/\/+$/, '')}/view`;
}

// openShareViewing sends the POST and resolves (never rejects) with one of:
//   { state: 'open' }                    the viewing exists; media may load
//   { state: 'gone', message }           404/410: revoked, expired or used up
//   { state: 'failed', message, status } anything else (status 0: no answer)
//
// A 410 is asked about once more after a short pause. The page was served,
// so the share had a use left a moment ago; if it is gone now, the likely
// taker is this same browser — a second tab on the same link — whose viewing
// cookie has arrived in the meantime and makes the repeated request succeed.
// Without the retry the losing tab would call a working link invalid. A 404
// (unknown or revoked) is final.
//
// fetchImpl, sleep and retryDelayMs are injectable for tests.
export async function openShareViewing(pagePath, options = {}) {
  const { sleep = defaultSleep, retryDelayMs = GONE_RETRY_DELAY_MS } = options;
  let result = await requestViewing(pagePath, options);
  if (result.status === 410) {
    await sleep(retryDelayMs);
    result = await requestViewing(pagePath, options);
  }
  return result;
}

// requestViewing sends one POST and classifies the answer.
//
// credentials: 'same-origin' is fetch's default, spelled out because the
// call exists for its cookie: the response sets it, and on a reload the
// request must carry it so that no second use is consumed.
async function requestViewing(pagePath, { fetchImpl = fetch }) {
  let res;
  try {
    res = await fetchImpl(shareViewUrl(pagePath), { method: 'POST', cache: 'no-store', credentials: 'same-origin' });
  } catch {
    return { state: 'failed', status: 0, message: SHARE_FAILED_MESSAGE };
  }
  if (res.ok) return { state: 'open' };
  if (res.status === 404 || res.status === 410) return { state: 'gone', status: res.status, message: SHARE_GONE_MESSAGE };
  return { state: 'failed', status: res.status, message: SHARE_FAILED_MESSAGE };
}

function defaultSleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
