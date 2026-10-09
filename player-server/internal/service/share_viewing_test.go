package service

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// verifyCase is one verifyShareViewing scenario.
type verifyCase struct {
	name       string
	key        []byte
	token      string
	credential string
	at         time.Time
	want       bool
}

// verifyCases lists what must and must not pass for good, a credential
// signed with key for the share "tok" at now, expiring at expiresAt.
func verifyCases(key []byte, good string, now, expiresAt time.Time) []verifyCase {
	expiry, mac, _ := strings.Cut(good, ".")
	later := strconv.FormatInt(expiresAt.Add(time.Hour).Unix(), 10)
	// Change the MAC's first character, staying inside base64url.
	flipped := "A"
	if mac[0] == 'A' {
		flipped = "B"
	}
	return []verifyCase{
		{"valid", key, "tok", good, now, true},
		{"valid until the last second", key, "tok", good, expiresAt.Add(-time.Second), true},
		{"expired at the expiry instant", key, "tok", good, expiresAt, false},
		{"expired long ago", key, "tok", good, expiresAt.Add(24 * time.Hour), false},
		{"another share's token", key, "other", good, now, false},
		{"token that extends this one", key, "tok\n1", good, now, false},
		{"another key", []byte("ffffffffffffffffffffffffffffffff"), "tok", good, now, false},
		{"empty", key, "tok", "", now, false},
		{"expiry only", key, "tok", expiry, now, false},
		{"expiry and dot", key, "tok", expiry + ".", now, false},
		{"mac only", key, "tok", "." + mac, now, false},
		{"truncated mac", key, "tok", good[:len(good)-1], now, false},
		{"truncated to half", key, "tok", good[:len(good)/2], now, false},
		{"extended mac", key, "tok", good + "A", now, false},
		{"flipped mac", key, "tok", expiry + "." + flipped + mac[1:], now, false},
		{"extended expiry", key, "tok", later + "." + mac, now, false},
		{"padded mac", key, "tok", good + "=", now, false},
		// Go's base64 decoder skips CR and LF, also in Strict mode; without
		// the alphabet check these would be further spellings of good.
		{"trailing CR", key, "tok", good + "\r", now, false},
		{"trailing LF", key, "tok", good + "\n", now, false},
		{"trailing CRLF", key, "tok", good + "\r\n", now, false},
		{"embedded LF", key, "tok", expiry + "." + mac[:7] + "\n" + mac[7:], now, false},
		{"LF before the mac", key, "tok", expiry + ".\n" + mac, now, false},
		{"space", key, "tok", good + " ", now, false},
		{"NUL", key, "tok", good + "\x00", now, false},
		{"non-ASCII", key, "tok", good + "é", now, false},
		{"standard base64 alphabet", key, "tok", expiry + "." + strings.NewReplacer("-", "+", "_", "/").Replace(mac) + "+", now, false},
		{"leading zero expiry", key, "tok", "0" + good, now, false},
		{"signed expiry", key, "tok", "+" + good, now, false},
		{"negative expiry", key, "tok", "-1." + mac, now, false},
		{"non-numeric expiry", key, "tok", "soon." + mac, now, false},
		{"oversized", key, "tok", good + strings.Repeat("A", 200), now, false},
	}
}

func TestVerifyShareViewing(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_800_000_000, 0)
	expiresAt := now.Add(ShareViewingLifetime)
	good := signShareViewing(key, "tok", expiresAt)

	for _, tt := range verifyCases(key, good, now, expiresAt) {
		t.Run(tt.name, func(t *testing.T) {
			gotExpiry, ok := verifyShareViewing(tt.key, tt.token, tt.credential, tt.at)
			if ok != tt.want {
				t.Fatalf("verify(%q) = %v, want %v", tt.credential, ok, tt.want)
			}
			if ok && !gotExpiry.Equal(expiresAt) {
				t.Errorf("expiry = %v, want %v", gotExpiry, expiresAt)
			}
		})
	}
	if len(good) > maxShareCredentialLen {
		t.Errorf("a genuine credential (%d chars) exceeds the parse limit %d", len(good), maxShareCredentialLen)
	}
}

