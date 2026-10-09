package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestConnectionDSN(t *testing.T) {
	const fk, busy = "_pragma=foreign_keys(1)", "_pragma=busy_timeout(5000)"
	cases := map[string]string{
		":memory:":                         ":memory:?_timezone=UTC&" + fk + "&" + busy,
		"/data/media.db":                   "/data/media.db?_timezone=UTC&" + fk + "&" + busy,
		"file:x.db?cache=shared":           "file:x.db?cache=shared&_timezone=UTC&" + fk + "&" + busy,
		"x.db?_pragma=foreign_keys(0)":     "x.db?_pragma=foreign_keys(0)&_timezone=UTC&" + busy,
		"x.db?_pragma=busy_timeout(1)":     "x.db?_pragma=busy_timeout(1)&_timezone=UTC&" + fk,
		"x.db?_timezone=Europe%2FHelsinki": "x.db?_timezone=Europe%2FHelsinki&" + fk + "&" + busy,
	}
	for dsn, want := range cases {
		if got := connectionDSN(dsn); got != want {
			t.Errorf("connectionDSN(%q) = %q, want %q", dsn, got, want)
		}
	}
}

// dropConnection makes database/sql discard the pooled connection, as it
// does after a driver error or a cancelled query, so that the next statement
// runs on a freshly opened one.
func dropConnection(t *testing.T, s *SQLite) {
	t.Helper()
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func pragmaInt(t *testing.T, s *SQLite, name string) int {
	t.Helper()
	var value int
	if err := s.db.QueryRow("PRAGMA " + name).Scan(&value); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}
	return value
}

// A file database is used on purpose: a new connection to ":memory:" would
// be a new, empty database.
func openFileStore(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpen_PragmasSurviveAReplacedConnection(t *testing.T) {
	s := openFileStore(t)
	for round := 0; round < 3; round++ {
		if got := pragmaInt(t, s, "foreign_keys"); got != 1 {
			t.Fatalf("round %d: foreign_keys = %d, want 1", round, got)
		}
		if got := pragmaInt(t, s, "busy_timeout"); got != sqliteBusyTimeoutMS {
			t.Fatalf("round %d: busy_timeout = %d, want %d", round, got, sqliteBusyTimeoutMS)
		}
		dropConnection(t, s)
	}
}

// owned holds one row in every table that belongs to a user.
type owned struct {
	userID, mediaID int64
	sessionID       string
	tokenHash       string
}

func seedOwned(t *testing.T, s *SQLite, name string) owned {
	t.Helper()
	ctx, now := context.Background(), time.Now()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	userID, err := s.CreateUser(ctx, &model.User{Username: name, PasswordHash: "h", CreatedAt: now})
	must(err)
	setID, err := s.CreateSet(ctx, &model.Set{Name: "set-" + name, RootPath: "set-" + name, CreatedAt: now})
	must(err)
	mediaID, err := s.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: name + ".mp4", FileName: name + ".mp4", AbsPath: "/m/" + name + ".mp4", Type: model.MediaTypeVideo, CreatedAt: now})
	must(err)
	o := owned{userID: userID, mediaID: mediaID, sessionID: "sess-" + name, tokenHash: "hash-" + name}
	must(s.CreateSession(ctx, &model.Session{ID: o.sessionID, UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}))
	_, err = s.Create(ctx, &model.APIToken{UserID: userID, TokenHash: o.tokenHash, Name: "t", CreatedAt: now})
	must(err)
	must(s.GrantPermission(ctx, &model.SetPermission{SetID: setID, UserID: userID, Role: model.RoleViewer, CreatedAt: now}))
	_, err = s.ToggleFavorite(ctx, userID, mediaID)
	must(err)
	return o
}

// userRowCount counts the rows a user owns across userOwned.
func userRowCount(t *testing.T, s *SQLite, userID int64) int {
	t.Helper()
	total := 0
	for _, o := range userOwned {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM "+o.table+" WHERE "+o.column+" = ?", userID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", o.table, err)
		}
		total += n
	}
	return total
}

