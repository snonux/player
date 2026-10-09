// Package ffsafetest holds what the real-ffmpeg regression tests of the
// packages calling ffmpeg/ffprobe share: the skip-or-fail rule and the
// fixtures of the "playlist disguised as media" attack. It is imported by
// tests only.
package ffsafetest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// RequireEnv names the environment variable that turns every "ffmpeg is
// missing" skip into a failure, so CI cannot pass by skipping.
const RequireEnv = "PLAYER_REQUIRE_FFMPEG"

// SecretWidth and SecretHeight are the dimensions of the video Secret
// creates. No other fixture has them, so metadata showing them proves that
// ffmpeg read the secret file.
const (
	SecretWidth  = 96
	SecretHeight = 54
)

// secretRef is how the playlists name the secret file: relative to their
// own directory, which ffmpeg's concat demuxer accepts in its default safe
// mode. An attacker needs nothing more than a guessable name.
const secretRef = "private/s.mp4"

// AssertHardened fails unless the argument list args opens path through
// input (what ffsafe returned for it) and names it bare nowhere. The
// argument-list tests of every ffmpeg/ffprobe call site use it; they need
// no ffmpeg.
func AssertHardened(t testing.TB, args, input []string, path string) {
	t.Helper()
	found := false
	for i := 0; i+len(input) <= len(args); i++ {
		if slices.Equal(args[i:i+len(input)], input) {
			found = true
		}
	}
	if !found {
		t.Errorf("arguments %q do not contain the ffsafe input %q", args, input)
	}
	if slices.Contains(args, path) {
		t.Errorf("arguments %q name the bare path %q", args, path)
	}
}

// SkipOrFail skips the test, or fails it when RequireEnv is "1".
func SkipOrFail(t testing.TB, reason string) {
	t.Helper()
	if os.Getenv(RequireEnv) == "1" {
		t.Fatalf("%s=1 but %s", RequireEnv, reason)
	}
	t.Skip(reason)
}

// RequireFFmpeg skips (or fails, see SkipOrFail) the test unless ffmpeg and
// ffprobe are installed.
func RequireFFmpeg(t testing.TB) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			SkipOrFail(t, bin+" not installed")
		}
	}
}

// Generate creates the file at path with ffmpeg; args are everything
// between the global options and the output, typically a lavfi source. A
// build that cannot create the file skips the test (or fails it in CI).
func Generate(t testing.TB, path string, args ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	full := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)
	// The file: prefix makes ffmpeg take names with ":" or a leading "-"
	// literally.
	full = append(full, "file:"+path)
	if out, err := exec.Command("ffmpeg", full...).CombinedOutput(); err != nil {
		SkipOrFail(t, "cannot generate "+filepath.Base(path)+": "+err.Error()+": "+string(out))
	}
}

// Video creates a short silent test video of the given size at path.
func Video(t testing.TB, path, size string) {
	t.Helper()
	Generate(t, path, "-f", "lavfi", "-i", "testsrc=d=3:s="+size+":r=5", "-pix_fmt", "yuv420p")
}

// Still creates a single-frame image at path; the extension picks the
// format. -update 1 keeps the image muxer from expanding a "%d" pattern in
// the name.
func Still(t testing.TB, path string) {
	t.Helper()
	Generate(t, path, "-f", "lavfi", "-i", "testsrc=s=64x48", "-frames:v", "1", "-update", "1")
}

// Secret creates the video the playlists point at, dir/private/s.mp4, and
// returns its path. It stands for media of a set the uploader cannot see.
func Secret(t testing.TB, dir string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(secretRef))
	Video(t, path, fmt.Sprintf("%dx%d", SecretWidth, SecretHeight))
	return path
}

// Playlists returns the content of the playlist formats ffmpeg follows to
// other files, by name. Each references the file Secret creates.
func Playlists() map[string]string {
	return map[string]string{
		"ffconcat": "ffconcat version 1.0\nfile '" + secretRef + "'\n",
		"hls": "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3\n" +
			"#EXTINF:3.0,\n" + secretRef + "\n#EXT-X-ENDLIST\n",
	}
}

// DisguisedExts are the media extensions the playlists are hidden under.
func DisguisedExts() []string {
	return []string{".avi", ".mp4", ".mkv", ".jpg", ".png"}
}

// WriteDisguised writes every playlist under every disguise extension into
// dir ("ffconcat-evil.avi", "hls-evil.jpg", ...) and returns the paths.
// Call Secret(t, dir) as well, or the playlists point at nothing.
func WriteDisguised(t testing.TB, dir string) []string {
	t.Helper()
	var paths []string
	for kind, content := range Playlists() {
		for _, ext := range DisguisedExts() {
			path := filepath.Join(dir, kind+"-evil"+ext)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, path)
		}
	}
	return paths
}
