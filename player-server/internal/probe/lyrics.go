package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
)

// MaxLyricsBytes bounds sidecar text and embedded lyrics.
const MaxLyricsBytes = 1 << 20

func lyricsArgs(path string) ([]string, error) {
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		return nil, err
	}
	return append([]string{"-v", "error", "-show_entries", "format_tags:stream_tags", "-of", "json"}, input...), nil
}

// ReadLyrics reads local format/stream lyrics tags, with bounded output and runtime.
// The caller must validate that path is a regular file inside its media root.
func ReadLyrics(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	args, err := lyricsArgs(path)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.WaitDelay = time.Second
	out := &boundedLyricsOutput{}
	cmd.Stdout = out
	// Diagnostics are intentionally discarded: they may contain local paths.
	if err := cmd.Run(); err != nil {
		return "", err
	}
	var result struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Tags map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return "", err
	}
	tags := []map[string]string{result.Format.Tags}
	for _, stream := range result.Streams {
		tags = append(tags, stream.Tags)
	}
	for _, group := range tags {
		keys := make([]string, 0, len(group))
		for key := range group {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			name := strings.ToLower(key)
			if name == "lyrics" || name == "unsyncedlyrics" || name == "unsynced lyrics" || strings.HasPrefix(name, "lyrics-") {
				value := group[key]
				if len(value) <= MaxLyricsBytes && strings.TrimSpace(value) != "" {
					return value, nil
				}
			}
		}
	}
	return "", nil
}

type boundedLyricsOutput struct{ bytes.Buffer }

func (b *boundedLyricsOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*MaxLyricsBytes {
		return 0, errors.New("lyrics metadata too large")
	}
	return b.Buffer.Write(p)
}
