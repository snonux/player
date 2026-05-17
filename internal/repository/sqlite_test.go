package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	return s
}

func TestSQLite_UserRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create and get by id",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, err := s.CreateUser(ctx, &model.User{Username: "alice", PasswordHash: "h", IsAdmin: true, CreatedAt: now})
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				u, err := s.GetUserByID(ctx, id)
				if err != nil {
					t.Fatalf("get by id: %v", err)
				}
				if u.Username != "alice" || !u.IsAdmin {
					t.Fatalf("unexpected user: %+v", u)
				}
			},
		},
		{
			name: "get by username",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, _ := s.CreateUser(ctx, &model.User{Username: "bob", PasswordHash: "h", CreatedAt: now})
				u, err := s.GetUserByUsername(ctx, "bob")
				if err != nil {
					t.Fatalf("get by username: %v", err)
				}
				if u.ID != id {
					t.Fatalf("id mismatch")
				}
			},
		},
		{
			name: "list and count",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				_, _ = s.CreateUser(ctx, &model.User{Username: "u1", PasswordHash: "h", CreatedAt: now})
				_, _ = s.CreateUser(ctx, &model.User{Username: "u2", PasswordHash: "h", CreatedAt: now})
				cnt, err := s.CountUsers(ctx)
				if err != nil {
					t.Fatalf("count: %v", err)
				}
				if cnt != 2 {
					t.Fatalf("expected 2, got %d", cnt)
				}
				users, err := s.ListUsers(ctx)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(users) != 2 {
					t.Fatalf("expected 2, got %d", len(users))
				}
			},
		},
		{
			name: "delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, _ := s.CreateUser(ctx, &model.User{Username: "del", PasswordHash: "h", CreatedAt: now})
				if err := s.DeleteUser(ctx, id); err != nil {
					t.Fatalf("delete: %v", err)
				}
				u, err := s.GetUserByID(ctx, id)
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if u != nil {
					t.Fatal("expected nil user after delete")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_SetRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create and get",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, err := s.CreateSet(ctx, &model.Set{Name: "vids", RootPath: "/vids", CreatedAt: now})
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				st, err := s.GetSetByID(ctx, id)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if st.Name != "vids" {
					t.Fatalf("unexpected name: %s", st.Name)
				}
			},
		},
		{
			name: "update",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				st := &model.Set{ID: id, Name: "s2", RootPath: "/s2", CoverThumbnailPath: "/t.jpg"}
				if err := s.UpdateSet(ctx, st); err != nil {
					t.Fatalf("update: %v", err)
				}
				got, _ := s.GetSetByID(ctx, id)
				if got.Name != "s2" || got.CoverThumbnailPath != "/t.jpg" {
					t.Fatalf("unexpected update: %+v", got)
				}
			},
		},
		{
			name: "list",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				_, _ = s.CreateSet(ctx, &model.Set{Name: "a", RootPath: "/a", CreatedAt: now})
				_, _ = s.CreateSet(ctx, &model.Set{Name: "b", RootPath: "/b", CreatedAt: now})
				sets, err := s.ListSets(ctx)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(sets) != 2 {
					t.Fatalf("expected 2, got %d", len(sets))
				}
			},
		},
		{
			name: "delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, _ := s.CreateSet(ctx, &model.Set{Name: "del", RootPath: "/del", CreatedAt: now})
				if err := s.DeleteSet(ctx, id); err != nil {
					t.Fatalf("delete: %v", err)
				}
				st, err := s.GetSetByID(ctx, id)
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if st != nil {
					t.Fatal("expected nil set after delete")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_MediaRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create and get",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, err := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				m, err := s.GetMediaByID(ctx, mid)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if m.FileName != "a.mp4" {
					t.Fatalf("unexpected media: %+v", m)
				}
			},
		},
		{
			name: "update and increment play count",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				m, _ := s.GetMediaByID(ctx, mid)
				m.PlayCount = 5
				if err := s.UpdateMedia(ctx, m); err != nil {
					t.Fatalf("update: %v", err)
				}
				if err := s.IncrementPlayCount(ctx, mid); err != nil {
					t.Fatalf("increment: %v", err)
				}
				got, _ := s.GetMediaByID(ctx, mid)
				if got.PlayCount != 6 {
					t.Fatalf("expected 6, got %d", got.PlayCount)
				}
			},
		},
		{
			name: "soft delete restore",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.SoftDeleteMedia(ctx, mid); err != nil {
					t.Fatalf("soft delete: %v", err)
				}
				active, _ := s.ListMedia(ctx, MediaFilter{})
				if len(active) != 0 {
					t.Fatalf("expected 0 active, got %d", len(active))
				}
				deleted, _ := s.ListDeletedMedia(ctx)
				if len(deleted) != 1 {
					t.Fatalf("expected 1 deleted, got %d", len(deleted))
				}
				if err := s.RestoreMedia(ctx, mid); err != nil {
					t.Fatalf("restore: %v", err)
				}
				active, _ = s.ListMedia(ctx, MediaFilter{})
				if len(active) != 1 {
					t.Fatalf("expected 1 active after restore, got %d", len(active))
				}
			},
		},
		{
			name: "excludes soft-deleted from get by id",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.SoftDeleteMedia(ctx, mid); err != nil {
					t.Fatalf("soft delete: %v", err)
				}
				m, err := s.GetMediaByID(ctx, mid)
				if err != nil {
					t.Fatalf("expected no error for soft-deleted media, got %v", err)
				}
				if m != nil {
					t.Fatalf("expected nil media for soft-deleted record, got %+v", m)
				}
				deleted, _ := s.ListDeletedMedia(ctx)
				if len(deleted) != 1 {
					t.Fatalf("expected 1 deleted media, got %d", len(deleted))
				}
				if deleted[0].ID != mid {
					t.Fatalf("expected deleted media ID %d, got %d", mid, deleted[0].ID)
				}
			},
		},
		{
			name: "hard delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.HardDeleteMedia(ctx, mid); err != nil {
					t.Fatalf("hard delete: %v", err)
				}
				m, err := s.GetMediaByID(ctx, mid)
				if err != nil {
					t.Fatalf("expected no error after hard delete, got %v", err)
				}
				if m != nil {
					t.Fatal("expected nil media after hard delete")
				}
			},
		},
		{
			name: "list filter by set id",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				res, err := s.ListMedia(ctx, MediaFilter{SetID: &sid})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 {
					t.Fatalf("expected 1, got %d", len(res))
				}
			},
		},
		{
			name: "search escapes LIKE wildcards",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				// Create media with names that include literal wildcard characters.
				m1, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "ab_c.mp4", FileName: "ab_c.mp4", AbsPath: "/s/ab_c.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				m2, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "de%f.mp4", FileName: "de%f.mp4", AbsPath: "/s/de%f.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				m3, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "gh\\ij.mp4", FileName: "gh\\ij.mp4", AbsPath: "/s/gh\\ij.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "normal.mp4", FileName: "normal.mp4", AbsPath: "/s/normal.mp4", Type: model.MediaTypeVideo, CreatedAt: now})

				for _, tc := range []struct {
					search   string
					expected []int64
				}{
					{"ab_c", []int64{m1}},
					{"de%f", []int64{m2}},
					{"gh\\ij", []int64{m3}},
					{"_", []int64{m1}},  // literal underscore must match only ab_c.mp4
					{"%", []int64{m2}},  // literal percent must match only de%f.mp4
					{"\\", []int64{m3}}, // literal backslash must match only gh\ij.mp4
				} {
					res, err := s.ListMedia(ctx, MediaFilter{Search: tc.search})
					if err != nil {
						t.Fatalf("search %q: %v", tc.search, err)
					}
					if len(res) != len(tc.expected) {
						t.Fatalf("search %q: expected %d results, got %d", tc.search, len(tc.expected), len(res))
					}
					got := make(map[int64]struct{}, len(res))
					for _, r := range res {
						got[r.ID] = struct{}{}
					}
					for _, id := range tc.expected {
						if _, ok := got[id]; !ok {
							t.Fatalf("search %q: expected media id %d in results", tc.search, id)
						}
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_TagRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create get list delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				id, err := s.CreateTag(ctx, "action")
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				got, err := s.GetTagByID(ctx, id)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if got.Name != "action" {
					t.Fatalf("unexpected tag: %+v", got)
				}
				byName, _ := s.GetTagByName(ctx, "action")
				if byName.ID != id {
					t.Fatal("id mismatch by name")
				}
				tags, _ := s.ListTags(ctx)
				if len(tags) != 1 {
					t.Fatalf("expected 1 tag, got %d", len(tags))
				}
				if err := s.DeleteTag(ctx, id); err != nil {
					t.Fatalf("delete: %v", err)
				}
				tag, err := s.GetTagByID(ctx, id)
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if tag != nil {
					t.Fatal("expected nil tag after delete")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_FavoriteRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "toggle favorite",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				fav, err := s.ToggleFavorite(ctx, uid, mid)
				if err != nil {
					t.Fatalf("toggle: %v", err)
				}
				if !fav {
					t.Fatal("expected true")
				}
				ok, _ := s.IsFavorite(ctx, uid, mid)
				if !ok {
					t.Fatal("expected favorite")
				}
				favs, _ := s.ListFavoritesByUser(ctx, uid)
				if len(favs) != 1 {
					t.Fatalf("expected 1 favorite, got %d", len(favs))
				}
				fav, _ = s.ToggleFavorite(ctx, uid, mid)
				if fav {
					t.Fatal("expected false after toggle")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_PlaybackProgressRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "upsert get list",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.UpsertProgress(ctx, &model.PlaybackProgress{UserID: uid, MediaID: mid, PositionSeconds: 42, UpdatedAt: now}); err != nil {
					t.Fatalf("upsert: %v", err)
				}
				p, err := s.GetProgress(ctx, uid, mid)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if p.PositionSeconds != 42 {
					t.Fatalf("expected 42, got %f", p.PositionSeconds)
				}
				if p.Finished {
					t.Fatal("expected unfinished")
				}
				if err := s.MarkFinished(ctx, uid, mid); err != nil {
					t.Fatalf("mark finished: %v", err)
				}
				p, err = s.GetProgress(ctx, uid, mid)
				if err != nil {
					t.Fatalf("get after mark finished: %v", err)
				}
				if !p.Finished {
					t.Fatal("expected finished")
				}
				pp, _ := s.ListProgressByUser(ctx, uid)
				if len(pp) != 1 {
					t.Fatalf("expected 1, got %d", len(pp))
				}
				if !pp[0].Finished {
					t.Fatal("expected listed progress to be finished")
				}
				if err := s.DeleteProgress(ctx, uid, mid); err != nil {
					t.Fatalf("delete progress: %v", err)
				}
				p, err = s.GetProgress(ctx, uid, mid)
				if err != nil {
					t.Fatalf("get after delete: %v", err)
				}
				if p != nil {
					t.Fatal("expected nil progress after delete")
				}
			},
		},
		{
			name: "list in-progress media respects allowed sets and excludes finished and deleted",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				setAllowed, _ := s.CreateSet(ctx, &model.Set{Name: "allowed", RootPath: "/allowed", CreatedAt: now})
				setOther, _ := s.CreateSet(ctx, &model.Set{Name: "other", RootPath: "/other", CreatedAt: now})
				keepID, _ := s.CreateMedia(ctx, &model.Media{SetID: setAllowed, RelPath: "keep.mp4", FileName: "keep.mp4", AbsPath: "/allowed/keep.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				finishedID, _ := s.CreateMedia(ctx, &model.Media{SetID: setAllowed, RelPath: "finished.mp4", FileName: "finished.mp4", AbsPath: "/allowed/finished.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				deletedID, _ := s.CreateMedia(ctx, &model.Media{SetID: setAllowed, RelPath: "deleted.mp4", FileName: "deleted.mp4", AbsPath: "/allowed/deleted.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				otherID, _ := s.CreateMedia(ctx, &model.Media{SetID: setOther, RelPath: "other.mp4", FileName: "other.mp4", AbsPath: "/other/other.mp4", Type: model.MediaTypeVideo, CreatedAt: now})

				progress := []model.PlaybackProgress{
					{UserID: uid, MediaID: keepID, PositionSeconds: 10, UpdatedAt: now.Add(3 * time.Second)},
					{UserID: uid, MediaID: finishedID, PositionSeconds: 20, Finished: true, UpdatedAt: now.Add(2 * time.Second)},
					{UserID: uid, MediaID: deletedID, PositionSeconds: 30, UpdatedAt: now.Add(time.Second)},
					{UserID: uid, MediaID: otherID, PositionSeconds: 40, UpdatedAt: now},
				}
				for i := range progress {
					if err := s.UpsertProgress(ctx, &progress[i]); err != nil {
						t.Fatalf("upsert progress %d: %v", i, err)
					}
				}
				if err := s.SoftDeleteMedia(ctx, deletedID); err != nil {
					t.Fatalf("soft delete: %v", err)
				}

				media, err := s.ListInProgressMedia(ctx, uid, MediaFilter{AllowedSetIDs: []int64{setAllowed}})
				if err != nil {
					t.Fatalf("list in-progress media: %v", err)
				}
				if len(media) != 1 {
					t.Fatalf("expected 1 in-progress media, got %d: %+v", len(media), media)
				}
				if media[0].ID != keepID {
					t.Fatalf("expected media %d, got %d", keepID, media[0].ID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_PlaybackAccumulatorRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "upsert get",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				if err := s.CreateSession(ctx, &model.Session{ID: "sess", UserID: uid, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
					t.Fatalf("create session: %v", err)
				}
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: "sess", MediaID: mid, LastPosition: 10, AccumulatedSeconds: 20, UpdatedAt: now}); err != nil {
					t.Fatalf("upsert: %v", err)
				}
				acc, err := s.GetAccumulator(ctx, "sess", mid)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if acc.AccumulatedSeconds != 20 {
					t.Fatalf("expected 20, got %f", acc.AccumulatedSeconds)
				}
			},
		},
		{
			name: "delete by media deletes all sessions for media only",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				for _, sessionID := range []string{"sess1", "sess2"} {
					if err := s.CreateSession(ctx, &model.Session{ID: sessionID, UserID: uid, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
						t.Fatalf("create session %s: %v", sessionID, err)
					}
				}
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mediaID, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				otherMediaID, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s/b.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				accs := []model.PlaybackAccumulator{
					{SessionID: "sess1", MediaID: mediaID, LastPosition: 10, AccumulatedSeconds: 20, UpdatedAt: now},
					{SessionID: "sess2", MediaID: mediaID, LastPosition: 11, AccumulatedSeconds: 21, UpdatedAt: now},
					{SessionID: "sess1", MediaID: otherMediaID, LastPosition: 12, AccumulatedSeconds: 22, UpdatedAt: now},
				}
				for i := range accs {
					if err := s.UpsertAccumulator(ctx, &accs[i]); err != nil {
						t.Fatalf("upsert accumulator %d: %v", i, err)
					}
				}
				if err := s.DeleteAccumulatorByMedia(ctx, mediaID); err != nil {
					t.Fatalf("delete accumulator by media: %v", err)
				}
				for _, sessionID := range []string{"sess1", "sess2"} {
					acc, err := s.GetAccumulator(ctx, sessionID, mediaID)
					if err != nil {
						t.Fatalf("get deleted accumulator %s: %v", sessionID, err)
					}
					if acc != nil {
						t.Fatalf("expected accumulator %s/%d to be deleted", sessionID, mediaID)
					}
				}
				acc, err := s.GetAccumulator(ctx, "sess1", otherMediaID)
				if err != nil {
					t.Fatalf("get other accumulator: %v", err)
				}
				if acc == nil {
					t.Fatal("expected other media accumulator to remain")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_SessionRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create get delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				if err := s.CreateSession(ctx, &model.Session{ID: "abc", UserID: uid, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
					t.Fatalf("create: %v", err)
				}
				got, err := s.GetSessionByID(ctx, "abc")
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if got.ID != "abc" {
					t.Fatalf("unexpected: %+v", got)
				}
				if err := s.DeleteSession(ctx, "abc"); err != nil {
					t.Fatalf("delete: %v", err)
				}
				sess, err := s.GetSessionByID(ctx, "abc")
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if sess != nil {
					t.Fatal("expected nil session after delete")
				}
			},
		},
		{
			name: "delete expired",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				_ = s.CreateSession(ctx, &model.Session{ID: "old", UserID: uid, ExpiresAt: now.Add(-time.Hour), CreatedAt: now})
				_ = s.CreateSession(ctx, &model.Session{ID: "new", UserID: uid, ExpiresAt: now.Add(time.Hour), CreatedAt: now})
				if err := s.DeleteExpiredSessions(ctx, now); err != nil {
					t.Fatalf("delete expired: %v", err)
				}
				sess, err := s.GetSessionByID(ctx, "old")
				if err != nil {
					t.Fatalf("expected no error for old session, got %v", err)
				}
				if sess != nil {
					t.Fatal("expected old session gone")
				}
				sess, err = s.GetSessionByID(ctx, "new")
				if err != nil {
					t.Fatalf("expected new session present: %v", err)
				}
				if sess == nil {
					t.Fatal("expected new session present")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_ShareRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "create get use delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				share := &model.Share{Token: "tok1", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
				if err := s.CreateShare(ctx, share); err != nil {
					t.Fatalf("create: %v", err)
				}
				got, err := s.GetShareByToken(ctx, "tok1")
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if got.Token != "tok1" {
					t.Fatalf("unexpected: %+v", got)
				}
				if err := s.UseShare(ctx, "tok1"); err != nil {
					t.Fatalf("use: %v", err)
				}
				got, _ = s.GetShareByToken(ctx, "tok1")
				if got.UsedCount != 1 {
					t.Fatalf("expected 1, got %d", got.UsedCount)
				}
				shares, _ := s.ListSharesByMedia(ctx, mid)
				if len(shares) != 1 {
					t.Fatalf("expected 1 share, got %d", len(shares))
				}
				if err := s.DeleteShare(ctx, "tok1"); err != nil {
					t.Fatalf("delete: %v", err)
				}
				sh, err := s.GetShareByToken(ctx, "tok1")
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if sh != nil {
					t.Fatal("expected nil share after delete")
				}
			},
		},
		{
			name: "delete expired",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_ = s.CreateShare(ctx, &model.Share{Token: "old", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(-time.Hour)})
				_ = s.CreateShare(ctx, &model.Share{Token: "new", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
				if err := s.DeleteExpiredShares(ctx, now); err != nil {
					t.Fatalf("delete expired: %v", err)
				}
				sh, err := s.GetShareByToken(ctx, "old")
				if err != nil {
					t.Fatalf("expected no error for old share, got %v", err)
				}
				if sh != nil {
					t.Fatal("expected old share gone")
				}
				sh, err = s.GetShareByToken(ctx, "new")
				if err != nil {
					t.Fatalf("expected new share present: %v", err)
				}
				if sh == nil {
					t.Fatal("expected new share present")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_NoteRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "upsert get delete",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				if err := s.UpsertNote(ctx, &model.Note{MediaID: mid, UserID: uid, Content: "hello", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatalf("upsert: %v", err)
				}
				note, err := s.GetNote(ctx, mid, uid)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if note.Content != "hello" {
					t.Fatalf("unexpected: %s", note.Content)
				}
				if err := s.UpsertNote(ctx, &model.Note{MediaID: mid, UserID: uid, Content: "world", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatalf("upsert update: %v", err)
				}
				note, _ = s.GetNote(ctx, mid, uid)
				if note.Content != "world" {
					t.Fatalf("expected world, got %s", note.Content)
				}
				if err := s.DeleteNote(ctx, mid, uid); err != nil {
					t.Fatalf("delete: %v", err)
				}
				note, err = s.GetNote(ctx, mid, uid)
				if err != nil {
					t.Fatalf("expected no error after delete, got %v", err)
				}
				if note != nil {
					t.Fatal("expected nil note after delete")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_MediaTagRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "assign list remove",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				tid, _ := s.CreateTag(ctx, "action")
				if err := s.AssignTag(ctx, mid, tid); err != nil {
					t.Fatalf("assign: %v", err)
				}
				tags, err := s.ListTagsByMedia(ctx, mid)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(tags) != 1 {
					t.Fatalf("expected 1 tag, got %d", len(tags))
				}
				if err := s.RemoveTag(ctx, mid, tid); err != nil {
					t.Fatalf("remove: %v", err)
				}
				tags, _ = s.ListTagsByMedia(ctx, mid)
				if len(tags) != 0 {
					t.Fatalf("expected 0 tags after remove, got %d", len(tags))
				}
			},
		},
		{
			name: "list media with tag filter",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				m1, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				m2, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s/b.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				t1, _ := s.CreateTag(ctx, "action")
				t2, _ := s.CreateTag(ctx, "comedy")
				_ = s.AssignTag(ctx, m1, t1)
				_ = s.AssignTag(ctx, m1, t2)
				_ = s.AssignTag(ctx, m2, t1)
				res, err := s.ListMedia(ctx, MediaFilter{Tags: []string{"action"}})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 2 {
					t.Fatalf("expected 2, got %d", len(res))
				}
				res, _ = s.ListMedia(ctx, MediaFilter{Tags: []string{"action", "comedy"}})
				if len(res) != 1 {
					t.Fatalf("expected 1 with both tags, got %d", len(res))
				}
				res, _ = s.ListMedia(ctx, MediaFilter{Tags: []string{"action"}, SetID: &sid})
				if len(res) != 2 {
					t.Fatalf("expected 2 with set filter, got %d", len(res))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_SetPermissionRepo(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "grant get list revoke",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				if err := s.GrantPermission(ctx, &model.SetPermission{SetID: sid, UserID: uid, Role: model.RoleOwner, CreatedAt: now}); err != nil {
					t.Fatalf("grant: %v", err)
				}
				perm, err := s.GetPermission(ctx, sid, uid)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if perm.Role != model.RoleOwner {
					t.Fatalf("unexpected role: %s", perm.Role)
				}
				ps, _ := s.ListPermissionsBySet(ctx, sid)
				if len(ps) != 1 {
					t.Fatalf("expected 1 by set, got %d", len(ps))
				}
				ps, _ = s.ListPermissionsByUser(ctx, uid)
				if len(ps) != 1 {
					t.Fatalf("expected 1 by user, got %d", len(ps))
				}
				if err := s.RevokePermission(ctx, sid, uid); err != nil {
					t.Fatalf("revoke: %v", err)
				}
				perm, err = s.GetPermission(ctx, sid, uid)
				if err != nil {
					t.Fatalf("expected no error after revoke, got %v", err)
				}
				if perm != nil {
					t.Fatal("expected nil permission after revoke")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_MediaFilters(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "favorites filter",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				uid, _ := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_, _ = s.ToggleFavorite(ctx, uid, mid)
				res, err := s.ListMedia(ctx, MediaFilter{Favorites: true, UserID: uid})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 {
					t.Fatalf("expected 1 favorite media, got %d", len(res))
				}
			},
		},
		{
			name: "type filter",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp3", FileName: "b.mp3", AbsPath: "/s/b.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
				audio := model.MediaTypeAudio
				res, err := s.ListMedia(ctx, MediaFilter{Type: &audio})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 || res[0].FileName != "b.mp3" {
					t.Fatalf("unexpected result: %+v", res)
				}
			},
		},
		{
			name: "duration and sort filters",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, Duration: 100, PlayCount: 5, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s/b.mp4", Type: model.MediaTypeVideo, Duration: 200, PlayCount: 1, CreatedAt: now.Add(time.Hour)})
				minDur := 150.0
				res, err := s.ListMedia(ctx, MediaFilter{MinDuration: &minDur, Sort: "duration"})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 || res[0].FileName != "b.mp4" {
					t.Fatalf("unexpected result: %+v", res)
				}
				res, _ = s.ListMedia(ctx, MediaFilter{Sort: "play_count"})
				if len(res) != 2 || res[0].FileName != "a.mp4" {
					t.Fatalf("unexpected play_count sort: %+v", res)
				}
				res, _ = s.ListMedia(ctx, MediaFilter{Sort: "date"})
				if len(res) != 2 || res[0].FileName != "b.mp4" {
					t.Fatalf("unexpected date sort: %+v", res)
				}
				res, _ = s.ListMedia(ctx, MediaFilter{Sort: "random"})
				if len(res) != 2 {
					t.Fatalf("unexpected random sort count: %d", len(res))
				}
			},
		},
		{
			name: "max duration filter",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, Duration: 100, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s/b.mp4", Type: model.MediaTypeVideo, Duration: 200, CreatedAt: now})
				maxDur := 150.0
				res, err := s.ListMedia(ctx, MediaFilter{MaxDuration: &maxDur, Sort: "duration"})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 || res[0].FileName != "a.mp4" {
					t.Fatalf("unexpected result: %+v", res)
				}
			},
		},
		{
			name: "limit offset",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s/b.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				res, err := s.ListMedia(ctx, MediaFilter{Limit: 1, Offset: 1})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 {
					t.Fatalf("expected 1, got %d", len(res))
				}
			},
		},
		{
			name: "allowed set ids",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				s1, _ := s.CreateSet(ctx, &model.Set{Name: "s1", RootPath: "/s1", CreatedAt: now})
				s2, _ := s.CreateSet(ctx, &model.Set{Name: "s2", RootPath: "/s2", CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: s1, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s1/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				_, _ = s.CreateMedia(ctx, &model.Media{SetID: s2, RelPath: "b.mp4", FileName: "b.mp4", AbsPath: "/s2/b.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				res, err := s.ListMedia(ctx, MediaFilter{AllowedSetIDs: []int64{s1}})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(res) != 1 || res[0].FileName != "a.mp4" {
					t.Fatalf("unexpected result: %+v", res)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_Ping(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestSQLite_Helpers(t *testing.T) {
	if got := sqlNullTime(nil); got.Valid {
		t.Fatal("expected sqlNullTime(nil) to be invalid")
	}
	if got := sqlNullInt(nil); got.Valid {
		t.Fatal("expected sqlNullInt(nil) to be invalid")
	}
}

func TestSQLite_MiscRepos(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "IsFavorite false for missing",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				ok, err := s.IsFavorite(ctx, 1, 1)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ok {
					t.Fatal("expected false")
				}
			},
		},
		{
			name: "ListFavoritesByUser empty",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				favs, err := s.ListFavoritesByUser(ctx, 1)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(favs) != 0 {
					t.Fatalf("expected 0, got %d", len(favs))
				}
			},
		},
		{
			name: "CountUsers zero",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				cnt, err := s.CountUsers(ctx)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cnt != 0 {
					t.Fatalf("expected 0, got %d", cnt)
				}
			},
		},
		{
			name: "DeleteTag and scanSet cover",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				id, _ := s.CreateTag(ctx, "action")
				if err := s.DeleteTag(ctx, id); err != nil {
					t.Fatalf("delete tag: %v", err)
				}
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				if err := s.UpdateSet(ctx, &model.Set{ID: sid, Name: "s2", RootPath: "/s2", CoverThumbnailPath: "/cover.jpg"}); err != nil {
					t.Fatalf("update set: %v", err)
				}
				got, _ := s.GetSetByID(ctx, sid)
				if got.CoverThumbnailPath != "/cover.jpg" {
					t.Fatalf("unexpected cover path: %s", got.CoverThumbnailPath)
				}
			},
		},
		{
			name: "AssignTag and RemoveTag",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				tid, _ := s.CreateTag(ctx, "rock")
				if err := s.AssignTag(ctx, mid, tid); err != nil {
					t.Fatalf("assign: %v", err)
				}
				if err := s.RemoveTag(ctx, mid, tid); err != nil {
					t.Fatalf("remove: %v", err)
				}
				tags, _ := s.ListTagsByMedia(ctx, mid)
				if len(tags) != 0 {
					t.Fatalf("expected 0 tags, got %d", len(tags))
				}
			},
		},
		{
			name: "UpdateMedia with all fields",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				now := time.Now().Truncate(time.Second)
				sid, _ := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now})
				mid, _ := s.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo, CreatedAt: now})
				m, _ := s.GetMediaByID(ctx, mid)
				m.Duration = 120
				m.Codec = "h264"
				m.Resolution = "1920x1080"
				m.Bitrate = 5000
				m.FileSizeBytes = 1000
				m.ThumbnailPath = "/t.jpg"
				m.PlayCount = 3
				if err := s.UpdateMedia(ctx, m); err != nil {
					t.Fatalf("update: %v", err)
				}
				got, _ := s.GetMediaByID(ctx, mid)
				if got.Duration != 120 || got.Codec != "h264" || got.Resolution != "1920x1080" || got.Bitrate != 5000 || got.FileSizeBytes != 1000 || got.ThumbnailPath != "/t.jpg" || got.PlayCount != 3 {
					t.Fatalf("unexpected update: %+v", got)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			defer s.Close()
			tt.run(t, context.Background(), s)
		})
	}
}

func TestSQLite_OpenFailures(t *testing.T) {
	t.Run("invalid dsn", func(t *testing.T) {
		_, err := Open("/dev/null/invalid")
		if err == nil {
			t.Fatal("expected error for invalid dsn")
		}
	})

	t.Run("closed db schema initialization failure", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		db.Close()
		_, err = New(db)
		if err == nil {
			t.Fatal("expected error when initializing schema on closed db")
		}
	})
}

func TestSQLite_SchemaInitialization(t *testing.T) {
	t.Run("open configures sqlite for single-process nfs use", func(t *testing.T) {
		s := newTestStore(t)
		defer s.Close()

		stats := s.db.Stats()
		if stats.MaxOpenConnections != 1 {
			t.Fatalf("expected max open connections 1, got %d", stats.MaxOpenConnections)
		}

		var timeout int
		if err := s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatalf("busy timeout pragma: %v", err)
		}
		if timeout != sqliteBusyTimeoutMS {
			t.Fatalf("expected busy_timeout %d, got %d", sqliteBusyTimeoutMS, timeout)
		}
	})

	t.Run("fresh database includes podcast column and foreign keys", func(t *testing.T) {
		s := newTestStore(t)
		defer s.Close()

		var isPodcastColumn int
		rows, err := s.db.Query(`PRAGMA table_info(sets)`)
		if err != nil {
			t.Fatalf("table info: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var defaultValue sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
				t.Fatalf("scan column: %v", err)
			}
			if name == "is_podcast" {
				isPodcastColumn++
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		if isPodcastColumn != 1 {
			t.Fatalf("expected one is_podcast column, got %d", isPodcastColumn)
		}

		var foreignKeys int
		if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatalf("foreign_keys pragma: %v", err)
		}
		if foreignKeys != 1 {
			t.Fatalf("expected foreign keys enabled, got %d", foreignKeys)
		}
	})

	t.Run("stale pre-podcast sets schema is upgraded", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer db.Close()

		_, err = db.Exec(`
CREATE TABLE sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    root_path TEXT UNIQUE NOT NULL,
    cover_thumbnail_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`)
		if err != nil {
			t.Fatalf("create stale schema: %v", err)
		}

		s, err := New(db)
		if err != nil {
			t.Fatalf("initialize stale schema: %v", err)
		}
		defer s.Close()

		var isPodcastColumn int
		rows, err := s.db.Query(`PRAGMA table_info(sets)`)
		if err != nil {
			t.Fatalf("table info: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var defaultValue sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
				t.Fatalf("scan column: %v", err)
			}
			if name == "is_podcast" {
				isPodcastColumn++
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		if isPodcastColumn != 1 {
			t.Fatalf("expected one is_podcast column, got %d", isPodcastColumn)
		}
	})
}

func TestSQLite_ErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, s *SQLite)
	}{
		{
			name: "Ping on closed store",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.Ping(ctx)
				if err == nil {
					t.Fatal("expected error pinging closed store")
				}
			},
		},
		{
			name: "CreateUser error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h"})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CreateSet error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s"})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CreateMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.CreateMedia(ctx, &model.Media{SetID: 1, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CreateTag error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.CreateTag(ctx, "rock")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CreateSession error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.CreateSession(ctx, &model.Session{ID: "abc", UserID: 1, ExpiresAt: time.Now()})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CreateShare error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.CreateShare(ctx, &model.Share{Token: "t", MediaID: 1, CreatedBy: 1, ExpiresAt: time.Now()})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UpsertNote error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 1, Content: "hi"})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UpsertProgress error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UpsertProgress(ctx, &model.PlaybackProgress{UserID: 1, MediaID: 1, PositionSeconds: 10})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UpsertAccumulator error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UpsertAccumulator(ctx, &model.PlaybackAccumulator{SessionID: "s", MediaID: 1})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GrantPermission error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.GrantPermission(ctx, &model.SetPermission{SetID: 1, UserID: 1, Role: model.RoleOwner})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ToggleFavorite error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ToggleFavorite(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListMedia(ctx, MediaFilter{})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListSets error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListSets(ctx)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListUsers error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListUsers(ctx)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListTags error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListTags(ctx)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListDeletedMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListDeletedMedia(ctx)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListProgressByUser error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListProgressByUser(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListSharesByMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListSharesByMedia(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListPermissionsBySet error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListPermissionsBySet(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListPermissionsByUser error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListPermissionsByUser(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListFavoritesByUser error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListFavoritesByUser(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "ListTagsByMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.ListTagsByMedia(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UpdateMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UpdateMedia(ctx, &model.Media{ID: 1, SetID: 1, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: "/s/a.mp4", Type: model.MediaTypeVideo})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UpdateSet error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UpdateSet(ctx, &model.Set{ID: 1, Name: "s", RootPath: "/s"})
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "SoftDeleteMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.SoftDeleteMedia(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "RestoreMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.RestoreMedia(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "HardDeleteMedia error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.HardDeleteMedia(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "IncrementPlayCount error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.IncrementPlayCount(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteUser error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteUser(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteSet error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteSet(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteTag error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteTag(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteSession error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteSession(ctx, "abc")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteExpiredSessions error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteExpiredSessions(ctx, time.Now())
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteShare error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteShare(ctx, "abc")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteExpiredShares error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteExpiredShares(ctx, time.Now())
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "DeleteNote error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.DeleteNote(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "RevokePermission error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.RevokePermission(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "AssignTag error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.AssignTag(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "RemoveTag error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.RemoveTag(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "UseShare error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				err := s.UseShare(ctx, "abc")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetUserByID error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetUserByID(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetUserByUsername error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetUserByUsername(ctx, "u")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetSetByID error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetSetByID(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetMediaByID error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetMediaByID(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetTagByID error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetTagByID(ctx, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetTagByName error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetTagByName(ctx, "rock")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetPermission error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetPermission(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetNote error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetNote(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetProgress error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetProgress(ctx, 1, 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetAccumulator error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetAccumulator(ctx, "s", 1)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetSessionByID error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetSessionByID(ctx, "abc")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "GetShareByToken error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.GetShareByToken(ctx, "abc")
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
		{
			name: "CountUsers error on closed db",
			run: func(t *testing.T, ctx context.Context, s *SQLite) {
				s.Close()
				_, err := s.CountUsers(ctx)
				if err == nil {
					t.Fatal("expected error")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			tt.run(t, context.Background(), s)
		})
	}
}
