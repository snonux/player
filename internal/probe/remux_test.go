package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeMPEGTS(t *testing.T) {
	ts := make([]byte, 188*5)
	for i := 0; i < len(ts); i += 188 {
		ts[i] = 0x47
	}
	tsPath := filepath.Join(t.TempDir(), "mislabelled.mp4")
	if err := os.WriteFile(tsPath, ts, 0o644); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeMPEGTS(tsPath) {
		t.Fatal("expected MPEG-TS sync bytes to be detected")
	}

	mp4Path := filepath.Join(t.TempDir(), "real.mp4")
	if err := os.WriteFile(mp4Path, []byte("\x00\x00\x00\x18ftypmp42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LooksLikeMPEGTS(mp4Path) {
		t.Fatal("did not expect MP4 header to be detected as MPEG-TS")
	}
}
