package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestProgressMigration_BackfillsExistingSessionPlayback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "player.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "hash", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, &model.Session{ID: "old", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: userID, MediaID: mediaID, PositionSeconds: 70, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-upgrade accumulator schema to exercise the FK migration.
	if _, err := store.db.Exec(`DROP TABLE playback_accumulator;
CREATE TABLE playback_accumulator (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    last_position REAL NOT NULL DEFAULT 0,
    accumulated_seconds REAL NOT NULL DEFAULT 0,
    counted INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, media_id)
);`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: "old", MediaID: mediaID, LastPosition: 70, AccumulatedSeconds: 70, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`ALTER TABLE playback_progress DROP COLUMN accumulated_seconds`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	for pass := range 2 {
		store, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		progress, err := store.GetProgress(ctx, userID, mediaID)
		want := float64(70)
		if pass == 1 {
			want = 0
		}
		if err != nil || progress.AccumulatedSeconds != want {
			t.Fatalf("migration should run only once: progress=%+v want=%v err=%v", progress, want, err)
		}
		if err := store.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: "api-token:1", MediaID: mediaID, LastPosition: 30, AccumulatedSeconds: 30, UpdatedAt: now}); err != nil {
			t.Fatalf("synthetic token session should be accepted after migration: %v", err)
		}
		if pass == 0 {
			if err := store.MarkFinished(ctx, userID, mediaID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE playback_progress SET finished = 0 WHERE user_id = ? AND media_id = ?`, userID, mediaID); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteUser_RemovesCookieAndTokenAccumulators(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var deletedUser int64
	var retainedTokenSession string
	for _, username := range []string{"deleted", "retained"} {
		userID, err := store.CreateUser(ctx, &model.User{Username: username, PasswordHash: "hash", CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if username == "deleted" {
			deletedUser = userID
		}
		cookie := username + "-cookie"
		if err := store.CreateSession(ctx, &model.Session{ID: cookie, UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		tokenID, err := store.Create(ctx, &model.APIToken{UserID: userID, TokenHash: username + "-hash", Name: "mobile", CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if username == "retained" {
			retainedTokenSession = fmt.Sprintf("api-token:%d", tokenID)
		}
		for _, sessionID := range []string{cookie, fmt.Sprintf("api-token:%d", tokenID)} {
			if err := store.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: sessionID, MediaID: mediaID, AccumulatedSeconds: 30, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := store.DeleteUser(ctx, deletedUser); err != nil {
		t.Fatal(err)
	}
	var remaining []string
	rows, err := store.db.QueryContext(ctx, `SELECT session_id FROM playback_accumulator ORDER BY session_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		remaining = append(remaining, sessionID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0] != retainedTokenSession || remaining[1] != "retained-cookie" {
		t.Fatalf("unexpected surviving accumulators: %v", remaining)
	}
}

func TestResetProgress_RollsBackAccumulatorDeletion(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "hash", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, &model.Session{ID: "cookie", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: userID, MediaID: mediaID, PositionSeconds: 30, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: "cookie", MediaID: mediaID, AccumulatedSeconds: 30, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER block_progress_delete BEFORE DELETE ON playback_progress BEGIN SELECT RAISE(FAIL, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetProgress(ctx, userID, mediaID); err == nil {
		t.Fatal("expected reset to fail")
	}
	acc, err := store.GetAccumulator(ctx, "cookie", mediaID)
	if err != nil || acc == nil {
		t.Fatalf("accumulator deletion should roll back: acc=%+v err=%v", acc, err)
	}
}
