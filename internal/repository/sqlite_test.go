package repository

import (
	"context"
	"testing"
	"time"

	"github.com/paul/kiss-media-player/internal/model"
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
					{"_", []int64{m1}},        // literal underscore must match only ab_c.mp4
					{"%", []int64{m2}},        // literal percent must match only de%f.mp4
					{"\\", []int64{m3}},       // literal backslash must match only gh\ij.mp4
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
				pp, _ := s.ListProgressByUser(ctx, uid)
				if len(pp) != 1 {
					t.Fatalf("expected 1, got %d", len(pp))
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
