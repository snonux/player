package probe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func generateEXIFImage(t *testing.T, dst string) {
	t.Helper()
	if _, err := exec.LookPath("convert"); err != nil {
		t.Skip("ImageMagick convert not available")
	}
	if _, err := exec.LookPath("exiv2"); err != nil {
		t.Skip("exiv2 not available")
	}

	cmd := exec.Command("convert", "-size", "2x2", "xc:red", dst)
	if err := cmd.Run(); err != nil {
		t.Fatalf("convert failed: %v", err)
	}

	exiv := exec.Command("exiv2",
		"-Mset Exif.Image.Make Canon",
		"-Mset Exif.Image.Model EOS 5D",
		"-Mset Exif.Photo.LensModel EF 50mm f/1.8",
		"-Mset Exif.Photo.DateTimeOriginal 2024:01:01 12:00:00",
		"-Mset Exif.Photo.ISOSpeedRatings 400",
		"-Mset Exif.Photo.FNumber 18/10",
		"-Mset Exif.Photo.ExposureTime 1/250",
		"-Mset Exif.Photo.FocalLength 50/1",
		dst,
	)
	if err := exiv.Run(); err != nil {
		t.Fatalf("exiv2 failed: %v", err)
	}
}

func generateVideo(t *testing.T, dst string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "color=c=red:size=2x2:d=1",
		"-pix_fmt", "yuv420p", "-c:v", "libx264", "-an", "-y", dst)
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg failed: %v", err)
	}
}

func TestIsImagePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"image.png", true},
		{"anim.gif", true},
		{"img.webp", true},
		{"legacy.bmp", true},
		{"modern.avif", true},
		{"vector.svg", true},
		{"video.mp4", false},
		{"audio.mp3", false},
		{"archive.tar.gz", false},
		{"no_ext", false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := isImagePath(c.path); got != c.want {
				t.Errorf("isImagePath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestIsImagePath_CaseInsensitive(t *testing.T) {
	if !isImagePath("UPPER.JPG") {
		t.Error("expected true for uppercase extension")
	}
	if !isImagePath("Mixed.PnG") {
		t.Error("expected true for mixed-case extension")
	}
}

func TestExtractEXIF(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.jpg")
	generateEXIFImage(t, imgPath)

	meta := &model.Metadata{}
	extractEXIF(imgPath, meta)

	if meta.EXIFCamera != "Canon EOS 5D" {
		t.Errorf("EXIFCamera = %q, want \"Canon EOS 5D\"", meta.EXIFCamera)
	}
	if meta.EXIFLens != "EF 50mm f/1.8" {
		t.Errorf("EXIFLens = %q, want \"EF 50mm f/1.8\"", meta.EXIFLens)
	}
	if meta.EXIFDate != "2024:01:01 12:00:00" {
		t.Errorf("EXIFDate = %q, want \"2024:01:01 12:00:00\"", meta.EXIFDate)
	}
	if meta.EXIFISO != "400" {
		t.Errorf("EXIFISO = %q, want \"400\"", meta.EXIFISO)
	}
	if meta.EXIFFNumber != "f/1.8" {
		t.Errorf("EXIFFNumber = %q, want \"f/1.8\"", meta.EXIFFNumber)
	}
	if meta.EXIFExposure != "1/250 s" {
		t.Errorf("EXIFExposure = %q, want \"1/250 s\"", meta.EXIFExposure)
	}
	if meta.EXIFFocalLength != "50.0 mm" {
		t.Errorf("EXIFFocalLength = %q, want \"50.0 mm\"", meta.EXIFFocalLength)
	}
}

func TestExtractEXIF_NonExistentFile(t *testing.T) {
	meta := &model.Metadata{}
	extractEXIF("/does/not/exist.jpg", meta)
	// Should not panic and leave fields empty.
	if meta.EXIFCamera != "" {
		t.Errorf("expected empty EXIFCamera, got %q", meta.EXIFCamera)
	}
}

func TestExtractEXIF_FileWithoutEXIF(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "noexif.jpg")
	if _, err := exec.LookPath("convert"); err != nil {
		t.Skip("ImageMagick convert not available")
	}
	cmd := exec.Command("convert", "-size", "2x2", "xc:blue", imgPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("convert failed: %v", err)
	}

	meta := &model.Metadata{}
	extractEXIF(imgPath, meta)
	if meta.EXIFCamera != "" {
		t.Errorf("expected empty EXIFCamera, got %q", meta.EXIFCamera)
	}
}

func TestFFProber_ProbeImage(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "photo.jpg")
	generateEXIFImage(t, imgPath)

	p := NewFFProber()
	ctx := context.Background()
	meta, err := p.Probe(ctx, imgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	// Verify EXIF extraction happened.
	if meta.EXIFCamera != "Canon EOS 5D" {
		t.Errorf("EXIFCamera = %q, want \"Canon EOS 5D\"", meta.EXIFCamera)
	}
}

func TestFFProber_ProbeVideo(t *testing.T) {
	tmpDir := t.TempDir()
	vidPath := filepath.Join(tmpDir, "video.mp4")
	generateVideo(t, vidPath)

	p := NewFFProber()
	ctx := context.Background()
	meta, err := p.Probe(ctx, vidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.Codec == "" {
		t.Error("expected non-empty Codec for video")
	}
	if meta.Resolution == "" {
		t.Error("expected non-empty Resolution for video")
	}
	if meta.Duration <= 0 {
		t.Errorf("expected positive Duration, got %v", meta.Duration)
	}
}

func TestFFProber_ProbeNonExistent(t *testing.T) {
	p := NewFFProber()
	ctx := context.Background()
	_, err := p.Probe(ctx, "/nonexistent/file.mp4")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestFFProber_ProbeEmptyFile(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.mp4")
	if err := os.WriteFile(emptyPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewFFProber()
	ctx := context.Background()
	_, err := p.Probe(ctx, emptyPath)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestParseFFprobeOutput_BitrateParsing(t *testing.T) {
	input := `{"format":{"duration":"60","bit_rate":"0"},"streams":[{"codec_name":"h264","width":640,"height":480,"codec_type":"video"}]}`
	meta, err := parseFFprobeOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Bitrate != 0 {
		t.Errorf("Bitrate = %d, want 0", meta.Bitrate)
	}
}