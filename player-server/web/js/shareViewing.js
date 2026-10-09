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

// What the visitor is told when the viewing cannot be opened.
export const SHARE_GONE_MESSAGE = 'This share link is no longer valid.';
export const SHARE_FAILED_MESSAGE = 'This share could not be opened. Reload the page to try again.';

// shareViewUrl returns the "open viewing" URL for the share page at pagePath
// (location.pathname: "/s/{token}", tolerating a trailing slash).
export function shareViewUrl(pagePath) {
  return `${pagePath.replace(/\/+$/, '')}/view`;
}

// openShareViewing sends the POST and resolves (never rejects) with one of:
//   { state: 'open' }                    the viewing exists; media may load
//   { state: 'gone', message }           404/410: revoked, expired or used up
//   { state: 'failed', message, status } anything else (status 0: no answer)
// fetchImpl is injectable for tests.
//
// credentials: 'same-origin' is fetch's default, spelled out because the
// call exists for its cookie: the response sets it, and on a reload the
// request must carry it so that no second use is consumed.
export async function openShareViewing(pagePath, { fetchImpl = fetch } = {}) {
  let res;
  try {
    res = await fetchImpl(shareViewUrl(pagePath), { method: 'POST', cache: 'no-store', credentials: 'same-origin' });
  } catch {
    return { state: 'failed', status: 0, message: SHARE_FAILED_MESSAGE };
  }
  if (res.ok) return { state: 'open' };
  if (res.status === 404 || res.status === 410) return { state: 'gone', message: SHARE_GONE_MESSAGE };
  return { state: 'failed', status: res.status, message: SHARE_FAILED_MESSAGE };
}
