package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
)

const radioBookmarkColumns = `station_uuid, name, stream_url, homepage, favicon_url, country, country_code,
	language, tags, codec, bitrate, votes, created_at`

// UpsertRadioBookmark saves a bookmark. Bookmarking a station again
// refreshes its data and keeps the first bookmark time.
func (s *SQLite) UpsertRadioBookmark(ctx context.Context, b *model.RadioBookmark) (*model.RadioBookmark, error) {
	st := b.Station
	_, err := s.db.ExecContext(ctx, `INSERT INTO radio_bookmarks (user_id, `+radioBookmarkColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, station_uuid) DO UPDATE SET
		name = excluded.name, stream_url = excluded.stream_url, homepage = excluded.homepage,
		favicon_url = excluded.favicon_url, country = excluded.country, country_code = excluded.country_code,
		language = excluded.language, tags = excluded.tags, codec = excluded.codec,
		bitrate = excluded.bitrate, votes = excluded.votes`,
		b.UserID, st.UUID, st.Name, st.StreamURL, st.Homepage, st.FaviconURL, st.Country, st.CountryCode,
		st.Language, strings.Join(st.Tags, ","), st.Codec, st.Bitrate, st.Votes, b.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert radio bookmark: %w", err)
	}
	saved, err := s.GetRadioBookmark(ctx, b.UserID, st.UUID)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, errors.New("upsert radio bookmark: row missing after write")
	}
	return saved, nil
}

// ListRadioBookmarks returns a user's bookmarks, newest first.
func (s *SQLite) ListRadioBookmarks(ctx context.Context, userID int64) ([]model.RadioBookmark, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+radioBookmarkColumns+`
		FROM radio_bookmarks WHERE user_id = ? ORDER BY created_at DESC, name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list radio bookmarks: %w", err)
	}
	defer rows.Close()
	bookmarks := []model.RadioBookmark{}
	for rows.Next() {
		b, err := scanRadioBookmark(rows)
		if err != nil {
			return nil, err
		}
		b.UserID = userID
		bookmarks = append(bookmarks, *b)
	}
	return bookmarks, rows.Err()
}

// GetRadioBookmark returns nil, nil when the user did not bookmark the station.
func (s *SQLite) GetRadioBookmark(ctx context.Context, userID int64, uuid string) (*model.RadioBookmark, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+radioBookmarkColumns+`
		FROM radio_bookmarks WHERE user_id = ? AND station_uuid = ?`, userID, uuid)
	b, err := scanRadioBookmark(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.UserID = userID
	return b, nil
}

// DeleteRadioBookmark removes a bookmark; removing a missing one is no error.
func (s *SQLite) DeleteRadioBookmark(ctx context.Context, userID int64, uuid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM radio_bookmarks WHERE user_id = ? AND station_uuid = ?`, userID, uuid); err != nil {
		return fmt.Errorf("delete radio bookmark: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRadioBookmark(row rowScanner) (*model.RadioBookmark, error) {
	var b model.RadioBookmark
	var tags string
	var created time.Time
	st := &b.Station
	err := row.Scan(&st.UUID, &st.Name, &st.StreamURL, &st.Homepage, &st.FaviconURL, &st.Country,
		&st.CountryCode, &st.Language, &tags, &st.Codec, &st.Bitrate, &st.Votes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("scan radio bookmark: %w", err)
	}
	st.Tags = model.SplitRadioTags(tags)
	b.CreatedAt = created.UTC()
	return &b, nil
}
