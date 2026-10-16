package ffsafetest

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// Audio makes a short FLAC, optionally containing a blue back and red front
// picture in that order. The artwork inputs stay outside the scanned library.
func Audio(t testing.TB, path string, artwork bool) {
	t.Helper()
	args := []string{"-f", "lavfi", "-i", "sine=duration=0.1"}
	if artwork {
		for i, c := range []color.RGBA{{B: 255, A: 255}, {R: 255, A: 255}} {
			name := filepath.Join(t.TempDir(), "art.jpg")
			img := image.NewRGBA(image.Rect(0, 0, 32+i*16, 32))
			for y := 0; y < 32; y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					img.SetRGBA(x, y, c)
				}
			}
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			err = jpeg.Encode(f, img, nil)
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			args = append(args, "-i", "file:"+name)
		}
		args = append(args, "-map", "0:a", "-map", "1:v", "-map", "2:v", "-c:v", "copy",
			"-disposition:v", "attached_pic", "-metadata:s:v:0", "comment=Cover (back)",
			"-metadata:s:v:1", "comment=Cover (front)")
	}
	Generate(t, path, append(args, "-c:a", "flac")...)
}
