package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

func TestScan_MusicRefreshPreservesIdentityAndState(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "Music", "unrelated-name.flac")
	write := func(title string) {
		t.Helper()
		args := []string{"-f", "lavfi", "-i", "sine=duration=1", "-c:a", "flac"}
		if title != "" {
			args = append(args, "-metadata", "title="+title, "-metadata", "artist=Ünïcødé", "-metadata", "discsubtitle=Disc "+title, "-metadata", "compilation=1", "-metadata", "disc=2/3", "-metadata", "composer=Composer; 東京 "+title, "-metadata", "performer=Performer / A", "-metadata", "release_date=2025-04", "-metadata", "originaldate=1979", "-metadata", "recordingdate=1978-11", "-metadata", "label=Label "+title, "-metadata", "credit:Tape Op=A / B", "-metadata", "bpm=123.5", "-metadata", "replaygain_track_gain=0 dB", "-metadata", "replaygain_album_gain=-6.25 dB", "-metadata", "replaygain_track_peak=0.89")
		}
		ffsafetest.Generate(t, path, args...)
	}
	write("First Title")
	initialFile, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(filepath.Join(t.TempDir(), "music.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now()
	sid, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	// Upgrade: an old row already has progress/counts but no music object.
	id, err := store.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: filepath.Base(path), FileName: filepath.Base(path), AbsPath: path, Type: model.MediaTypeAudio, FileSizeBytes: initialFile.Size(), PlayCount: 7, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	uid, err := store.CreateUser(ctx, &model.User{Username: "listener", PasswordHash: "hash", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: uid, MediaID: id, PositionSeconds: 0.3, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	scanner := NewFSScanner(store, probe.NewFFProber(), &thumb.MockGenerator{}, clock.RealClock{}, root)
	for _, title := range []string{"First Title", "Updated 東京", strings.Repeat("Long metadata 東京 ", 1000) + "End", ""} {
		write(title)
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ListMedia(ctx, repository.MediaFilter{})
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows=%v %v", rows, err)
		}
		row := rows[0]
		sourceFile, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if row.FileSizeBytes != sourceFile.Size() {
			t.Fatalf("indexed source size=%d, actual=%d after retagging title length=%d", row.FileSizeBytes, sourceFile.Size(), len(title))
		}
		if row.ID != id || row.FileName != filepath.Base(path) || row.PlayCount != 7 || row.Music == nil || row.Music.Title != title {
			t.Fatalf("row=%+v music=%+v", row, row.Music)
		}
		progress, err := store.GetProgress(ctx, uid, id)
		if err != nil || progress == nil || progress.PositionSeconds != 0.3 {
			t.Fatalf("progress=%+v %v", progress, err)
		}
		if row.Music.SampleRate != 44100 || row.Music.BitDepth != 16 || row.Music.Channels != 1 {
			t.Fatalf("source technical fields lost after rescan: %+v", row.Music)
		}
		if title == "" && (row.Music.BPM != nil || row.Music.ReplayGainTrackGainDB != nil || row.Music.ReplayGainAlbumGainDB != nil || row.Music.ReplayGainTrackPeak != nil) {
			t.Fatalf("removed audio tags retained: %+v", row.Music)
		}
		if title != "" && (row.Music.BPM == nil || *row.Music.BPM != 123.5 || row.Music.ReplayGainTrackGainDB == nil || *row.Music.ReplayGainTrackGainDB != 0 || row.Music.ReplayGainAlbumGainDB == nil || *row.Music.ReplayGainAlbumGainDB != -6.25 || row.Music.ReplayGainTrackPeak == nil || *row.Music.ReplayGainTrackPeak != 0.89) {
			t.Fatalf("audio tags lost after retag: %+v", row.Music)
		}
		if title == "" {
			if row.Music.DiscSubtitle != "" || row.Music.Compilation || len(row.Music.Composers) != 0 || len(row.Music.Performers) != 0 || row.Music.ReleaseDate != "" || row.Music.OriginalDate != "" || row.Music.RecordingDate != "" || len(row.Music.Labels) != 0 || len(row.Music.Credits) != 0 {
				t.Fatalf("removed credits retained %+v", row.Music)
			}
		} else {
			if row.Music.DiscSubtitle != "Disc "+title || !row.Music.Compilation || len(row.Music.Composers) != 1 || row.Music.Composers[0] != "Composer; 東京 "+title || len(row.Music.Performers) != 1 || row.Music.Performers[0] != "Performer / A" || row.Music.ReleaseDate != "2025-04" || row.Music.OriginalDate != "1979" || row.Music.RecordingDate != "1978-11" || len(row.Music.Labels) != 1 || row.Music.Labels[0] != "Label "+title || len(row.Music.Credits) != 1 {
				t.Fatalf("scan/rescan credits %+v", row.Music)
			}
		}
		if title != "" {
			matches, err := store.ListMedia(ctx, repository.MediaFilter{Search: title})
			if err != nil || len(matches) != 1 {
				t.Fatalf("title search rows=%v %v", matches, err)
			}
		}
		if title == "First Title" {
			if err := os.WriteFile(path, append([]byte("fLaC"), make([]byte, 100)...), 0600); err != nil {
				t.Fatal(err)
			}
			if err := scanner.Scan(ctx, root, nil); err != nil {
				t.Fatal(err)
			}
			retained, err := store.GetMediaByID(ctx, id)
			if err != nil || retained.Music.BPM == nil || *retained.Music.BPM != 123.5 || retained.Music.ReplayGainTrackGainDB == nil || *retained.Music.ReplayGainTrackGainDB != 0 || retained.Music.SampleRate != 44100 || retained.Music.BitDepth != 16 || retained.Music.DiscSubtitle != "Disc First Title" || !retained.Music.Compilation || retained.Music.ReleaseDate != "2025-04" || retained.Music.OriginalDate != "1979" || len(retained.Music.Credits) != 1 {
				t.Fatalf("failed probe discarded release snapshot: %+v %v", retained, err)
			}
		}
	}
}
