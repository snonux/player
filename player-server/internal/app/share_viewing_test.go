package app

// End-to-end tests of share viewings through the production wiring (real
// SQLite store, real services, real routes): max_uses counts viewings, not
// HTTP requests.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// shareJSON is the part of the share metadata a media-player client uses.
type shareJSON struct {
	StreamURL   string `json:"stream_url"`
	PlaybackURL string `json:"playback_url"`
	DownloadURL string `json:"download_url"`
	View        string `json:"view"`
	// ViewExpiresAt is when the viewing ends.
	ViewExpiresAt time.Time `json:"view_expires_at"`
}

// shareDo sends an anonymous request with the given headers.
func (e *e2e) shareDo(method, path string, header http.Header) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rr := httptest.NewRecorder()
	e.server.ServeHTTP(rr, req)
	// Every public share answer, whatever its status.
	if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
		e.t.Errorf("%s %s: headers %v, want no-store and no-referrer", method, strings.SplitN(path, "?", 2)[0], rr.Header())
	}
	return rr
}

// ranged returns the headers of a ranged request, optionally with a cookie.
func ranged(byteRange string, cookie *http.Cookie) http.Header {
	h := http.Header{"Range": {byteRange}}
	if cookie != nil {
		h.Set("Cookie", cookie.String())
	}
	return h
}

// openJSON fetches the share metadata as a client without a cookie jar.
func (e *e2e) openJSON(token string) (shareJSON, int) {
	e.t.Helper()
	rr := e.shareDo(http.MethodGet, "/s/"+token, http.Header{"Accept": {"application/json"}})
	var meta shareJSON
	if rr.Code == http.StatusOK {
		e.must(json.Unmarshal(rr.Body.Bytes(), &meta))
	}
	return meta, rr.Code
}

// viewCookie returns the viewing cookie a response set, or nil.
func viewCookie(rr *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == "share_view" {
			return c
		}
	}
	return nil
}

func (e *e2e) usedCount(token string) int {
	e.t.Helper()
	share, err := e.store.GetShareByToken(context.Background(), token)
	e.must(err)
	if share == nil {
		e.t.Fatalf("share %s is gone", token)
	}
	return share.UsedCount
}

// The bug this design fixes: a player fetches media with many ranged GETs.
// On a max_uses=1 share they must all succeed once the metadata was fetched,
// on the original stream and on the compatibility rendition, and the share
// must count exactly one use.
func TestShareViewing_SingleUseSharePlaysWithManyRangedRequests(t *testing.T) {
	tests := []struct {
		name, file, endpoint, wantBody string
	}{
		{"original stream", "movie.mp4", "stream", "orig"},
		{"compat rendition", "clip.avi", "compat", "tran"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newE2E(t, &fakeRunner{})
			token := e.share(tt.file, "alice", `{"max_uses":1}`)
			meta, code := e.openJSON(token)
			if code != http.StatusOK || meta.View == "" {
				t.Fatalf("metadata = %d, view %q", code, meta.View)
			}
			if want := "/s/" + token + "/" + tt.endpoint + "?view=" + meta.View; meta.PlaybackURL != want {
				t.Fatalf("playback_url = %q, want %q", meta.PlaybackURL, want)
			}
			for i := range 6 {
				rr := e.shareDo(http.MethodGet, meta.PlaybackURL, ranged("bytes=0-3", nil))
				if rr.Code != http.StatusPartialContent || rr.Body.String() != tt.wantBody {
					t.Fatalf("ranged GET %d = %d %q", i+1, rr.Code, rr.Body.String())
				}
				if viewCookie(rr) != nil {
					t.Errorf("ranged GET %d inside a viewing set a new cookie", i+1)
				}
			}
			if rr := e.shareDo(http.MethodGet, meta.DownloadURL, nil); rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Disposition"), "attachment") {
				t.Errorf("download inside the viewing = %d %v", rr.Code, rr.Header())
			}
			if got := e.usedCount(token); got != 1 {
				t.Fatalf("used_count = %d, want 1", got)
			}

			// Another client finds the single use spent, on every route.
			if _, code := e.openJSON(token); code != http.StatusGone {
				t.Errorf("second metadata fetch = %d, want 410", code)
			}
			for _, name := range []string{"", "/stream", "/compat", "/download", "/thumbnail"} {
				if rr := e.shareDo(http.MethodGet, "/s/"+token+name, nil); rr.Code != http.StatusGone {
					t.Errorf("GET /s/{token}%s without a credential = %d, want 410", name, rr.Code)
				}
			}
			if got := e.usedCount(token); got != 1 {
				t.Errorf("used_count after refused requests = %d, want 1", got)
			}
		})
	}
}