// The alphabet check on its own: verifyShareViewing has a second line of
// defence (the re-encoding comparison), so only a direct test pins this one.
func TestIsShareCredentialText(t *testing.T) {
	tests := []struct {
		name, text string
		want       bool
	}{
		{"empty", "", true},
		{"whole alphabet", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-", true},
		{"genuine shape", "1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU", true},
		{"CR", "abc\r", false},
		{"LF", "abc\n", false},
		{"LF inside", "ab\nc", false},
		{"tab", "a\tb", false},
		{"space", "a b", false},
		{"NUL", "a\x00b", false},
		{"DEL", "a\x7fb", false},
		{"plus", "a+b", false},
		{"slash", "a/b", false},
		{"padding", "abc=", false},
		{"percent", "a%0Ab", false},
		{"ampersand", "a&b", false},
		{"colon", "a:b", false},
		{"at", "a@b", false},
		{"backtick", "a`b", false},
		{"bracket", "a[b", false},
		{"brace", "a{b", false},
		{"non-ASCII letter", "aéb", false},
		{"high byte", "a\xffb", false},
	}
	for _, tt := range tests {
		if got := isShareCredentialText(tt.text); got != tt.want {
			t.Errorf("%s: isShareCredentialText(%q) = %v, want %v", tt.name, tt.text, got, tt.want)
		}
	}
}

// The first valid credential counts, wherever it stands: a stale "view"
// parameter in front of a good cookie must not cost a use.
func TestShareViewing_FirstValidCredentialCounts(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("tok", time.Hour, 1)
	good := f.open("tok")

	for _, credentials := range [][]string{{good}, {"1.AAAA", good}, {"", "junk", good}, {good, "junk"}} {
		_, viewing, err := f.svc.StreamSharedMedia(context.Background(), ShareAccess{Token: "tok", Credentials: credentials})
		if err != nil || viewing.Opened || viewing.Credential != good {
			t.Errorf("credentials %q: viewing %+v, err %v; want the existing viewing", credentials, viewing, err)
		}
	}
	if _, _, err := f.svc.StreamSharedMedia(context.Background(), ShareAccess{Token: "tok", Credentials: []string{"1.AAAA", "junk"}}); !errors.Is(err, ErrShareExpired) {
		t.Errorf("only invalid credentials on the exhausted share = %v, want ErrShareExpired", err)
	}
	if got := f.used("tok"); got != 1 {
		t.Errorf("used_count = %d, want 1", got)
	}
}

// A viewing ends with its share at the latest; a share that outlives the
// viewing lifetime does not extend it.
func TestShareViewing_ExpiryIsCappedAtTheSharesExpiry(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	tests := []struct {
		token    string
		lifetime time.Duration
		want     time.Duration
	}{
		{"short", 20 * time.Minute, 20 * time.Minute},
		{"long", 30 * 24 * time.Hour, ShareViewingLifetime},
	}
	for _, tt := range tests {
		f.share(tt.token, tt.lifetime, 0)
		res, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: tt.token})
		f.must(err)
		if want := f.clk.T.Add(tt.want); !res.Viewing.ExpiresAt.Equal(want) {
			t.Errorf("%s share: viewing expires %v, want %v", tt.token, res.Viewing.ExpiresAt, want)
		}
		if got := res.WithViewCredential().ViewExpiresAt; got == nil || !got.Equal(res.Viewing.ExpiresAt) {
			t.Errorf("%s share: view_expires_at = %v, want the viewing's expiry", tt.token, got)
		}
	}
}

