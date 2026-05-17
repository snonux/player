package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestProgressService_UpdateProgress_Validation(t *testing.T) {
	ctx := context.Background()
	svc := NewProgressService(&repository.MockStore{}, newMockClock())

	if err := svc.UpdateProgress(ctx, "", 1, 10, 5); err == nil {
		t.Fatal("expected error for empty sessionID")
	}
	if err := svc.UpdateProgress(ctx, "sess", 1, 0, 5); err == nil {
		t.Fatal("expected error for mediaID=0")
	}
}

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

func TestProgressService_MarkFinished(t *testing.T) {
	ctx := context.Background()
	var saved *model.PlaybackProgress

	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: id, SetID: 7, Duration: 123.5}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
			UpsertProgressFunc: func(ctx context.Context, progress *model.PlaybackProgress) error {
				saved = progress
				return nil
			},
		},
	}

	svc := NewProgressService(store, newMockClock())
	if err := svc.MarkFinished(ctx, 1, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved progress")
	}
	if !saved.Finished {
		t.Fatal("expected progress marked finished")
	}
	if saved.PositionSeconds != 123.5 {
		t.Fatalf("expected position_seconds=duration, got %v", saved.PositionSeconds)
	}
}

func TestProgressService_MarkFinished_Validation(t *testing.T) {
	ctx := context.Background()
	svc := NewProgressService(&repository.MockStore{}, newMockClock())

	if err := svc.MarkFinished(ctx, 1, 0); err == nil {
		t.Fatal("expected error for mediaID=0")
	}
}

func TestProgressService_MarkNotStarted(t *testing.T) {
	ctx := context.Background()
	var deletedProgress bool
	var deletedAccumulator bool

	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: id, SetID: 7}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
			DeleteProgressFunc: func(ctx context.Context, userID, mediaID int64) error {
				deletedProgress = userID == 1 && mediaID == 10
				return nil
			},
		},
		PlaybackAccumulatorRepo: repository.MockPlaybackAccumulatorRepo{
			DeleteAccumulatorByMediaFunc: func(ctx context.Context, mediaID int64) error {
				deletedAccumulator = mediaID == 10
				return nil
			},
		},
	}

	svc := NewProgressService(store, newMockClock())
	if err := svc.MarkNotStarted(ctx, 1, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deletedProgress {
		t.Fatal("expected DeleteProgress called")
	}
	if !deletedAccumulator {
		t.Fatal("expected DeleteAccumulatorByMedia called")
	}
}

func TestProgressService_MarkNotStarted_Validation(t *testing.T) {
	ctx := context.Background()
	svc := NewProgressService(&repository.MockStore{}, newMockClock())

	if err := svc.MarkNotStarted(ctx, 1, 0); err == nil {
		t.Fatal("expected error for mediaID=0")
	}
}

func TestProgressService_ListInProgress(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		user      *model.User
		perms     []model.SetPermission
		want      []model.Media
		wantAllow []int64
		wantCalls int
	}{
		{
			name:      "admin lists without allowed set filter",
			user:      &model.User{ID: 1, IsAdmin: true},
			want:      []model.Media{{ID: 10, SetID: 7}},
			wantAllow: nil,
			wantCalls: 1,
		},
		{
			name:      "viewer lists only permitted sets",
			user:      &model.User{ID: 2, IsAdmin: false},
			perms:     []model.SetPermission{{SetID: 7, UserID: 2}, {SetID: 8, UserID: 2}},
			want:      []model.Media{{ID: 10, SetID: 7}},
			wantAllow: []int64{7, 8},
			wantCalls: 1,
		},
		{
			name:      "viewer with no permissions does not query media",
			user:      &model.User{ID: 3, IsAdmin: false},
			want:      []model.Media{},
			wantAllow: nil,
			wantCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var gotAllowed []int64

			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return tt.user, nil
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					ListPermissionsByUserFunc: func(ctx context.Context, userID int64) ([]model.SetPermission, error) {
						return tt.perms, nil
					},
				},
				PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
					ListInProgressMediaFunc: func(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error) {
						calls++
						gotAllowed = filter.AllowedSetIDs
						return tt.want, nil
					},
				},
			}

			svc := NewProgressService(store, newMockClock())
			got, err := svc.ListInProgress(ctx, tt.user.ID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if calls != tt.wantCalls {
				t.Fatalf("expected %d ListInProgressMedia calls, got %d", tt.wantCalls, calls)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d media, got %d", len(tt.want), len(got))
			}
			if tt.wantCalls > 0 && !equalInt64Slices(gotAllowed, tt.wantAllow) {
				t.Fatalf("expected AllowedSetIDs=%v, got %v", tt.wantAllow, gotAllowed)
			}
		})
	}
}

func equalInt64Slices(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
