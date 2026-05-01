package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestProgressService_UpdateProgress(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		sessionID         string
		userID            int64
		mediaID           int64
		position          float64
		accLastPosition   float64
		accAccumulated    float64
		accCounted        bool
		accErr            error
		upsertProgressErr error
		upsertAccErr      error
		incrementErr      error
		wantErr           bool
		wantCounted       bool
	}{
		{
			name:            "fresh accumulator does not reach 60 due to clamp",
			sessionID:       "sess1",
			userID:          1,
			mediaID:         10,
			position:        65,
			accLastPosition: 0,
			accAccumulated:  0,
			accCounted:      false,
			wantCounted:     false,
		},
		{
			name:            "accumulator reaches 60",
			sessionID:       "sess1",
			userID:          1,
			mediaID:         10,
			position:        12,
			accLastPosition: 0,
			accAccumulated:  48,
			accCounted:      false,
			wantCounted:     true,
		},
		{
			name:            "delta clamped to 12",
			sessionID:       "sess1",
			userID:          1,
			mediaID:         10,
			position:        20,
			accLastPosition: 0,
			accAccumulated:  0,
			accCounted:      false,
			wantCounted:     false,
		},
		{
			name:            "negative delta clamped",
			sessionID:       "sess1",
			userID:          1,
			mediaID:         10,
			position:        5,
			accLastPosition: 10,
			accAccumulated:  50,
			accCounted:      false,
			wantCounted:     false,
		},
		{
			name:            "already counted",
			sessionID:       "sess1",
			userID:          1,
			mediaID:         10,
			position:        10,
			accLastPosition: 0,
			accAccumulated:  65,
			accCounted:      true,
			wantCounted:     true,
		},
		{
			name:              "upsert progress error",
			sessionID:         "sess1",
			userID:            1,
			mediaID:           10,
			position:          12,
			accAccumulated:    48,
			upsertProgressErr: errors.New("boom"),
			wantErr:           true,
		},
		{
			name:           "get accumulator error",
			sessionID:      "sess1",
			userID:         1,
			mediaID:        10,
			position:       12,
			accAccumulated: 48,
			accErr:         errors.New("boom"),
			wantErr:        true,
		},
		{
			name:           "upsert accumulator error",
			sessionID:      "sess1",
			userID:         1,
			mediaID:        10,
			position:       12,
			accAccumulated: 48,
			upsertAccErr:   errors.New("boom"),
			wantErr:        true,
		},
		{
			name:           "increment error",
			sessionID:      "sess1",
			userID:         1,
			mediaID:        10,
			position:       12,
			accAccumulated: 48,
			incrementErr:   errors.New("boom"),
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var savedAcc *model.PlaybackAccumulator
			var incremented int64

			store := &repository.MockStore{
				PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
					UpsertProgressFunc: func(ctx context.Context, progress *model.PlaybackProgress) error {
						return tt.upsertProgressErr
					},
				},
				PlaybackAccumulatorRepo: repository.MockPlaybackAccumulatorRepo{
					GetAccumulatorFunc: func(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
						if tt.accErr != nil {
							return nil, tt.accErr
						}
						return &model.PlaybackAccumulator{
							SessionID:          sessionID,
							MediaID:            mediaID,
							LastPosition:       tt.accLastPosition,
							AccumulatedSeconds: tt.accAccumulated,
							Counted:            tt.accCounted,
						}, nil
					},
					UpsertAccumulatorFunc: func(ctx context.Context, acc *model.PlaybackAccumulator) error {
						savedAcc = acc
						return tt.upsertAccErr
					},
				},
				MediaRepo: repository.MockMediaRepo{
					IncrementPlayCountFunc: func(ctx context.Context, id int64) error {
						incremented = id
						return tt.incrementErr
					},
				},
			}

			svc := NewProgressService(store, newMockClock())
			err := svc.UpdateProgress(ctx, tt.sessionID, tt.userID, tt.mediaID, tt.position)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if savedAcc == nil {
				t.Fatal("expected accumulator saved")
			}
			if savedAcc.Counted != tt.wantCounted {
				t.Fatalf("expected Counted=%v, got %v", tt.wantCounted, savedAcc.Counted)
			}
			if tt.wantCounted && !tt.accCounted && incremented != tt.mediaID {
				t.Fatalf("expected IncrementPlayCount called with %d", tt.mediaID)
			}
		})
	}
}