// A probe (HEAD, the HTML page) never opens a viewing, with or without a
// credential, and reports the viewing its credential stands for.
func TestShareViewing_ProbeNeverOpens(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("tok", time.Hour, 1)

	res, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: "tok", Probe: true})
	if err != nil || res.Viewing != (ShareViewing{}) || f.used("tok") != 0 {
		t.Fatalf("probe without a credential: viewing %+v, err %v, used %d", res.Viewing, err, f.used("tok"))
	}
	good := f.open("tok")
	res, err = f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: "tok", Credentials: []string{good}, Probe: true})
	if err != nil || res.Viewing.Opened || res.Viewing.Credential != good {
		t.Errorf("probe inside the viewing: %+v, err %v", res.Viewing, err)
	}
	if _, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: "tok", Probe: true}); !errors.Is(err, ErrShareExpired) {
		t.Errorf("probe of the exhausted share without a credential = %v, want ErrShareExpired", err)
	}
	if got := f.used("tok"); got != 1 {
		t.Errorf("used_count = %d, want 1", got)
	}
}

func TestWithViewCredential(t *testing.T) {
	expiresAt := time.Unix(1_800_000_000, 0)
	plain := GetSharedMediaResult{
		StreamURL: "/s/tok/stream", PlaybackURL: "/s/tok/compat", DownloadURL: "/s/tok/download", ThumbURL: "/s/tok/thumbnail",
		Viewing: ShareViewing{Credential: "123.abc_-", ExpiresAt: expiresAt},
	}
	got := plain.WithViewCredential()
	for name, url := range map[string]string{"stream": got.StreamURL, "compat": got.PlaybackURL, "download": got.DownloadURL, "thumbnail": got.ThumbURL} {
		if want := "/s/tok/" + name + "?view=123.abc_-"; url != want {
			t.Errorf("%s url = %q, want %q", name, url, want)
		}
	}
	if got.View != "123.abc_-" || got.ViewExpiresAt == nil || !got.ViewExpiresAt.Equal(expiresAt) {
		t.Errorf("view = %q, expires %v", got.View, got.ViewExpiresAt)
	}
	if plain.StreamURL != "/s/tok/stream" || plain.View != "" {
		t.Errorf("the original was modified: %+v", plain)
	}

	// No viewing (a probe) and no thumbnail: nothing is appended anywhere.
	bare := GetSharedMediaResult{StreamURL: "/s/tok/stream"}.WithViewCredential()
	if bare.StreamURL != "/s/tok/stream" || bare.ThumbURL != "" || bare.View != "" || bare.ViewExpiresAt != nil {
		t.Errorf("result without a viewing was changed: %+v", bare)
	}
}

// viewingFixture is a share service on a real SQLite store with a movable
// clock, holding one media item with a thumbnail.
type viewingFixture struct {
	t     *testing.T
	store *repository.SQLite
	clk   *clock.MockClock
	svc   *shareService
	media int64
	user  int64
}

func newViewingFixture(t *testing.T, dbPath string) *viewingFixture {
	t.Helper()
	store, err := repository.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	clk := newMockClock()
	f := &viewingFixture{t: t, store: store, clk: clk, svc: NewShareService(store, clk, NewAccessHelper(store))}
	f.user, err = store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "h", CreatedAt: clk.T})
	f.must(err)
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "set", CreatedAt: clk.T})
	f.must(err)
	f.media, err = store.CreateMedia(ctx, &model.Media{
		SetID: setID, RelPath: "set/a.mp4", FileName: "a.mp4", AbsPath: "/media/set/a.mp4",
		ThumbnailPath: "/media/set/.thumbnails/a.mp4.jpg", Type: model.MediaTypeVideo, CreatedAt: clk.T,
	})
	f.must(err)
	return f
}

