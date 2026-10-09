package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// eest is a zone three hours ahead of UTC, as on the server where shares
// with an explicit expiry died early.
var eest = time.FixedZone("EEST", 3*60*60)

// zoneFixture is a store with one user and one media item to hang shares on.
type zoneFixture struct {
	t     *testing.T
	store *SQLite
	path  string
	user  int64
	media int64
}

func newZoneFixture(t *testing.T) *zoneFixture {
	t.Helper()
	f := &zoneFixture{t: t, path: filepath.Join(t.TempDir(), "media.db")}
	f.reopen()
	ctx, now := context.Background(), time.Now()
	var err error
	f.user, err = f.store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "h", CreatedAt: now})
	f.must(err)
	setID, err := f.store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "set", CreatedAt: now})
	f.must(err)
	f.media, err = f.store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "set/a.mp4", FileName: "a.mp4", AbsPath: "/m/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
	f.must(err)
	return f
}

func (f *zoneFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

// reopen closes the store, if open, and opens the database again, as a
// server restart does (running the startup normalisation).
func (f *zoneFixture) reopen() {
	f.t.Helper()
	if f.store != nil {
		f.must(f.store.Close())
	}
	store, err := Open(f.path)
	f.must(err)
	f.store = store
	f.t.Cleanup(func() { _ = store.Close() })
}

func (f *zoneFixture) share(token string, created, expires time.Time) {
	f.t.Helper()
	f.must(f.store.CreateShare(context.Background(), &model.Share{
		Token: token, MediaID: f.media, CreatedBy: f.user, CreatedAt: created, ExpiresAt: expires,
	}))
}

// rawTime returns a timestamp exactly as it is stored.
func (f *zoneFixture) rawTime(table, column, where string) string {
	f.t.Helper()
	var text string
	f.must(f.store.db.QueryRow(`SELECT CAST(` + column + ` AS TEXT) FROM ` + table + ` WHERE ` + where).Scan(&text))
	return text
}

// zonePairs are the zones "now" and a stored expiry can carry: the server's
// clock gives local time, an expires_at from a request body is usually UTC.
var zonePairs = []struct {
	name            string
	nowZone, expiry *time.Location
}{
	{"both UTC", time.UTC, time.UTC},
	{"local now, UTC expiry", eest, time.UTC},
	{"UTC now, local expiry", time.UTC, eest},
	{"both local", eest, eest},
}

// A share is usable until its expiry and not a moment longer, whatever zone
// the two timestamps are expressed in. (Compared as text, a UTC expiry ten
// minutes ahead of an EEST "now" looked three hours in the past.)
func TestUseShare_ExpiryIsComparedAsTimeNotText(t *testing.T) {
	instant := time.Date(2026, 10, 9, 13, 16, 35, 0, time.UTC)
	for _, zones := range zonePairs {
		t.Run(zones.name, func(t *testing.T) {
			f := newZoneFixture(t)
			now := instant.In(zones.nowZone)
			f.share("alive", now, instant.Add(10*time.Minute).In(zones.expiry))
			f.share("dead", now, instant.Add(-10*time.Minute).In(zones.expiry))
			f.share("this instant", now, instant.In(zones.expiry))

			for token, want := range map[string]bool{"alive": true, "dead": false, "this instant": false} {
				got, err := f.store.UseShare(context.Background(), token, now)
				if err != nil || got != want {
					t.Errorf("UseShare(%s) = %v, %v; want %v", token, got, err, want)
				}
			}
			// The garbage collector removes exactly the expired one.
			f.must(f.store.DeleteExpiredShares(context.Background(), now))
			for token, want := range map[string]bool{"alive": true, "dead": false, "this instant": true} {
				share, err := f.store.GetShareByToken(context.Background(), token)
				if err != nil || (share != nil) != want {
					t.Errorf("after GC: share %s exists = %v (err %v), want %v", token, share != nil, err, want)
				}
			}
		})
	}
}

// The same holds for the session garbage collector and for ORDER BY.
func TestSessionsAndOrdering_AcrossZones(t *testing.T) {
	instant := time.Date(2026, 10, 9, 13, 16, 35, 0, time.UTC)
	for _, zones := range zonePairs {
		t.Run(zones.name, func(t *testing.T) {
			f := newZoneFixture(t)
			ctx, now := context.Background(), instant.In(zones.nowZone)
			for id, expires := range map[string]time.Time{"alive": instant.Add(10 * time.Minute), "dead": instant.Add(-10 * time.Minute)} {
				f.must(f.store.CreateSession(ctx, &model.Session{ID: id, UserID: f.user, CreatedAt: now, ExpiresAt: expires.In(zones.expiry)}))
			}
			f.must(f.store.DeleteExpiredSessions(ctx, now))
			for id, want := range map[string]bool{"alive": true, "dead": false} {
				session, err := f.store.GetSessionByID(ctx, id)
				if err != nil || (session != nil) != want {
					t.Errorf("after GC: session %s exists = %v (err %v), want %v", id, session != nil, err, want)
				}
			}

			// Created one minute apart, alternating zones: newest first.
			f.share("first", instant.In(zones.nowZone), instant.Add(time.Hour))
			f.share("second", instant.Add(time.Minute).In(zones.expiry), instant.Add(time.Hour))
			f.share("third", instant.Add(2*time.Minute).In(zones.nowZone), instant.Add(time.Hour))
			shares, err := f.store.ListSharesByUser(ctx, f.user)
			f.must(err)
			if len(shares) != 3 || shares[0].Token != "third" || shares[1].Token != "second" || shares[2].Token != "first" {
				t.Errorf("shares by creation time, newest first = %+v", shares)
			}
		})
	}
}

// With a non-UTC local zone the server's clock returns local time while an
// expires_at parsed from a request is UTC: the reported failure, end to end.
func TestUseShare_NonUTCLocalZone(t *testing.T) {
	previous := time.Local
	time.Local = eest
	t.Cleanup(func() { time.Local = previous })

	f := newZoneFixture(t)
	now := time.Now()
	if _, offset := now.Zone(); offset != 3*60*60 {
		t.Fatalf("time.Now() is not in the local test zone: %v", now)
	}
	requested, err := time.Parse(time.RFC3339, now.Add(10*time.Minute).UTC().Format(time.RFC3339))
	f.must(err)
	f.share("tok", now, requested)

	if used, err := f.store.UseShare(context.Background(), "tok", time.Now()); err != nil || !used {
		t.Fatalf("share expiring in 10 minutes: UseShare = %v, %v; want it usable", used, err)
	}
	if raw := f.rawTime("shares", "created_at", "token = 'tok'"); raw[len(raw)-len(utcTimeSuffix):] != utcTimeSuffix {
		t.Errorf("created_at stored as %q, want UTC", raw)
	}
	share, err := f.store.GetShareByToken(context.Background(), "tok")
	if err != nil || !share.ExpiresAt.Equal(requested) {
		t.Errorf("expiry read back as %v (err %v), want %v", share.ExpiresAt, err, requested)
	}
}

// Rows written by older versions in the server's local zone are converted
// at startup; rows in other shapes are left alone; a second start changes
// nothing.
func TestNormalizeStoredTimes_OldRows(t *testing.T) {
	f := newZoneFixture(t)
	instant := time.Date(2026, 10, 9, 13, 16, 35, 0, time.UTC)
	for _, token := range []string{"local", "local-mono", "sqlite", "utc", "junk"} {
		f.share(token, instant, instant.Add(10*time.Minute))
	}
	stored := map[string]string{
		// time.String() of a local value, without and with the monotonic
		// clock reading of time.Now().
		"local":      "2026-10-09 16:26:35 +0300 EEST",
		"local-mono": "2026-10-09 16:26:35.123456789 +0300 EEST m=+12.500000001",
		// SQLite's CURRENT_TIMESTAMP shape: UTC already.
		"sqlite": "2026-10-09 13:26:35",
		"utc":    "2026-10-09 13:26:35 +0000 UTC",
		"junk":   "next tuesday +0300 EEST",
	}
	for token, text := range stored {
		_, err := f.store.db.Exec(`UPDATE shares SET expires_at = ? WHERE token = ?`, text, token)
		f.must(err)
	}

	want := map[string]string{
		"local":      "2026-10-09 13:26:35 +0000 UTC",
		"local-mono": "2026-10-09 13:26:35.123456789 +0000 UTC",
		"sqlite":     stored["sqlite"],
		"utc":        stored["utc"],
		"junk":       stored["junk"],
	}
	for round := 1; round <= 2; round++ {
		f.reopen()
		for token, text := range want {
			if got := f.rawTime("shares", "expires_at", "token = '"+token+"'"); got != text {
				t.Errorf("start %d: %s stored as %q, want %q", round, token, got, text)
			}
		}
	}

	// The old local rows now expire at the right moment for a local clock.
	now := instant.In(eest)
	for _, token := range []string{"local", "local-mono", "sqlite", "utc"} {
		if used, err := f.store.UseShare(context.Background(), token, now); err != nil || !used {
			t.Errorf("%s, 10 minutes before its expiry: UseShare = %v, %v", token, used, err)
		}
		if used, err := f.store.UseShare(context.Background(), token, now.Add(11*time.Minute)); err != nil || used {
			t.Errorf("%s, a minute after its expiry: UseShare = %v, %v", token, used, err)
		}
	}
}

func TestUTCTimesDSN(t *testing.T) {
	tests := map[string]string{
		"/data/media.db":                  "/data/media.db?_timezone=UTC",
		":memory:":                        ":memory:?_timezone=UTC",
		"file:media.db?cache=shared":      "file:media.db?cache=shared&_timezone=UTC",
		"media.db?_timezone=Europe/Sofia": "media.db?_timezone=Europe/Sofia",
	}
	for dsn, want := range tests {
		if got := utcTimesDSN(dsn); got != want {
			t.Errorf("utcTimesDSN(%q) = %q, want %q", dsn, got, want)
		}
	}
}
