package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
	"github.com/snonux/player/internal/transcode"
)

type shareQualityRenditions struct {
	path    string
	pending bool
	failed  bool
	calls   []transcode.Source
}

func (p *shareQualityRenditions) Ensure(_ context.Context, source transcode.Source) (transcode.Rendition, error) {
	p.calls = append(p.calls, source)
	if p.failed {
		return transcode.Rendition{}, errors.New("encoder unavailable: private server diagnostic")
	}
	if p.pending {
		return transcode.Rendition{}, transcode.ErrPending
	}
	return transcode.Rendition{Path: p.path, ContentType: "audio/mpeg", ETag: "mp3-profile"}, nil
}

func TestShareQualityEnforcedAcrossAliases(t *testing.T) {
	for _, collection := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "collection"}[collection], func(t *testing.T) {
			ctx := context.Background()
			store, err := repository.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			clk := &clock.MockClock{T: time.Now().UTC().Truncate(time.Second)}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			uid, err := store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "h", CreatedAt: clk.T})
			must(err)
			sid, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: clk.T})
			must(err)
			must(store.GrantPermission(ctx, &model.SetPermission{SetID: sid, UserID: uid, Role: model.RoleViewer, CreatedAt: clk.T}))
			original := makeTempFile(t, "ORIGINAL BYTES MUST NOT LEAK")
			mid, err := store.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "song.flac", FileName: "song.flac", AbsPath: original, Type: model.MediaTypeAudio, CreatedAt: clk.T})
			must(err)
			helper := service.NewAccessHelper(store)
			shares := service.NewShareService(store, clk, helper)
			limit := 1
			var share *model.Share
			if collection {
				share, err = shares.CreateCollectionShare(ctx, uid, "Album", []int64{mid, mid}, clk.T.Add(time.Hour), &limit, "mp3192")
			} else {
				share, err = shares.CreateShare(ctx, uid, mid, clk.T.Add(time.Hour), &limit, "mp3192")
			}
			must(err)
			path := filepath.Join(t.TempDir(), "encode.mp3")
			must(os.WriteFile(path, []byte("ENCODED MP3 BYTES"), 0600))
			provider := &shareQualityRenditions{path: path, pending: true}
			compat := service.NewCompatStreamService(helper, shares, provider, "")
			media := service.NewMediaService(store, clk, "", nil, nil)
			srv := newTestServer(t, store, nil, nil, &internal.Config{SessionTimeoutHours: 24, ShareDefaultExpiryDays: 7}, nil, nil, media, nil, nil, nil, nil, nil, nil, nil)
			srv.media.Share = shares
			srv.media.Compat = compat

			// Exercise JSON creation and omission-aware PATCH with real services.
			authCall := func(handler http.HandlerFunc, body string, values map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/api/shares", strings.NewReader(body))
				r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: uid}))
				for key, value := range values {
					r.SetPathValue(key, value)
				}
				rr := httptest.NewRecorder()
				handler(rr, r)
				return rr
			}
			for _, profile := range []string{"original", "aac128", "aac256", "mp3192", "opus96"} {
				body := fmt.Sprintf(`{"audio_quality":%q}`, profile)
				handler := srv.handleCreateShare
				values := map[string]string{"id": fmt.Sprint(mid)}
				wantStatus := 200
				if collection {
					body = fmt.Sprintf(`{"title":"API album","media_ids":[%d,%d],"audio_quality":%q}`, mid, mid, profile)
					handler = srv.handleCreateCollectionShare
					values = nil
					wantStatus = 201
				}
				rr := authCall(handler, body, values)
				if rr.Code != wantStatus {
					t.Fatalf("create policy %s: %d %s", profile, rr.Code, rr.Body.String())
				}
				var created model.Share
				must(json.Unmarshal(rr.Body.Bytes(), &created))
				if created.AudioQuality != profile {
					t.Fatal("creation lost selected quality")
				}
				rr = authCall(srv.handleUpdateShare, `{"description":"older editor"}`, map[string]string{"token": created.Token})
				if rr.Code != 200 {
					t.Fatalf("old editor: %d %s", rr.Code, rr.Body.String())
				}
				must(json.Unmarshal(rr.Body.Bytes(), &created))
				if created.AudioQuality != profile {
					t.Fatal("PATCH omission reset policy")
				}
				rr = authCall(srv.handleUpdateShare, `{"audio_quality":""}`, map[string]string{"token": created.Token})
				if rr.Code != 200 {
					t.Fatalf("clear policy: %d %s", rr.Code, rr.Body.String())
				}
				must(json.Unmarshal(rr.Body.Bytes(), &created))
				if created.AudioQuality != "" {
					t.Fatal("explicit Auto not saved")
				}
			}
			base := "/s/" + share.Token
			paths := []string{base + "/stream", base + "/download", base + "/compat"}
			if collection {
				paths = append(paths, base+"/items/0/stream", base+"/items/1/download", base+"/items/1/compat")
			}
			request := func(method, path string, cookie *http.Cookie, rangeHeader bool) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, path, nil)
				if cookie != nil {
					req.AddCookie(cookie)
				}
				if rangeHeader {
					req.Header.Set("Range", "bytes=0-6")
				}
				rr := httptest.NewRecorder()
				srv.ServeHTTP(rr, req)
				return rr
			}
			used := func() int { got, e := store.GetShareByToken(ctx, share.Token); must(e); return got.UsedCount }
			// Preparing a conversion, including direct stream/download aliases, spends no viewing.
			for _, path := range paths {
				for _, method := range []string{"HEAD", "GET"} {
					rr := request(method, path+"?profile=original&quality=original", nil, false)
					if rr.Code != 503 {
						t.Fatalf("pending %s %s: %d %s", method, path, rr.Code, rr.Body.String())
					}
				}
			}
			if used() != 0 {
				t.Fatal("pending consumed a viewing")
			}
			provider.pending = false
			provider.failed = true
			for _, path := range paths {
				rr := request("GET", path, nil, false)
				if rr.Code != 500 || strings.Contains(rr.Body.String(), "private server") || !strings.Contains(rr.Body.String(), "choose another audio quality") {
					t.Fatalf("conversion failure %s: %d %s", path, rr.Code, rr.Body.String())
				}
			}
			if used() != 0 {
				t.Fatal("failed conversion spent viewing")
			}
			provider.failed = false
			for _, path := range paths {
				rr := request("HEAD", path, nil, false)
				if rr.Code != 200 || rr.Body.Len() != 0 {
					t.Fatalf("HEAD %s: %d", path, rr.Code)
				}
			}
			if used() != 0 {
				t.Fatal("HEAD consumed a viewing")
			}
			rr := request("GET", paths[0], nil, true)
			if rr.Code != 206 || rr.Body.String() != "ENCODED" {
				t.Fatalf("first GET %d %s", rr.Code, rr.Body.String())
			}
			cookies := rr.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("missing viewing cookie")
			}
			cookie := cookies[0]
			page, err := shares.GetSharedMedia(ctx, service.ShareAccess{Token: share.Token, Credentials: []string{cookie.Value}})
			must(err)
			if page.AudioQuality != "mp3192" || !page.Transcoded || !strings.Contains(page.PlaybackURL, "/compat") {
				t.Fatalf("wrong public policy: %+v", page)
			}
			view := page.WithViewCredential().View
			for _, path := range paths {
				rr := request("GET", path+"?view="+view+"&profile=original&audio_quality=original", nil, true)
				if rr.Code != 206 || rr.Body.String() != "ENCODED" || rr.Header().Get("Content-Type") != "audio/mpeg" {
					t.Fatalf("bypass %s: %d %s", path, rr.Code, rr.Body.String())
				}
				if strings.HasSuffix(path, "/download") && !strings.Contains(rr.Header().Get("Content-Disposition"), ".mp3") {
					t.Fatal("wrong download extension")
				}
			}
			if used() != 1 {
				t.Fatal("ranges double counted viewing")
			}
			for _, call := range provider.calls {
				if call.Kind != transcode.KindMP3192 || call.Requester != "shares" {
					t.Fatalf("overridden profile/budget %+v", call)
				}
			}
			description := "Old editor"
			_, err = shares.UpdateShare(ctx, share.Token, uid, &description, nil)
			must(err)
			saved, err := store.GetShareByToken(ctx, share.Token)
			must(err)
			if saved.AudioQuality != "mp3192" {
				t.Fatal("old editor erased policy")
			}
			// Changing the policy affects previously issued URLs and active viewings.
			quality := "original"
			_, err = shares.UpdateShare(ctx, share.Token, uid, nil, nil, &quality)
			must(err)
			rr = request("GET", paths[0], cookie, false)
			if rr.Code != 200 || rr.Body.String() != "ORIGINAL BYTES MUST NOT LEAK" {
				t.Fatalf("original %d %s", rr.Code, rr.Body.String())
			}
			rr = request("GET", base+"/compat", cookie, false)
			if rr.Code != 400 {
				t.Fatalf("explicit original converted: %d", rr.Code)
			}
			// Current creator access, expiry and revocation apply to already signed links.
			quality = "mp3192"
			_, err = shares.UpdateShare(ctx, share.Token, uid, nil, nil, &quality)
			must(err)
			must(store.RevokePermission(ctx, sid, uid))
			for _, path := range paths {
				if got := request("GET", path, cookie, false).Code; got != 404 {
					t.Fatalf("lost ACL %s=%d", path, got)
				}
			}
			must(store.GrantPermission(ctx, &model.SetPermission{SetID: sid, UserID: uid, Role: model.RoleViewer, CreatedAt: clk.T}))
			clk.T = clk.T.Add(2 * time.Hour)
			for _, path := range paths {
				if got := request("GET", path, cookie, false).Code; got != 410 {
					t.Fatalf("expired %s=%d", path, got)
				}
			}
			must(shares.RevokeShare(ctx, share.Token, uid))
			for _, path := range paths {
				if got := request("GET", path, cookie, false).Code; got != 404 {
					t.Fatalf("revoked %s=%d", path, got)
				}
			}
			if _, err := shares.CreateShare(ctx, uid, mid, clk.T.Add(time.Hour), nil, "bogus"); !errors.Is(err, service.ErrInvalidShareQuality) {
				t.Fatalf("unsupported quality %v", err)
			}
		})
	}
}
