package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
)

// Lyrics contains readable text; Start is in seconds and absent for plain lyrics.
type Lyrics struct {
	ProviderEnabled bool         `json:"provider_enabled,omitempty"`
	ProviderState   string       `json:"provider_state,omitempty"`
	Source          string       `json:"source"`
	Language        string       `json:"language,omitempty"`
	Synced          bool         `json:"synced"`
	Lines           []LyricsLine `json:"lines"`
}

// LyricsLine retains readable text even when optional rich metadata is absent.
type LyricsLine struct {
	Text         string            `json:"text"`
	Start        *float64          `json:"start,omitempty"`
	End          *float64          `json:"end,omitempty"`
	Voice        string            `json:"voice,omitempty"`
	Words        []LyricsWord      `json:"words,omitempty"`
	Translations map[string]string `json:"translations,omitempty"`
	language     string
}

// LyricsWord is a text fragment with absolute playback timing in seconds.
type LyricsWord struct {
	Text  string   `json:"text"`
	Start float64  `json:"start"`
	End   *float64 `json:"end,omitempty"`
}

func (s *browseService) GetLyrics(ctx context.Context, mediaID, userID int64) (*Lyrics, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	result := s.loadLyrics(ctx, media, userID, "")
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *shareService) GetSharedLyrics(ctx context.Context, token string, credentials ...string) (*Lyrics, error) {
	media, err := s.ResolveSharedMedia(ctx, token, credentials...)
	if err != nil {
		return nil, err
	}
	return localLyrics(ctx, s.lyricsRoot, media), nil
}

// localAudioSource validates an audio item for local metadata reads. It
// returns the absolute root, the indexed path and the resolved regular file,
// or ok=false for missing, unreadable and unsafe sources.
func localAudioSource(ctx context.Context, root string, media *model.Media) (absRoot, mediaPath, source string, ok bool) {
	// Normalize both sides before resolving symlinks: a relative MEDIA_ROOT
	// can point at the same tree as an absolute path already stored in SQLite.
	if root == "" || media.AbsPath == "" {
		return "", "", "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", false
	}
	mediaPath, err = filepath.Abs(media.AbsPath)
	if err != nil {
		return "", "", "", false
	}
	if media.Type != model.MediaTypeAudio || ctx.Err() != nil || !confinedPath(absRoot, mediaPath) {
		return "", "", "", false
	}
	source, err = resolveSourcePath(absRoot, mediaPath)
	if err != nil {
		return "", "", "", false
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", false
	}
	return absRoot, mediaPath, source, true
}

// Missing, unreadable and unsafe sources all have the same empty response.
func localLyrics(ctx context.Context, root string, media *model.Media) *Lyrics {
	empty := &Lyrics{Lines: []LyricsLine{}}
	root, mediaPath, source, ok := localAudioSource(ctx, root, media)
	if !ok {
		return empty
	}
	// Match the indexed filename, including when it is a confined symlink.
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	for _, ext := range []string{".ttml", ".yaml", ".yml", ".elrc", ".lrc", ".srt", ".txt"} {
		path := stem + ext
		if !confinedPath(root, path) {
			continue
		}
		realPath, err := resolveSourcePath(root, path)
		if err != nil {
			continue
		}

		// Root.OpenFile also confines symlink resolution at open time. Nonblock
		// prevents a sidecar replaced with a FIFO from hanging the request.
		rootDir, err := os.OpenRoot(root)
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			_ = rootDir.Close()
			continue
		}
		absRoot, err := filepath.Abs(realRoot)
		if err != nil {
			_ = rootDir.Close()
			continue
		}
		absPath, err := filepath.Abs(realPath)
		if err != nil {
			_ = rootDir.Close()
			continue
		}
		relative, err := filepath.Rel(absRoot, absPath)
		if err != nil {
			_ = rootDir.Close()
			continue
		}
		f, err := rootDir.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		_ = rootDir.Close()
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > probe.MaxLyricsBytes {
			_ = f.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, probe.MaxLyricsBytes+1))
		_ = f.Close()
		if err != nil || len(data) > probe.MaxLyricsBytes {
			continue
		}
		result := parseLyricsFormat(string(data), "sidecar", ext)
		if len(result.Lines) > 0 {
			return result
		}
	}
	if ctx.Err() != nil {
		return empty
	}
	candidates, err := probe.ReadLyricsCandidates(ctx, source)
	if err != nil {
		return empty
	}
	for _, text := range candidates {
		if result := parseLyrics(text, "embedded"); len(result.Lines) > 0 {
			return result
		}
	}
	return empty
}

