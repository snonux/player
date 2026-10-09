package app

// End-to-end tests of the browser's way into a share: the HTML page is free
// (link previewers fetch it), and the page opens its viewing with
// POST /s/{token}/view.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// previewers are requests of the kind chat apps, mail scanners and
// prefetching browsers send for every link they see.
var previewers = []http.Header{
	{"User-Agent": {"TelegramBot (like TwitterBot)"}},
	{"User-Agent": {"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"}, "Accept": {"*/*"}},
	{"User-Agent": {"Mozilla/5.0 (compatible; Discordbot/2.0)"}, "Accept": {"text/html"}},
	{"Accept": {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}, "Sec-Purpose": {"prefetch"}},
	{},
}

// openViewing sends the share page's POST, optionally with a cookie.
func (e *e2e) openViewing(token string, cookie *http.Cookie) (int, *http.Cookie) {
	e.t.Helper()
	header := http.Header{}
	if cookie != nil {
		header.Set("Cookie", cookie.Name+"="+cookie.Value)
	}
	rr := e.shareDo(http.MethodPost, "/s/"+token+"/view", header)
	if rr.Body.Len() != 0 && rr.Code == http.StatusNoContent {
		e.t.Errorf("POST view answered 204 with a body: %q", rr.Body.String())
	}
	return rr.Code, viewCookie(rr)
}

// Fetching the page costs nothing and sets no cookie, however often and by
// whomever: a single-use link survives every preview. (Before, such a fetch
// spent the use and the recipient found the link dead.)
func TestSharePage_PreviewersDoNotSpendTheUse(t *testing.T) {
	for _, file := range []string{"movie.mp4", "pic.png", "clip.avi"} {
		t.Run(file, func(t *testing.T) {
			e := newE2E(t, &fakeRunner{})
			token := e.share(file, "alice", `{"max_uses":1}`)
			for i, header := range previewers {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					rr := e.shareDo(method, "/s/"+token, header)
					if rr.Code != http.StatusOK || viewCookie(rr) != nil {
						t.Fatalf("previewer %d %s = %d, cookie %v", i, method, rr.Code, viewCookie(rr))
					}
				}
			}
			page := e.shareDo(http.MethodGet, "/s/"+token, previewers[0]).Body.String()
			if strings.Contains(page, "view") || !strings.Contains(page, `"/s/`+token+`/stream"`) {
				t.Errorf("share page must embed plain URLs and no credential: %s", page)
			}
			if got := e.usedCount(token); got != 0 {
				t.Fatalf("used_count after previews = %d, want 0", got)
			}

			// The recipient still gets the one viewing.
			code, cookie := e.openViewing(token, nil)
			if code != http.StatusNoContent || cookie == nil {
				t.Fatalf("POST view = %d, cookie %v", code, cookie)
			}
			if rr := e.shareDo(http.MethodGet, "/s/"+token+"/stream", ranged("bytes=0-7", cookie)); rr.Code != http.StatusPartialContent || rr.Body.String() != "original" {
				t.Fatalf("media after the previews = %d %q", rr.Code, rr.Body.String())
			}
		})
	}
}

// The page's POST opens the viewing: exactly one use and the cookie. With
// the cookie, repeating it and reloading the page change nothing; without,
// the spent share is gone for the page, the POST and the media alike.
func TestSharePage_PostOpensExactlyOneViewing(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("movie.mp4", "alice", `{"max_uses":1}`)

	code, cookie := e.openViewing(token, nil)
	if code != http.StatusNoContent || cookie == nil {
		t.Fatalf("POST view = %d, cookie %v", code, cookie)
	}
	if cookie.Path != "/s/"+token || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 || cookie.Secure {
		t.Errorf("cookie = %+v, want HttpOnly, SameSite=Lax, Path=/s/{token}, a lifetime, and no Secure (SECURE_COOKIES is off here)", cookie)
	}
	jar := http.Header{"Accept": {"text/html"}, "Cookie": {cookie.Name + "=" + cookie.Value}}
	for i := range 3 {
		if code, again := e.openViewing(token, cookie); code != http.StatusNoContent || again != nil {
			t.Fatalf("POST %d with the cookie = %d, new cookie %v", i+2, code, again)
		}
		if reload := e.shareDo(http.MethodGet, "/s/"+token, jar); reload.Code != http.StatusOK || viewCookie(reload) != nil {
			t.Fatalf("reload %d = %d, new cookie %v", i+1, reload.Code, viewCookie(reload))
		}
		if rr := e.shareDo(http.MethodGet, "/s/"+token+"/stream", ranged("bytes=9-13", cookie)); rr.Code != http.StatusPartialContent || rr.Body.String() != "movie" {
			t.Fatalf("ranged GET %d with the cookie = %d %q", i+1, rr.Code, rr.Body.String())
		}
	}
	if got := e.usedCount(token); got != 1 {
		t.Fatalf("used_count = %d, want 1", got)
	}

	// Another browser.
	if rr := e.shareDo(http.MethodGet, "/s/"+token, http.Header{"Accept": {"text/html"}}); rr.Code != http.StatusGone {
		t.Errorf("page for another browser = %d, want 410", rr.Code)
	}
	if code, cookie := e.openViewing(token, nil); code != http.StatusGone || cookie != nil {
		t.Errorf("POST view for another browser = %d, cookie %v; want 410 and none", code, cookie)
	}
	if got := e.usedCount(token); got != 1 {
		t.Errorf("used_count = %d, want it unchanged at 1", got)
	}
}

