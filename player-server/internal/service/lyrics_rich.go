package service

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/snonux/player/internal/probe"
)

// Optional richness never makes otherwise usable line lyrics unavailable.
func sanitizeRichLine(line *LyricsLine) {
	if line.End != nil && (line.Start == nil || !validLyricTime(*line.End) || *line.End <= *line.Start) {
		line.End = nil
	}
	if len(line.Voice) > 128 {
		line.Voice = ""
	}
	if len(line.Translations) > 32 {
		line.Translations = nil
	}
	for language, text := range line.Translations {
		if !validLyricLanguage(language) || strings.TrimSpace(text) == "" {
			delete(line.Translations, language)
		}
	}
	var text strings.Builder
	valid := line.Start != nil && len(line.Words) <= maxLyricsNodes
	for i, word := range line.Words {
		text.WriteString(word.Text)
		if !validLyricTime(word.Start) || line.Start == nil || word.Start < *line.Start || (i > 0 && word.Start < line.Words[i-1].Start) ||
			(line.End != nil && word.Start >= *line.End) || (word.End != nil && (!validLyricTime(*word.End) || *word.End <= word.Start || (line.End != nil && *word.End > *line.End))) {
			valid = false
		}
	}
	if strings.TrimSpace(text.String()) != line.Text {
		valid = false
	}
	if !valid {
		line.Words = nil
		return
	}
	// Match the existing trimmed line projection, including Unicode whitespace
	// and whitespace-only edge fragments, without losing interior separators.
	for i := 0; i < len(line.Words); i++ {
		line.Words[i].Text = strings.TrimLeftFunc(line.Words[i].Text, unicode.IsSpace)
		if line.Words[i].Text != "" {
			break
		}
	}
	for i := len(line.Words) - 1; i >= 0; i-- {
		line.Words[i].Text = strings.TrimRightFunc(line.Words[i].Text, unicode.IsSpace)
		if line.Words[i].Text != "" {
			break
		}
	}

}

func validLyricLanguage(language string) bool {
	if len(language) > 64 {
		return false
	}
	parts := strings.Split(language, "-")
	for i, part := range parts {
		if len(part) < 1 || len(part) > 8 || (i == 0 && len(part) < 2) {
			return false
		}
		for _, ch := range part {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || i > 0 && ch >= '0' && ch <= '9') {
				return false
			}
		}
	}
	return true
}

func parseELRCWords(text string, timestamp *regexp.Regexp) []LyricsWord {
	matches := timestamp.FindAllStringIndex(text, -1)
	if len(matches) == 0 || len(matches) > maxLyricsNodes || strings.TrimSpace(text[:matches[0][0]]) != "" {
		return nil
	}
	words := []LyricsWord{}
	for i, match := range matches {
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		value := text[match[1]:end]
		if value == "" {
			continue
		} // A final marker commonly marks the last word's end.
		parts := strings.Split(text[match[0]+1:match[1]-1], ":")
		minutes, _ := strconv.ParseFloat(parts[0], 64)
		seconds, _ := strconv.ParseFloat(parts[1], 64)
		word := LyricsWord{Text: value, Start: minutes*60 + seconds}
		if i+1 < len(matches) {
			next := text[matches[i+1][0]+1 : matches[i+1][1]-1]
			parts = strings.Split(next, ":")
			minutes, _ = strconv.ParseFloat(parts[0], 64)
			seconds, _ = strconv.ParseFloat(parts[1], 64)
			n := minutes*60 + seconds
			word.End = &n
		}
		words = append(words, word)
	}
	return words
}

func lyricMilliseconds(value any, offset float64) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	case uint64:
		n = float64(v)
	case float64:
		n = v
	default:
		return 0, false
	}
	if !validLyricTime(n / 1000) {
		return 0, false
	}
	return max(0, n/1000-offset), true
}

// Alternate language paragraphs fold only when their start, end and explicit
// voice agree; same-language and unspecified-language simultaneous cues stay.
func foldLyricTranslations(lyrics *Lyrics, language string) {
	if len(lyrics.Lines) == 0 {
		return
	}
	if !validLyricLanguage(language) {
		for _, line := range lyrics.Lines {
			if validLyricLanguage(line.language) {
				language = line.language
				break
			}
		}
	}
	if !validLyricLanguage(language) {
		return
	}
	lyrics.Language = language
	type cueKey struct {
		start, end float64
		hasEnd     bool
		voice      string
	}
	keyFor := func(line LyricsLine) cueKey {
		key := cueKey{start: *line.Start, voice: line.Voice}
		if line.End != nil {
			key.end = *line.End
			key.hasEnd = true
		}
		return key
	}
	originals := map[cueKey]int{}
	lines := []LyricsLine{}
	for _, line := range lyrics.Lines {
		if line.Start != nil && validLyricLanguage(line.language) && line.language != language {
			if i, ok := originals[keyFor(line)]; ok {
				original := &lines[i]
				if original.Translations == nil {
					original.Translations = map[string]string{}
				}
				if _, exists := original.Translations[line.language]; !exists && len(original.Translations) < 32 {
					original.Translations[line.language] = line.Text
					continue
				}
			}
		}
		if line.Start != nil && line.language == language {
			key := keyFor(line)
			if _, exists := originals[key]; !exists {
				originals[key] = len(lines)
			}
		}
		lines = append(lines, line)
	}

	lyrics.Lines = lines
	boundRichLyrics(lyrics)
}

func boundRichLyrics(lyrics *Lyrics) {
	remaining := probe.MaxLyricsBytes
	for _, line := range lyrics.Lines {
		remaining -= len(line.Text) + 64
	}
	for i := range lyrics.Lines {
		line := &lyrics.Lines[i]
		cost := len(line.Voice) + 64
		for language, text := range line.Translations {
			cost += len(language) + len(text) + 32
		}
		for _, word := range line.Words {
			cost += len(word.Text) + 64
		}
		if cost > remaining {
			line.Words = nil
			line.Translations = nil
			line.Voice = ""
			line.End = nil
			continue
		}
		remaining -= cost
	}
}
