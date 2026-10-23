package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RadioRepo persists administrator-managed live stations.
type RadioRepo interface {
	ListRadioStations(context.Context) ([]model.RadioStation, error)
	GetRadioStation(context.Context, int64) (*model.RadioStation, error)
	SaveRadioStation(context.Context, *model.RadioStation) error
	DeleteRadioStation(context.Context, int64) (bool, error)
}

// ListRadioStations returns stations in readable, stable name order.
func (s *SQLite) ListRadioStations(ctx context.Context) ([]model.RadioStation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,stream_url,homepage_url,artwork_url,local_network FROM radio_stations ORDER BY name COLLATE NOCASE,id`)
	if err != nil {
		return nil, fmt.Errorf("list radio: %w", err)
	}
	defer rows.Close()
	result := []model.RadioStation{}
	for rows.Next() {
		var item model.RadioStation
		if err := rows.Scan(&item.ID, &item.Name, &item.StreamURL, &item.HomepageURL, &item.ArtworkURL, &item.LocalNetwork); err != nil {
			return nil, fmt.Errorf("read radio: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// GetRadioStation returns nil when the station has been removed.
func (s *SQLite) GetRadioStation(ctx context.Context, id int64) (*model.RadioStation, error) {
	var item model.RadioStation
	err := s.db.QueryRowContext(ctx, `SELECT id,name,stream_url,homepage_url,artwork_url,local_network FROM radio_stations WHERE id=?`, id).Scan(&item.ID, &item.Name, &item.StreamURL, &item.HomepageURL, &item.ArtworkURL, &item.LocalNetwork)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get radio: %w", err)
	}
	return &item, nil
}

// SaveRadioStation creates a station or updates an existing station.
func (s *SQLite) SaveRadioStation(ctx context.Context, item *model.RadioStation) error {
	if item.ID == 0 {
		result, err := s.db.ExecContext(ctx, `INSERT INTO radio_stations(name,stream_url,homepage_url,artwork_url,local_network) VALUES(?,?,?,?,?)`, item.Name, item.StreamURL, item.HomepageURL, item.ArtworkURL, item.LocalNetwork)
		if err != nil {
			return fmt.Errorf("create radio: %w", err)
		}
		item.ID, err = result.LastInsertId()
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE radio_stations SET name=?,stream_url=?,homepage_url=?,artwork_url=?,local_network=? WHERE id=?`, item.Name, item.StreamURL, item.HomepageURL, item.ArtworkURL, item.LocalNetwork, item.ID)
	if err != nil {
		return fmt.Errorf("update radio: %w", err)
	}
	return nil
}

// DeleteRadioStation removes a shared station.
func (s *SQLite) DeleteRadioStation(ctx context.Context, id int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM radio_stations WHERE id=?`, id)
	if err != nil {
		return false, fmt.Errorf("delete radio: %w", err)
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