// POST view answers like the other share routes for shares that are gone.
func TestSharePage_PostViewOnUnusableShares(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	revoked := e.share("movie.mp4", "alice", `{"max_uses":1}`)
	_, cookie := e.openViewing(revoked, nil)
	if rr := e.request(ctx, http.MethodDelete, "/api/v1/shares/"+revoked, "alice", ""); rr.Code != http.StatusOK {
		t.Fatalf("revoke = %d", rr.Code)
	}
	e.must(e.store.CreateShare(ctx, &model.Share{
		Token: "expired", MediaID: e.media["movie.mp4"], CreatedBy: e.users["alice"],
		CreatedAt: time.Now().Add(-48 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	}))

	tests := []struct {
		name, token string
		cookie      *http.Cookie
		want        int
	}{
		{"revoked", revoked, nil, http.StatusNotFound},
		{"revoked, inside its viewing", revoked, cookie, http.StatusNotFound},
		{"expired", "expired", nil, http.StatusGone},
		{"unknown", "no-such-token", nil, http.StatusNotFound},
	}
	for _, tt := range tests {
		if code, set := e.openViewing(tt.token, tt.cookie); code != tt.want || set != nil {
			t.Errorf("%s: POST view = %d, cookie %v; want %d and no cookie", tt.name, code, set, tt.want)
		}
	}
}

// A stale "view" parameter must not shadow the browser's valid cookie: the
// request belongs to the cookie's viewing and costs nothing.
func TestShareViewing_StaleParameterFallsBackToCookie(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("movie.mp4", "alice", `{"max_uses":1}`)
	_, cookie := e.openViewing(token, nil)

	for _, stale := range []string{"1.AAAA", "garbage", strings.Repeat("9", 100)} {
		rr := e.shareDo(http.MethodGet, "/s/"+token+"/stream?view="+stale, ranged("bytes=0-7", cookie))
		if rr.Code != http.StatusPartialContent || viewCookie(rr) != nil {
			t.Errorf("stale view=%.10s with a valid cookie = %d, new cookie %v; want 206 and none", stale, rr.Code, viewCookie(rr))
		}
	}
	if got := e.usedCount(token); got != 1 {
		t.Errorf("used_count = %d, want 1", got)
	}
}

// A viewing never outlives its share: the credential's expiry, the
// advertised view_expires_at and the cookie lifetime all stop at the share's
// own expiry.
func TestShareViewing_ExpiryIsCappedAtTheSharesExpiry(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	shareEnds := time.Now().Add(20 * time.Minute)
	for _, token := range []string{"short-json", "short-page"} {
		e.must(e.store.CreateShare(context.Background(), &model.Share{
			Token: token, MediaID: e.media["movie.mp4"], CreatedBy: e.users["alice"],
			CreatedAt: time.Now(), ExpiresAt: shareEnds,
		}))
	}

	meta, code := e.openJSON("short-json")
	if code != http.StatusOK || meta.ViewExpiresAt.After(shareEnds) || meta.ViewExpiresAt.Before(shareEnds.Add(-2*time.Second)) {
		t.Errorf("view_expires_at = %v (HTTP %d), want the share's expiry %v", meta.ViewExpiresAt, code, shareEnds)
	}
	if expiry, _, _ := strings.Cut(meta.View, "."); expiry != strconv.FormatInt(shareEnds.Unix(), 10) {
		t.Errorf("credential expiry = %s, want %d", expiry, shareEnds.Unix())
	}
	_, cookie := e.openViewing("short-page", nil)
	if cookie == nil || cookie.MaxAge <= 0 || cookie.MaxAge > 20*60 || cookie.Expires.After(shareEnds) {
		t.Errorf("cookie = %+v, want a lifetime of at most the share's 20 minutes", cookie)
	}
}
