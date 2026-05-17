package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type fixedTokenManager struct {
	plaintext string
	hash      string
}

func (m fixedTokenManager) Generate() (string, string) {
	return m.plaintext, m.hash
}

func (m fixedTokenManager) Hash(plaintext string) string {
	if plaintext == m.plaintext {
		return m.hash
	}
	return "unknown"
}

func TestAuthService_APITokenCRUD(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	clk := &clock.MockClock{T: now}

	var created *model.APIToken
	var deletedID int64
	store := &repository.MockStore{
		APITokenRepo: repository.MockAPITokenRepo{
			CreateFunc: func(ctx context.Context, token *model.APIToken) (int64, error) {
				created = token
				return 7, nil
			},
			ListByUserFunc: func(ctx context.Context, userID int64) ([]model.APIToken, error) {
				if userID != 42 {
					return nil, nil
				}
				return []model.APIToken{{ID: 7, UserID: userID, Name: "automation"}}, nil
			},
			DeleteByIDFunc: func(ctx context.Context, id int64) error {
				deletedID = id
				return nil
			},
		},
	}
	svc := NewAuthService(store, clk, nil, nil, fixedTokenManager{plaintext: "plain", hash: "hashed"})

	result, err := svc.CreateAPIToken(ctx, 42, "automation", &expiresAt)
	if err != nil {
		t.Fatalf("create api token: %v", err)
	}
	if result.Plaintext != "plain" || result.Token.ID != 7 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if created == nil || created.UserID != 42 || created.TokenHash != "hashed" || created.CreatedAt != now {
		t.Fatalf("unexpected created token: %#v", created)
	}

	tokens, err := svc.ListAPITokens(ctx, 42)
	if err != nil {
		t.Fatalf("list api tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != 7 {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}

	if err := svc.RevokeAPIToken(ctx, 42, 7); err != nil {
		t.Fatalf("revoke api token: %v", err)
	}
	if deletedID != 7 {
		t.Fatalf("expected delete id 7, got %d", deletedID)
	}
	if err := svc.RevokeAPIToken(ctx, 42, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAuthService_AuthenticateBearer(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	expiresAt := now.Add(time.Hour)
	expiredAt := now.Add(-time.Second)

	tests := []struct {
		name      string
		token     *model.APIToken
		plaintext string
		wantErr   error
		wantTouch bool
	}{
		{
			name:      "valid",
			token:     &model.APIToken{ID: 5, UserID: 42, CreatedAt: createdAt, ExpiresAt: &expiresAt},
			plaintext: "plain",
			wantTouch: true,
		},
		{
			name:      "revoked",
			plaintext: "plain",
			wantErr:   ErrInvalidCredentials,
		},
		{
			name:      "expired",
			token:     &model.APIToken{ID: 5, UserID: 42, CreatedAt: createdAt, ExpiresAt: &expiredAt},
			plaintext: "plain",
			wantErr:   ErrInvalidCredentials,
		},
		{
			name:    "empty",
			wantErr: ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			touched := false
			store := &repository.MockStore{
				APITokenRepo: repository.MockAPITokenRepo{
					GetByHashFunc: func(ctx context.Context, tokenHash string) (*model.APIToken, error) {
						if tokenHash != "hashed" {
							t.Fatalf("unexpected hash: %q", tokenHash)
						}
						return tt.token, nil
					},
					TouchLastUsedFunc: func(ctx context.Context, id int64, lastUsedAt time.Time) error {
						touched = true
						if id != 5 || lastUsedAt != now {
							t.Fatalf("unexpected touch: id=%d at=%v", id, lastUsedAt)
						}
						return nil
					},
				},
			}
			svc := NewAuthService(store, &clock.MockClock{T: now}, nil, nil, fixedTokenManager{plaintext: "plain", hash: "hashed"})

			sess, err := svc.AuthenticateBearer(ctx, tt.plaintext)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if touched != tt.wantTouch {
				t.Fatalf("expected touched=%v, got %v", tt.wantTouch, touched)
			}
			if tt.wantErr != nil {
				return
			}
			if sess == nil || sess.ID != "api-token:5" || sess.UserID != 42 || sess.ExpiresAt != expiresAt {
				t.Fatalf("unexpected session: %#v", sess)
			}
		})
	}
}
