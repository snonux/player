package repository

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// MusicEnrichmentRepo caches optional provider descriptions by catalog identity.
type MusicEnrichmentRepo interface {
	GetMusicEnrichment(context.Context, string, string, string) (*model.MusicEnrichment, error)
	SaveMusicEnrichment(context.Context, string, string, string, model.MusicEnrichment) error
}

// GetMusicEnrichment returns nil when no lookup has been cached.
func (s *SQLite) GetMusicEnrichment(ctx context.Context, provider, kind, key string) (*model.MusicEnrichment, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM music_enrichment WHERE provider=? AND kind=? AND entity_key=?`, provider, kind, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read music enrichment: %w", err)
	}
	var result model.MusicEnrichment
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("decode music enrichment: %w", err)
	}
	return &result, nil
}

// SaveMusicEnrichment replaces only enrichment; local covers and tags are separate.
func (s *SQLite) SaveMusicEnrichment(ctx context.Context, provider, kind, key string, item model.MusicEnrichment) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode music enrichment: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO music_enrichment(provider,kind,entity_key,payload) VALUES(?,?,?,?) ON CONFLICT(provider,kind,entity_key) DO UPDATE SET payload=excluded.payload`, provider, kind, key, string(data))
	if err != nil {
		return fmt.Errorf("save music enrichment: %w", err)
	}
	return nil
}

// MockMusicEnrichmentRepo provides optional enrichment persistence in fixtures.
type MockMusicEnrichmentRepo struct {
	GetMusicEnrichmentFunc  func(context.Context, string, string, string) (*model.MusicEnrichment, error)
	SaveMusicEnrichmentFunc func(context.Context, string, string, string, model.MusicEnrichment) error
}

// GetMusicEnrichment calls the fixture or returns no cached lookup.
func (m MockMusicEnrichmentRepo) GetMusicEnrichment(ctx context.Context, p, k, key string) (*model.MusicEnrichment, error) {
	if m.GetMusicEnrichmentFunc != nil {
		return m.GetMusicEnrichmentFunc(ctx, p, k, key)
	}
	return nil, nil
}

// SaveMusicEnrichment calls the fixture or succeeds.
func (m MockMusicEnrichmentRepo) SaveMusicEnrichment(ctx context.Context, p, k, key string, item model.MusicEnrichment) error {
	if m.SaveMusicEnrichmentFunc != nil {
		return m.SaveMusicEnrichmentFunc(ctx, p, k, key, item)
	}
	return nil
}