// Anything that is not a genuine credential of this very share counts as no
// credential: refused on an exhausted share, charged on one with uses left.
func TestShareViewing_InvalidCredentials(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	spent := e.share("movie.mp4", "alice", `{"max_uses":1}`)
	other := e.share("movie.mp4", "alice", `{"max_uses":1}`)
	open := e.share("movie.mp4", "alice", `{"max_uses":50}`)
	good, _ := e.openJSON(spent)
	foreign, _ := e.openJSON(other)
	if good.View == "" || foreign.View == "" {
		t.Fatal("no credentials issued")
	}
	expiry, mac, _ := strings.Cut(good.View, ".")
	invalid := map[string]string{
		"forged":             expiry + "." + strings.Repeat("A", len(mac)),
		"truncated":          good.View[:len(good.View)-2],
		"other share":        foreign.View,
		"extended expiry":    "9" + good.View,
		"garbage":            "not-a-credential",
		"oversized":          strings.Repeat("9", 5000),
		"url-encoded tricks": good.View + "%00",
		// Go's base64 decoder would skip these two.
		"trailing CR": good.View + "%0D",
		"embedded LF": expiry + "." + mac[:10] + "%0A" + mac[10:],
	}
	for name, credential := range invalid {
		t.Run(name, func(t *testing.T) {
			asParam := e.shareDo(http.MethodGet, "/s/"+spent+"/stream?view="+credential, nil)
			asCookie := e.shareDo(http.MethodGet, "/s/"+spent+"/stream", http.Header{"Cookie": {"share_view=" + credential}})
			if asParam.Code != http.StatusGone || asCookie.Code != http.StatusGone {
				t.Fatalf("exhausted share: param = %d, cookie = %d; want 410 for both", asParam.Code, asCookie.Code)
			}
			before := e.usedCount(open)
			rr := e.shareDo(http.MethodGet, "/s/"+open+"/stream?view="+credential, nil)
			if rr.Code != http.StatusOK || viewCookie(rr) == nil || e.usedCount(open) != before+1 {
				t.Fatalf("share with uses left = %d, cookie %v, used %d -> %d; want a new paid viewing", rr.Code, viewCookie(rr), before, e.usedCount(open))
			}
		})
	}
	// The genuine credential still works after all of that.
	if rr := e.shareDo(http.MethodGet, good.StreamURL, nil); rr.Code != http.StatusOK {
		t.Errorf("genuine credential = %d", rr.Code)
	}
	if got := e.usedCount(spent); got != 1 {
		t.Errorf("used_count of the exhausted share = %d, want 1", got)
	}
}

// Revoking a share ends its active viewings immediately.
func TestShareViewing_RevocationKillsActiveViewing(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("clip.avi", "alice", `{"max_uses":1}`)
	meta, _ := e.openJSON(token)
	page := e.shareDo(http.MethodGet, meta.StreamURL, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("stream before revocation = %d", page.Code)
	}

	if rr := e.request(context.Background(), http.MethodDelete, "/api/v1/shares/"+token, "alice", ""); rr.Code != http.StatusOK {
		t.Fatalf("revoke = %d", rr.Code)
	}
	cookie := http.Header{"Cookie": {"share_view=" + meta.View}, "Accept": {"application/json"}}
	for _, path := range []string{meta.StreamURL, meta.PlaybackURL, meta.DownloadURL, "/s/" + token + "/thumbnail?view=" + meta.View, "/s/" + token + "?view=" + meta.View} {
		if rr := e.shareDo(http.MethodGet, path, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s after revocation = %d, want 404", strings.SplitN(path, "?", 2)[0], rr.Code)
		}
	}
	if rr := e.shareDo(http.MethodGet, "/s/"+token+"/stream", cookie); rr.Code != http.StatusNotFound {
		t.Errorf("cookie viewing after revocation = %d, want 404", rr.Code)
	}
}

