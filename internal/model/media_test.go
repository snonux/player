package model

import (
	"testing"
	"time"
)

func TestStructsInstantiate(t *testing.T) {
	now := time.Now()
	_ = User{ID: 1, Username: "u", PasswordHash: "h", IsAdmin: true, CreatedAt: now}
	_ = Set{ID: 1, Name: "s", RootPath: "/r", CreatedAt: now}
	_ = Media{ID: 1, SetID: 1, RelPath: "r", FileName: "f", AbsPath: "a", Type: MediaTypeVideo, Duration: 1, DeletedAt: &now, CreatedAt: now}
	_ = Tag{ID: 1, Name: "t"}
	_ = Session{ID: "s", UserID: 1, ExpiresAt: now, CreatedAt: now}
	max := 1
	_ = Share{Token: "t", MediaID: 1, CreatedBy: 1, CreatedAt: now, ExpiresAt: now, MaxUses: &max}
	_ = Note{ID: 1, MediaID: 1, UserID: 1, Content: "c", CreatedAt: now, UpdatedAt: now}
	_ = PlaybackProgress{UserID: 1, MediaID: 1, PositionSeconds: 1, UpdatedAt: now}
	_ = PlaybackAccumulator{SessionID: "s", MediaID: 1, UpdatedAt: now}
	_ = Favorite{UserID: 1, MediaID: 1, CreatedAt: now}
	_ = MediaTag{MediaID: 1, TagID: 1}
	_ = SetPermission{SetID: 1, UserID: 1, Role: RoleOwner, CreatedAt: now}
	_ = Metadata{Duration: 1, Codec: "c", Resolution: "r", Bitrate: 1, FileSizeBytes: 1}
	if MediaTypeVideo != "video" || MediaTypeAudio != "audio" || RoleOwner != "owner" || RoleViewer != "viewer" {
		t.Fatal("constants mismatch")
	}
}