func (f *viewingFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

// share stores a share that lives for lifetime and allows maxUses viewings
// (0 = unlimited).
func (f *viewingFixture) share(token string, lifetime time.Duration, maxUses int) {
	f.t.Helper()
	share := &model.Share{Token: token, MediaID: f.media, CreatedBy: f.user, CreatedAt: f.clk.T, ExpiresAt: f.clk.T.Add(lifetime)}
	if maxUses > 0 {
		share.MaxUses = &maxUses
	}
	f.must(f.store.CreateShare(context.Background(), share))
}

func (f *viewingFixture) used(token string) int {
	f.t.Helper()
	share, err := f.store.GetShareByToken(context.Background(), token)
	f.must(err)
	return share.UsedCount
}

// open fetches the share metadata without a credential, as a new client
// does, and returns the credential of the viewing that opened.
func (f *viewingFixture) open(token string) string {
	f.t.Helper()
	res, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: token})
	f.must(err)
	if !res.Viewing.Opened || res.Viewing.Credential == "" {
		f.t.Fatalf("metadata fetch opened no viewing: %+v", res.Viewing)
	}
	return res.Viewing.Credential
}

// stream requests the shared file and reports the error and whether the
// request opened a new viewing.
func (f *viewingFixture) stream(token, credential string) (bool, error) {
	_, viewing, err := f.svc.StreamSharedMedia(context.Background(), ShareAccess{Token: token, Credentials: []string{credential}})
	return viewing.Opened, err
}

// A viewing costs one use and then serves any number of requests, also once
// max_uses is reached — until it expires, after which the exhausted share is
// gone for this client too.
func TestShareViewing_LifetimeOnRealStore(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("tok", 30*24*time.Hour, 1)
	credential := f.open("tok")

	for i := range 5 {
		if opened, err := f.stream("tok", credential); err != nil || opened {
			t.Fatalf("request %d inside the viewing: opened=%v err=%v", i+1, opened, err)
		}
	}
	if _, err := f.svc.GetSharedThumbnail(context.Background(), "tok", credential); err != nil {
		t.Errorf("thumbnail inside the viewing: %v", err)
	}
	if res, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: "tok", Credentials: []string{credential}}); err != nil || res.Viewing.Opened || res.Viewing.Credential != credential {
		t.Errorf("metadata reload inside the viewing: %+v, err=%v", res, err)
	}
	if got := f.used("tok"); got != 1 {
		t.Fatalf("used_count = %d, want 1", got)
	}

	// Everyone without the credential finds the share exhausted.
	if _, err := f.stream("tok", ""); !errors.Is(err, ErrShareExpired) {
		t.Errorf("stream without a credential = %v, want ErrShareExpired", err)
	}
	if _, err := f.svc.GetSharedMedia(context.Background(), ShareAccess{Token: "tok"}); !errors.Is(err, ErrShareExpired) {
		t.Errorf("second metadata fetch = %v, want ErrShareExpired", err)
	}
	if _, err := f.svc.GetSharedThumbnail(context.Background(), "tok", ""); !errors.Is(err, ErrShareExpired) {
		t.Errorf("thumbnail without a credential = %v, want ErrShareExpired", err)
	}

	f.clk.T = f.clk.T.Add(ShareViewingLifetime - time.Second)
	if _, err := f.stream("tok", credential); err != nil {
		t.Fatalf("just before the viewing expires: %v", err)
	}
	f.clk.T = f.clk.T.Add(time.Second)
	if _, err := f.stream("tok", credential); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("expired viewing on an exhausted share = %v, want ErrShareExpired", err)
	}
	if got := f.used("tok"); got != 1 {
		t.Errorf("used_count = %d, want it unchanged at 1", got)
	}
}

// An expired credential is no credential: on a share with uses left the
// request opens a new viewing and pays for it.
func TestShareViewing_ExpiredCredentialOpensNewViewing(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("tok", 30*24*time.Hour, 2)
	credential := f.open("tok")
	f.clk.T = f.clk.T.Add(ShareViewingLifetime)

	opened, err := f.stream("tok", credential)
	if err != nil || !opened {
		t.Fatalf("stream with an expired credential: opened=%v err=%v, want a new viewing", opened, err)
	}
	if got := f.used("tok"); got != 2 {
		t.Errorf("used_count = %d, want 2", got)
	}
}