// A request straight to /stream without a credential opens a viewing of its
// own: one use per such request, as before viewings existed, plus a cookie
// that makes the client's further requests free.
func TestShareViewing_ImplicitViewingForDirectStreamRequest(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("movie.mp4", "alice", `{"max_uses":2}`)
	path := "/s/" + token + "/stream"

	first := e.shareDo(http.MethodGet, path, ranged("bytes=0-7", nil))
	cookie := viewCookie(first)
	if first.Code != http.StatusPartialContent || first.Body.String() != "original" || cookie == nil || cookie.Path != "/s/"+token {
		t.Fatalf("direct stream request = %d %q, cookie %+v", first.Code, first.Body.String(), cookie)
	}
	if got := e.usedCount(token); got != 1 {
		t.Fatalf("used_count = %d, want 1", got)
	}
	// A client that keeps no cookies pays again; it gets the second use...
	if rr := e.shareDo(http.MethodGet, path, nil); rr.Code != http.StatusOK || e.usedCount(token) != 2 {
		t.Fatalf("second cookie-less request = %d, used %d", rr.Code, e.usedCount(token))
	}
	// ...and then the share is exhausted for it,
	if rr := e.shareDo(http.MethodGet, path, nil); rr.Code != http.StatusGone {
		t.Fatalf("third cookie-less request = %d, want 410", rr.Code)
	}
	// while the client that kept its cookie plays on, on every route.
	for _, name := range []string{"/stream", "/download", ""} {
		if rr := e.shareDo(http.MethodGet, "/s/"+token+name, ranged("bytes=0-3", cookie)); rr.Code >= 300 {
			t.Errorf("GET /s/{token}%s with the implicit viewing's cookie = %d", name, rr.Code)
		}
	}
	if got := e.usedCount(token); got != 2 {
		t.Errorf("used_count = %d, want 2", got)
	}
}

// HEAD delivers nothing and never opens a viewing, on any share route.
func TestShareViewing_HeadNeverConsumesAUse(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("movie.mp4", "alice", `{"max_uses":1}`)
	for _, name := range []string{"", "/stream", "/download", "/stream"} {
		rr := e.shareDo(http.MethodHead, "/s/"+token+name, http.Header{"Accept": {"application/json"}})
		if rr.Code != http.StatusOK || viewCookie(rr) != nil {
			t.Fatalf("HEAD /s/{token}%s = %d, cookie %v", name, rr.Code, viewCookie(rr))
		}
	}
	if got := e.usedCount(token); got != 0 {
		t.Fatalf("used_count after probes = %d, want 0", got)
	}
	meta, code := e.openJSON(token)
	if code != http.StatusOK {
		t.Fatalf("metadata after probes = %d", code)
	}
	// Exhausted now: a probe without the credential is refused, one with it
	// is not.
	if rr := e.shareDo(http.MethodHead, "/s/"+token+"/stream", nil); rr.Code != http.StatusGone {
		t.Errorf("HEAD without a credential on the exhausted share = %d, want 410", rr.Code)
	}
	if rr := e.shareDo(http.MethodHead, meta.StreamURL, nil); rr.Code != http.StatusOK {
		t.Errorf("HEAD inside the viewing = %d", rr.Code)
	}
}

// The use is claimed atomically: of many clients opening a max_uses=1 share
// at the same moment exactly one gets a viewing.
func TestShareViewing_ConcurrentOpensGetExactlyOneViewing(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("movie.mp4", "alice", `{"max_uses":1}`)

	const clients = 24
	codes := make([]int, clients)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
			req.Header.Set("Accept", "application/json")
			rr := httptest.NewRecorder()
			e.server.ServeHTTP(rr, req)
			codes[i] = rr.Code
		}()
	}
	close(start)
	wg.Wait()

	opened := 0
	for i, code := range codes {
		switch code {
		case http.StatusOK:
			opened++
		case http.StatusGone:
		default:
			t.Errorf("client %d = %d, want 200 or 410", i, code)
		}
	}
	if opened != 1 || e.usedCount(token) != 1 {
		t.Fatalf("%d viewings opened, used_count %d; want exactly one", opened, e.usedCount(token))
	}
}
