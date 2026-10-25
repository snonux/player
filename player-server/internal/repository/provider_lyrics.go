package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProviderLyricsRepo stores optional account-owned lyric snapshots.
type ProviderLyricsRepo interface {
	HasProviderLyrics(context.Context, int64, int64) (bool, error)
	LoadProviderLyrics(context.Context, int64, int64, string) ([]byte, error)
	SaveProviderLyrics(context.Context, int64, int64, string, []byte) error
	DeleteProviderLyrics(context.Context, int64, int64) error
}

// HasProviderLyrics avoids hashing audio for ordinary unfetched GETs.
func (s *SQLite) HasProviderLyrics(ctx context.Context, user, media int64) (bool, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM provider_lyrics WHERE fetched_at<=?`, s.now().Add(-30*24*time.Hour)); err != nil {
		return false, err
	}
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provider_lyrics WHERE user_id=? AND media_id=? AND fetched_at>?)`, user, media, s.now().Add(-30*24*time.Hour)).Scan(&found)
	return found, err
}

// LoadProviderLyrics returns only the currently matching source/provider copy.
func (s *SQLite) LoadProviderLyrics(ctx context.Context, user, media int64, identity string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM provider_lyrics WHERE user_id=? AND media_id=? AND identity=? AND fetched_at>?`, user, media, identity, s.now().Add(-30*24*time.Hour)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// SaveProviderLyrics replaces this account's previous copy for the media.
func (s *SQLite) SaveProviderLyrics(ctx context.Context, user, media int64, identity string, data []byte) error {
	if len(data) > 1024*1024 {
		return errors.New("lyrics too large")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_lyrics(user_id,media_id,identity,payload,fetched_at) VALUES(?,?,?,?,?) ON CONFLICT(user_id,media_id) DO UPDATE SET identity=excluded.identity,payload=excluded.payload,fetched_at=excluded.fetched_at`, user, media, identity, string(data), s.now())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM provider_lyrics WHERE fetched_at<=? OR (user_id=? AND media_id NOT IN (SELECT media_id FROM provider_lyrics WHERE user_id=? ORDER BY fetched_at DESC,media_id DESC LIMIT 128))`, s.now().Add(-30*24*time.Hour), user, user)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteProviderLyrics removes only this account's cached provider lyrics.
func (s *SQLite) DeleteProviderLyrics(ctx context.Context, user, media int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM provider_lyrics WHERE user_id=? AND media_id=?`, user, media)
	return err
}