func parseLRCLyrics(text, source string) *Lyrics {
	result := &Lyrics{Lines: []LyricsLine{}}
	if len(text) > probe.MaxLyricsBytes {
		return result
	}
	text = strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\ufeff")
	// Compile locally: no package-level mutable state.
	wordTimestamp := regexp.MustCompile(`<\d{1,5}:[0-5]\d(?:\.\d{1,3})?>`)
	timestamp := regexp.MustCompile(`^\[(\d{1,5}):([0-5]\d(?:\.\d{1,3})?)\]`)
	offsetTag := regexp.MustCompile(`(?i)^\[offset:\s*([+-]?\d+)\s*\]$`)
	metadata := regexp.MustCompile(`(?i)^\[(ar|al|ti|au|by|re|ve|length|offset|lang|la|lr|tool|#):.*\]$`)
	languageTag := regexp.MustCompile(`(?i)^\[(?:lang|la):\s*([^\]]+)\]$`)
	var offset float64
	var plain []LyricsLine
	outputBytes := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if match := offsetTag.FindStringSubmatch(line); match != nil {
			if n, err := strconv.ParseInt(match[1], 10, 32); err == nil {
				offset = float64(n) / 1000
			}
			continue
		}
		if match := languageTag.FindStringSubmatch(line); match != nil && validLyricLanguage(match[1]) {
			result.Language = match[1]
		}
		if metadata.MatchString(line) {
			continue
		}
		var starts []float64
		for {
			match := timestamp.FindStringSubmatch(line)
			if match == nil {
				break
			}
			minutes, _ := strconv.ParseFloat(match[1], 64)
			seconds, _ := strconv.ParseFloat(match[2], 64)
			starts = append(starts, minutes*60+seconds)
			line = line[len(match[0]):]
		}
		if len(starts) == 0 {
			if line != "" {
				outputBytes += len(line) + 64
				if outputBytes > probe.MaxLyricsBytes {
					return &Lyrics{Lines: []LyricsLine{}}
				}
				plain = append(plain, LyricsLine{Text: line})
			}
		} else {
			rawWords := parseELRCWords(line, wordTimestamp)
			line = wordTimestamp.ReplaceAllString(line, "")
			for _, start := range starts {
				outputBytes += len(line) + 64
				if outputBytes > probe.MaxLyricsBytes {
					return &Lyrics{Lines: []LyricsLine{}}
				}
				result.Lines = append(result.Lines, LyricsLine{Text: strings.TrimSpace(line), Start: &start, Words: rawWords})
			}
		}
	}
	if len(result.Lines) > 0 {
		result.Synced = true
		for i := range result.Lines {
			*result.Lines[i].Start = max(0, *result.Lines[i].Start-offset)
			words := append([]LyricsWord(nil), result.Lines[i].Words...)
			for j := range words {
				words[j].Start = max(0, words[j].Start-offset)
				if words[j].End != nil {
					n := max(0, *words[j].End-offset)
					words[j].End = &n
				}
			}
			result.Lines[i].Words = words
			sanitizeRichLine(&result.Lines[i])
		}
		sort.SliceStable(result.Lines, func(i, j int) bool { return *result.Lines[i].Start < *result.Lines[j].Start })
	} else if len(plain) > 0 {
		result.Lines = plain
	}
	if len(result.Lines) > 0 {
		result.Source = source
	} else {
		result.Language = ""
	}
	boundRichLyrics(result)
	return result
}
