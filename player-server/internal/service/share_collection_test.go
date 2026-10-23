package service

import (
	"github.com/snonux/player/internal/model"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestShareCollectionSnapshotViewingAndLifecycle(t *testing.T) {
	f := newViewingFixture(t, ":memory:")
	ctx := context.Background()
	original, err := f.store.GetMediaByID(ctx, f.media)
	f.must(err)
	f.must(f.store.GrantPermission(ctx, &model.SetPermission{SetID: original.SetID, UserID: f.user, Role: model.RoleViewer, CreatedAt: f.clk.T}))
	song := func(name string) int64 {
		t.Helper()
		id, err := f.store.CreateMedia(ctx, &model.Media{SetID: original.SetID, FileName: name, RelPath: name, AbsPath: "/private/" + name, Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: name, Comment: "PRIVATE"}, CreatedAt: f.clk.T})
		f.must(err)
		return id
	}
	a, b := song("A.mp3"), song("B.mp3")
	ids := []int64{a, b, a}
	max := 1
	sh, err := f.svc.CreateCollectionShare(ctx, f.user, "Album", ids, f.clk.T.Add(time.Hour), &max)
	f.must(err)
	ids[0] = b
	stored, err := f.store.GetShareByToken(ctx, sh.Token)
	f.must(err)
	if stored.MediaID != 0 || stored.MediaIDs[0] != a {
		t.Fatalf("not independent snapshot: %+v", stored)
	}
	page, err := f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token, Probe: true})
	f.must(err)
	if f.used(sh.Token) != 0 || len(page.Items) != 3 {
		t.Fatal("probe spent use or lost repeats")
	}
	page, err = f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token})
	f.must(err)
	public := page.WithViewCredential()
	body, err := json.Marshal(public)
	f.must(err)
	if strings.Contains(string(body), "/private/") || strings.Contains(string(body), "PRIVATE") || public.Items[2].Media.ID != 3 || !strings.Contains(public.Items[1].PlaybackURL, "?view=") {
		t.Fatalf("unsafe public page: %s", body)
	}
	access := ShareAccess{Token: sh.Token, Item: 1, Credentials: []string{public.View}}
	file, _, err := f.svc.StreamSharedMedia(ctx, access)
	f.must(err)
	if file.FileName != "B.mp3" || f.used(sh.Token) != 1 {
		t.Fatal("wrong occurrence/use count")
	}
	access.Item = 3
	if _, _, err := f.svc.StreamSharedMedia(ctx, access); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("out of scope: %v", err)
	}
	f.must(f.store.HardDeleteMedia(ctx, a))
	page, err = f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token, Credentials: []string{public.View}})
	f.must(err)
	if !page.Items[0].Unavailable || page.Items[1].Unavailable || page.Items[0].PlaybackURL != "" {
		t.Fatal("deleted first source destroyed link or leaked")
	}
	f.must(f.store.RevokePermission(ctx, original.SetID, f.user))
	access.Item = 1
	if _, _, err := f.svc.StreamSharedMedia(ctx, access); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("creator ACL bypass: %v", err)
	}
	mine, err := f.svc.ListMyShares(ctx, f.user)
	f.must(err)
	if len(mine) != 1 || mine[0].FileName != "Album" {
		t.Fatalf("management missing: %+v", mine)
	}
	f.must(f.svc.RevokeShare(ctx, sh.Token, f.user))
	if _, err := f.svc.GetSharedMedia(ctx, ShareAccess{Token: sh.Token, Credentials: []string{public.View}}); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("revoked viewing survived: %v", err)
	}
}
