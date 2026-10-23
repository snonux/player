package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/snonux/player/internal/model"
)

// UpdateMediaMusic patches embedded tags and source size without overwriting saved state.
func (s *SQLite) UpdateMediaMusic(ctx context.Context, id int64, music *model.MusicMetadata, fileSizeBytes int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET music = ?, file_size_bytes = ? WHERE id = ?`, musicJSON(music), fileSizeBytes, id)
	if err != nil {
		return fmt.Errorf("update music tags: %w", err)
	}
	return nil
}

func musicJSON(music *model.MusicMetadata) any {
	if music == nil {
		return nil
	}
	// MusicMetadata contains only JSON-supported scalar and slice values.
	data, _ := json.Marshal(music)
	return string(data)
}
