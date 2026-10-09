package ffsafe

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInputArgs(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // expected "file:" argument
	}{
		{"absolute", "/media/set/a.avi", "file:/media/set/a.avi"},
		{"cleaned", "/media/set/../other/a.avi", "file:/media/other/a.avi"},
		// Names that look like options, protocols or sequence patterns
		// are passed literally behind the file: prefix.
		{"hostile name", "/media/-weird:name %03d.avi", "file:/media/-weird:name %03d.avi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InputArgs(tt.path)
			if err != nil {
				t.Fatalf("InputArgs: %v", err)
			}
			want := []string{"-protocol_whitelist", "file", "-format_whitelist", FormatWhitelist, "-i", tt.want}
			if !slices.Equal(got, want) {
				t.Errorf("InputArgs = %q, want %q", got, want)
			}
		})
	}
}

func TestInputArgs_RelativePathBecomesAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := InputArgs("media/-a.avi")
	if err != nil {
		t.Fatalf("InputArgs: %v", err)
	}
	if want := "file:" + filepath.Join(wd, "media/-a.avi"); got[len(got)-1] != want {
		t.Errorf("input = %q, want %q", got[len(got)-1], want)
	}
}

// The whitelist must never contain a demuxer that reads other files.
func TestFormatWhitelist_HasNoReferencingDemuxers(t *testing.T) {
	for _, name := range strings.Split(FormatWhitelist, ",") {
		switch name {
		case "concat", "ffconcat", "hls", "applehttp", "dash", "image2", "lavfi", "tty", "sdp", "rtsp", "concatf":
			t.Errorf("FormatWhitelist contains %q, which can reference other files", name)
		}
	}
}
