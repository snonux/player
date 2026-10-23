package service

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestNormalizeMusicArtwork(t *testing.T) {
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	art, err := NormalizeMusicArtwork(valid.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	again, err := NormalizeMusicArtwork(art.Data)
	if err != nil || again.Revision != art.Revision {
		t.Fatalf("unstable revision: %v", err)
	}
	var huge bytes.Buffer
	if err := png.Encode(&huge, image.NewGray(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"empty": nil, "invalid": []byte("not image"), "large dimensions": huge.Bytes(), "large bytes": make([]byte, MaxMusicArtworkBytes+1), "truncated": valid.Bytes()[:valid.Len()-10]} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeMusicArtwork(data); err == nil {
				t.Fatal("accepted invalid artwork")
			}
		})
	}
}
