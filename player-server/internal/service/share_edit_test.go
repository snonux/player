package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestShareEditPreservesViewingAndToken(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	ctx := context.Background()
	f.share("edit", time.Hour, 1)
	sh, err := f.store.GetShareByToken(ctx, "edit")
	f.must(err)
	page, err := f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token})
	f.must(err)
	description := "A description <script>opaque</script> 🎵"
	expiry := sh.ExpiresAt.Add(time.Hour)
	got, err := f.svc.UpdateShare(ctx, sh.Token, f.user, &description, &expiry)
	f.must(err)
	if got.Token != sh.Token || got.MediaID != sh.MediaID || got.UsedCount != 1 || got.MaxUses == nil || *got.MaxUses != 1 || got.Description != description || !got.ExpiresAt.Equal(expiry) {
		t.Fatalf("policy changed: %+v", got)
	}
	public, err := f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token, Credentials: []string{page.Viewing.Credential}})
	f.must(err)
	if public.Description != description || f.used(sh.Token) != 1 {
		t.Fatal("description or viewing lost")
	}
	if _, err := f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token}); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("edit reset spent uses: %v", err)
	}
	short := f.clk.T.Add(time.Second)
	_, err = f.svc.UpdateShare(ctx, sh.Token, f.user, nil, &short)
	f.must(err)
	f.clk.T = short
	if _, _, err := f.svc.StreamSharedMedia(ctx, ShareAccess{Token: sh.Token, Credentials: []string{page.Viewing.Credential}}); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("old viewing bypassed new expiry: %v", err)
	}
	// An expired description can be edited without silently extending its lifetime.
	got, err = f.svc.UpdateShare(ctx, sh.Token, f.user, &description, nil)
	f.must(err)
	if !got.ExpiresAt.Equal(short) {
		t.Fatal("description changed exact expiry")
	}
}

func TestShareEditPermissionsValidationAndCollection(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	ctx := context.Background()
	f.share("edit", time.Hour, 2)
	sh, err := f.store.GetShareByToken(ctx, "edit")
	f.must(err)
	description := "Changed"
	other, err := f.store.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "h", CreatedAt: f.clk.T})
	f.must(err)
	if _, err := f.svc.UpdateShare(ctx, sh.Token, other, &description, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign write: %v", err)
	}
	admin, err := f.store.CreateUser(ctx, &model.User{Username: "admin-edit", PasswordHash: "h", IsAdmin: true, CreatedAt: f.clk.T})
	f.must(err)
	_, err = f.svc.UpdateShare(ctx, sh.Token, admin, &description, nil)
	f.must(err)
	tooLong := strings.Repeat("🎵", 1001)
	past := f.clk.T.Add(-time.Second)
	for _, test := range []struct {
		description *string
		expiry      *time.Time
	}{{nil, nil}, {&tooLong, nil}, {nil, &past}} {
		if _, err := f.svc.UpdateShare(ctx, sh.Token, f.user, test.description, test.expiry); !errors.Is(err, ErrInvalidShareEdit) {
			t.Fatalf("invalid edit: %v", err)
		}
	}
	f.must(f.store.CreateShare(ctx, &model.Share{Token: "collection-edit", CreatedBy: f.user, CreatedAt: f.clk.T, ExpiresAt: sh.ExpiresAt, CollectionTitle: "Original", MediaIDs: []int64{f.media, f.media}}))
	got, err := f.svc.UpdateShare(ctx, "collection-edit", f.user, &description, nil)
	f.must(err)
	if got.CollectionTitle != "Original" || len(got.MediaIDs) != 2 || got.MediaIDs[0] != f.media {
		t.Fatal("snapshot changed")
	}
	f.must(f.svc.RevokeShare(ctx, sh.Token, f.user))
	if _, err := f.svc.UpdateShare(ctx, sh.Token, f.user, &description, nil); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("recreated deleted link: %v", err)
	}
}
