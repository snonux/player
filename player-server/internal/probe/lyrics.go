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
	candidates, err := ReadLyricsCandidates(ctx, path)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", nil
	}
	return candidates[0], nil
}

// ReadLyricsCandidates reads bounded local lyrics in deterministic priority:
// format tags before stream tags in stream order, with keys sorted per group.
// The caller validates the source path and selects the first usable candidate.
func ReadLyricsCandidates(ctx context.Context, path string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	args, err := lyricsArgs(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.WaitDelay = time.Second
	out := &boundedLyricsOutput{}
	cmd.Stdout = out
	// Diagnostics are intentionally discarded: they may contain local paths.
	if err := cmd.Run(); err != nil {
		return nil, err
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
		return nil, err
	}
	tags := []map[string]string{result.Format.Tags}
	for _, stream := range result.Streams {
		tags = append(tags, stream.Tags)
	}
	var candidates []string
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
					candidates = append(candidates, value)
				}
			}
		}
	}
	return candidates, nil
}

type boundedLyricsOutput struct{ bytes.Buffer }

func (b *boundedLyricsOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*MaxLyricsBytes {
		return 0, errors.New("lyrics metadata too large")
	}
	return b.Buffer.Write(p)
}