// The share's own expiry and its revocation end a viewing at once.
func TestShareViewing_ShareExpiryAndRevocationWin(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("short", time.Hour, 1)
	f.share("revoked", 30*24*time.Hour, 1)
	short, revoked := f.open("short"), f.open("revoked")

	f.clk.T = f.clk.T.Add(time.Hour)
	if _, err := f.stream("short", short); !errors.Is(err, ErrShareExpired) {
		t.Errorf("viewing of an expired share = %v, want ErrShareExpired", err)
	}
	f.must(f.svc.RevokeShare(context.Background(), "revoked", f.user))
	if _, err := f.stream("revoked", revoked); !errors.Is(err, ErrShareNotFound) {
		t.Errorf("viewing of a revoked share = %v, want ErrShareNotFound", err)
	}
	if _, err := f.svc.GetSharedThumbnail(context.Background(), "revoked", revoked); !errors.Is(err, ErrShareNotFound) {
		t.Errorf("thumbnail of a revoked share = %v, want ErrShareNotFound", err)
	}
}

// A credential is bound to its share: presenting it for another share is
// like presenting none.
func TestShareViewing_CredentialIsBoundToItsShare(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	f.share("one", time.Hour, 1)
	f.share("two", time.Hour, 1)
	credential := f.open("one")
	f.open("two")

	if _, err := f.stream("two", credential); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("share one's credential on exhausted share two = %v, want ErrShareExpired", err)
	}
}

// The signing key lives in the database, so viewings survive a restart: a
// service started later on the same database accepts the credential. A
// server with another database (another key) does not.
func TestShareViewing_SurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "media.db")
	f := newViewingFixture(t, dbPath)
	f.share("tok", time.Hour, 1)
	credential := f.open("tok")

	restarted := NewShareService(f.store, f.clk, NewAccessHelper(f.store))
	if _, _, err := restarted.StreamSharedMedia(context.Background(), ShareAccess{Token: "tok", Credentials: []string{credential}}); err != nil {
		t.Fatalf("credential after a restart: %v", err)
	}

	other := newViewingFixture(t, filepath.Join(t.TempDir(), "other.db"))
	other.share("tok", time.Hour, 1)
	other.open("tok")
	if _, err := other.stream("tok", credential); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("another server's credential = %v, want ErrShareExpired", err)
	}
}

func TestShareViewing_KeyErrors(t *testing.T) {
	one := 1
	exhausted := func(context.Context, string) (*model.Share, error) {
		return &model.Share{Token: "tok", MediaID: 1, ExpiresAt: newMockClock().T.Add(time.Hour), MaxUses: &one, UsedCount: 1}, nil
	}
	tests := []struct {
		name string
		key  func(context.Context) ([]byte, error)
	}{
		{"store error", func(context.Context) ([]byte, error) { return nil, errors.New("db down") }},
		{"empty key", func(context.Context) ([]byte, error) { return nil, nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uses := 0
			store := &repository.MockStore{ShareRepo: repository.MockShareRepo{
				GetShareByTokenFunc: exhausted,
				ShareViewingKeyFunc: tt.key,
				UseShareFunc: func(context.Context, string, time.Time) (bool, error) {
					uses++
					return true, nil
				},
			}}
			svc := NewShareService(store, newMockClock(), NewAccessHelper(store))
			// Neither an existing viewing can be checked nor a new one
			// signed: both fail as server errors, and no use is taken.
			if _, err := svc.ResolveSharedMedia(context.Background(), "tok", "1.x"); err == nil || errors.Is(err, ErrShareExpired) {
				t.Errorf("resolve = %v, want the key error", err)
			}
			if _, err := svc.EnsureShareViewing(context.Background(), "tok", ""); err == nil || errors.Is(err, ErrShareExpired) {
				t.Errorf("ensure = %v, want the key error", err)
			}
			if uses != 0 {
				t.Errorf("%d uses consumed without a key", uses)
			}
		})
	}
}
