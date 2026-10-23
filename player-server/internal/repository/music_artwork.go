package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// MusicArtworkRepo persists normalized custom artwork independently of tags.
type MusicArtworkRepo interface {
	GetMusicArtwork(context.Context, string, string) (*model.MusicArtwork, error)
	SaveMusicArtwork(context.Context, string, string, *model.MusicArtwork) error
}

// GetMusicArtwork returns no image when the entity uses its ordinary cover.
func (s *SQLite) GetMusicArtwork(ctx context.Context, kind, key string) (*model.MusicArtwork, error) {
	var a model.MusicArtwork
	err := s.db.QueryRowContext(ctx, `SELECT data,revision FROM music_artwork WHERE kind=? AND entity_key=?`, kind, key).Scan(&a.Data, &a.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read music artwork: %w", err)
	}
	return &a, nil
}

// SaveMusicArtwork atomically replaces or removes an image.
func (s *SQLite) SaveMusicArtwork(ctx context.Context, kind, key string, a *model.MusicArtwork) error {
	var err error
	if a == nil {
		_, err = s.db.ExecContext(ctx, `DELETE FROM music_artwork WHERE kind=? AND entity_key=?`, kind, key)
	} else {
		_, err = s.db.ExecContext(ctx, `INSERT INTO music_artwork(kind,entity_key,data,revision) VALUES(?,?,?,?) ON CONFLICT(kind,entity_key) DO UPDATE SET data=excluded.data,revision=excluded.revision`, kind, key, a.Data, a.Revision)
	}
	if err != nil {
		return fmt.Errorf("save music artwork: %w", err)
	}
	return nil
}

// MockMusicArtworkRepo supplies optional artwork storage in test fixtures.
type MockMusicArtworkRepo struct {
	GetMusicArtworkFunc  func(context.Context, string, string) (*model.MusicArtwork, error)
	SaveMusicArtworkFunc func(context.Context, string, string, *model.MusicArtwork) error
}

// GetMusicArtwork delegates to the fixture if configured.
func (m MockMusicArtworkRepo) GetMusicArtwork(ctx context.Context, k, key string) (*model.MusicArtwork, error) {
	if m.GetMusicArtworkFunc != nil {
		return m.GetMusicArtworkFunc(ctx, k, key)
	}
	return nil, nil
}

// SaveMusicArtwork delegates to the fixture if configured.
func (m MockMusicArtworkRepo) SaveMusicArtwork(ctx context.Context, k, key string, a *model.MusicArtwork) error {
	if m.SaveMusicArtworkFunc != nil {
		return m.SaveMusicArtworkFunc(ctx, k, key, a)
	}
	return nil
}
