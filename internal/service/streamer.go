package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/probe"
)

type mediaStreamer struct {
	remuxer probe.Remuxer
}

// NewMediaStreamer creates the default service for preparing media files for HTTP streaming.
func NewMediaStreamer(remuxer probe.Remuxer) MediaStreamer {
	return &mediaStreamer{remuxer: remuxer}
}

func (s *mediaStreamer) Open(ctx context.Context, file *FileResult, attachment bool) (*StreamResult, error) {
	if file == nil {
		return nil, ErrNotFound
	}

	f, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: open stream file: %v", ErrNotFound, err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: stat stream file: %v", ErrNotFound, err)
	}

	remuxed := !attachment && s.remuxer != nil && probe.LooksLikeMPEGTS(file.Path)
	contentType := mediatype.MIMETypeForExt(file.FileName)
	if remuxed {
		contentType = "video/mp4"
	}

	return &StreamResult{
		File:        f,
		Path:        file.Path,
		FileName:    file.FileName,
		Size:        stat.Size(),
		ModTime:     stat.ModTime(),
		ContentType: contentType,
		Attachment:  attachment,
		Remuxed:     remuxed,
		Duration:    file.Duration,
	}, nil
}

func (s *mediaStreamer) Remux(ctx context.Context, stream *StreamResult, w io.Writer) error {
	if stream == nil {
		return ErrNotFound
	}
	if s.remuxer == nil {
		return errors.New("remuxer not configured")
	}
	return s.remuxer.Remux(ctx, stream.Path, w)
}
