package thumb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
)

// TestThumbArgs pins the ffmpeg argument lists of the thumbnailer. It needs
// no ffmpeg, so it fails wherever the tests run if the input or output
// hardening is dropped.
func TestThumbArgs(t *testing.T) {
	image := filepath.Join(t.TempDir(), "a%03d.png")
	if err := os.WriteFile(image, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seek := 12.3456
	tests := []struct {
		name string
		src  string
		seek *float64
		head []string
	}{
		{"video with seek", "/media/set/-a:b.avi", &seek, []string{"-nostdin", "-ss", "12.346"}},
		{"video first frame", "/media/set/-a:b.avi", nil, []string{"-nostdin"}},
		{"image", image, nil, []string{"-nostdin"}},
	}
	const out = "/media/set/.thumbnails/-tmp:1%03d.jpg"
	tail := []string{"-vf", "scale=320:-1", "-frames:v", "1", "-q:v", "2", "-update", "1", "-y", "file:" + out}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := ffsafe.SourceArgs(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := thumbArgs(tt.src, out, tt.seek)
			if err != nil {
				t.Fatalf("thumbArgs: %v", err)
			}
			if want := slices.Concat(tt.head, input, tail); !slices.Equal(got, want) {
				t.Errorf("thumbArgs = %q, want %q", got, want)
			}
			ffsafetest.AssertHardened(t, got, input, tt.src)
			if slices.Contains(got, out) {
				t.Errorf("arguments %q name the bare output path", got)
			}
		})
	}
}

// A source refused by ffsafe never reaches ffmpeg.
func TestFFmpegGenerator_RefusedSourceRunsNothing(t *testing.T) {
	src := filepath.Join(t.TempDir(), "evil.jpg")
	if err := os.WriteFile(src, []byte(ffsafetest.Playlists()["ffconcat"]), 0o644); err != nil {
		t.Fatal(err)
	}
	var runs []float64
	g := &FFmpegGenerator{execer: recordingExecer(&runs, "true"), stat: wroteSomething}
	err := g.Generate(context.Background(), src, filepath.Join(t.TempDir(), "out.jpg"), 0)
	if !errors.Is(err, ffsafe.ErrNotAnImage) {
		t.Errorf("Generate = %v, want ErrNotAnImage", err)
	}
	if len(runs) != 0 {
		t.Errorf("ffmpeg was started %d times for a refused source", len(runs))
	}
}

// A playlist disguised as media gets no thumbnail: neither a frame of the
// file it points at, nor any file at all. Durations above the seek margin
// exercise the seeked run and the first-frame fallback.
func TestFFmpeg_RealRefusesDisguisedPlaylists(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	secret := ffsafetest.Secret(t, dir)
	maker := NewFSMaker(NewFFmpegGenerator(), nil, nil)
	if got, err := maker.Make(context.Background(), secret, 3); err != nil || got == "" {
		t.Fatalf("thumbnail of the secret file itself: %q, %v", got, err)
	}
	for _, path := range ffsafetest.WriteDisguised(t, dir) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			for _, duration := range []float64{0, 3} {
				got, err := maker.Make(context.Background(), path, duration)
				if err == nil || got != "" {
					t.Errorf("Make(duration %v) = %q, %v; want a failure", duration, got, err)
				}
			}
			if _, err := os.Stat(ThumbnailPathFor(path)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a thumbnail exists for the disguised playlist (stat: %v)", err)
			}
		})
	}
}

// Sources and outputs whose names start with "-" or contain ":" or "%" get
// thumbnails. There is no a000.png here, so "a%03d.png" read as an image
// sequence pattern would find nothing and fail; that it is the literal file
// which is read is shown with a decoy sibling in the ffsafe and probe tests.
func TestFFmpeg_RealHostileNames(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := filepath.Join(t.TempDir(), "-d:%03d")
	maker := NewFSMaker(NewFFmpegGenerator(), nil, nil)
	for _, name := range []string{"-dash.mp4", "co:lon.mp4", "-dash.png", "co:lon.jpg", "a%03d.png", "a%03d.jpg"} {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(dir, name)
			duration := 0.0
			if filepath.Ext(name) == ".mp4" {
				ffsafetest.Video(t, src, "64x48")
				duration = 3
			} else {
				ffsafetest.Still(t, src)
			}
			got, err := maker.Make(context.Background(), src, duration)
			if err != nil {
				t.Fatalf("Make: %v", err)
			}
			if want := filepath.Join(dir, DirName, name+".jpg"); got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			if info, err := os.Stat(got); err != nil || info.Size() == 0 {
				t.Errorf("thumbnail missing or empty: %v", err)
			}
		})
	}
}
