package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"github.com/snonux/player/internal/model"
)

// MaxMusicArtworkBytes bounds uploads before image decoding.
const MaxMusicArtworkBytes = 5 << 20

// NormalizeMusicArtwork rejects oversized/invalid images before full decoding.
// Reencoding strips metadata and animation and gives all clients one safe format.
func NormalizeMusicArtwork(data []byte) (*model.MusicArtwork, error) {
	invalid := &apiError{msg: "Choose a valid JPEG, PNG or GIF image up to 5 MB and 4096 pixels per side (maximum 8 million pixels).", status: 400}
	if len(data) == 0 || len(data) > MaxMusicArtworkBytes {
		return nil, invalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 8_000_000 {
		return nil, invalid
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalid
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, fmt.Errorf("encode artwork: %w", err)
	}
	if output.Len() > MaxMusicArtworkBytes {
		return nil, invalid
	}
	hash := sha256.Sum256(output.Bytes())
	return &model.MusicArtwork{Data: output.Bytes(), Revision: fmt.Sprintf("%x", hash)}, nil
}