func TestDeleteUser_CascadesAfterAReplacedConnection(t *testing.T) {
	s := openFileStore(t)
	gone, kept := seedOwned(t, s, "gone"), seedOwned(t, s, "kept")
	dropConnection(t, s)
	// Only the users row is deleted here, as the cascade must do the rest.
	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, gone.userID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := userRowCount(t, s, gone.userID); n != 0 {
		t.Errorf("deleted user still owns %d rows: the cascade did not run", n)
	}
	if n := userRowCount(t, s, kept.userID); n != 4 {
		t.Errorf("other user owns %d rows, want 4", n)
	}
}

func TestDeleteUser_RemovesOwnedRowsWithoutCascade(t *testing.T) {
	s := openFileStore(t)
	gone, kept := seedOwned(t, s, "gone"), seedOwned(t, s, "kept")
	// The state every replaced connection used to be in.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(context.Background(), gone.userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if n := userRowCount(t, s, gone.userID); n != 0 {
		t.Errorf("deleted user still owns %d rows", n)
	}
	if n := userRowCount(t, s, kept.userID); n != 4 {
		t.Errorf("other user owns %d rows, want 4", n)
	}
}

func TestLookups_IgnoreRowsOfAUserThatIsGone(t *testing.T) {
	s := openFileStore(t)
	ctx := context.Background()
	gone, kept := seedOwned(t, s, "gone"), seedOwned(t, s, "kept")
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, gone.userID); err != nil {
		t.Fatal(err)
	}
	if n := userRowCount(t, s, gone.userID); n == 0 {
		t.Fatal("precondition: the orphan rows must still be there")
	}
	if sess, err := s.GetSessionByID(ctx, gone.sessionID); err != nil || sess != nil {
		t.Errorf("orphaned session = %v, %v; want nil, nil", sess, err)
	}
	if token, err := s.GetByHash(ctx, gone.tokenHash); err != nil || token != nil {
		t.Errorf("orphaned token = %v, %v; want nil, nil", token, err)
	}
	if sess, err := s.GetSessionByID(ctx, kept.sessionID); err != nil || sess == nil {
		t.Errorf("live session = %v, %v; want it found", sess, err)
	}
	if token, err := s.GetByHash(ctx, kept.tokenHash); err != nil || token == nil {
		t.Errorf("live token = %v, %v; want it found", token, err)
	}
}

func TestRemoveOrphanRows(t *testing.T) {
	s := openFileStore(t)
	goneUser, goneMedia, kept := seedOwned(t, s, "gone-user"), seedOwned(t, s, "gone-media"), seedOwned(t, s, "kept")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`PRAGMA foreign_keys = OFF`)
	// Accumulators of an ended session go; those of a live session and of a
	// live API token ("api-token:<id>") stay.
	var keptToken int64
	if err := s.db.QueryRow(`SELECT id FROM api_tokens WHERE user_id = ?`, kept.userID).Scan(&keptToken); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"ended", kept.sessionID, fmt.Sprintf("api-token:%d", keptToken)} {
		exec(`INSERT INTO playback_accumulator (session_id, media_id, accumulated_seconds) VALUES (?, ?, 5)`, session, kept.mediaID)
	}
	exec(`DELETE FROM users WHERE id = ?`, goneUser.userID)
	exec(`DELETE FROM media WHERE id = ?`, goneMedia.mediaID)

	for round := 0; round < 2; round++ { // the second round proves idempotence
		if err := removeOrphanRows(s.db); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if n := userRowCount(t, s, goneUser.userID); n != 0 {
			t.Errorf("round %d: deleted user still owns %d rows", round, n)
		}
		// The user of the deleted media keeps session, token and permission;
		// only the favourite pointed at the media.
		if n := userRowCount(t, s, goneMedia.userID); n != 3 {
			t.Errorf("round %d: owner of the deleted media has %d rows, want 3", round, n)
		}
		if n := userRowCount(t, s, kept.userID); n != 4 {
			t.Errorf("round %d: untouched user has %d rows, want 4", round, n)
		}
		var accumulators, ended int
		if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(session_id = 'ended'), 0) FROM playback_accumulator`).Scan(&accumulators, &ended); err != nil || accumulators != 2 || ended != 0 {
			t.Errorf("round %d: %d accumulators left, %d of the ended session (%v); want 2 and 0", round, accumulators, ended, err)
		}
	}
}

func TestRemoveOrphanRows_ClosedDatabase(t *testing.T) {
	s := openFileStore(t)
	_ = s.db.Close()
	if err := removeOrphanRows(s.db); err == nil {
		t.Fatal("want an error on a closed database")
	}
}
