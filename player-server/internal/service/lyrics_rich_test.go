package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRichLyrics(t *testing.T) {
	yaml := `version: '1.0'
metadata: {language: en, offset_ms: 250}
lines:
 - start_ms: 1250
   end_ms: 4250
   voice: Lead
   translations: {fr: Bonjour monde, ja: こんにちは世界}
   words:
    - {text: 'Hello ', start_ms: 1250, end_ms: 2250}
    - {text: world, start_ms: 2250, end_ms: 4250}
 - {text: Harmony, start_ms: 1250, end_ms: 3250, voice: Harmony}
`
	result := parseLyricsFormat(yaml, "sidecar", ".yaml")
	if !result.Synced || result.Language != "en" || len(result.Lines) != 2 {
		t.Fatalf("rich document: %+v", result)
	}
	line := result.Lines[0]
	if line.Text != "Hello world" || *line.Start != 1 || line.End == nil || *line.End != 4 || line.Voice != "Lead" || line.Translations["fr"] != "Bonjour monde" || len(line.Words) != 2 || line.Words[1].Start != 2 || *line.Words[1].End != 4 {
		t.Fatalf("rich line: %+v", line)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"language":""`) || !strings.Contains(string(wire), `"start":2`) {
		t.Fatalf("wire: %s", wire)
	}
}

func TestRichELRCOffsetsAndFallback(t *testing.T) {
	result := parseLyrics("[offset:250]\n[00:01.25]<00:01.25>Hello <00:02.25>world<00:04.25>\n[00:08]Plain", "sidecar")
	first := result.Lines[0]
	if first.Text != "Hello world" || len(first.Words) != 2 || first.Words[0].Start != 1 || *first.Words[1].End != 4 {
		t.Fatalf("word offset applied incorrectly: %+v", first)
	}
	for _, text := range []string{"[00:01]<00:02>Hello <00:01>world", "[00:01]prefix <00:02>word"} {
		got := parseLyrics(text, "sidecar")
		if len(got.Lines) != 1 || got.Lines[0].Text == "" || len(got.Lines[0].Words) != 0 {
			t.Fatalf("optional fallback: %+v", got)
		}
	}
}

func TestRichWordTextTrimmingAndRepeatedELRC(t *testing.T) {
	got := parseLyrics("[offset:250]\n[00:01.25][00:01.25]<00:01.25>\u2003Hello <00:02.25>world\u2003<00:04.25>", "sidecar")
	if len(got.Lines) != 2 {
		t.Fatal(got)
	}
	for _, line := range got.Lines {
		var text strings.Builder
		for _, word := range line.Words {
			text.WriteString(word.Text)
		}
		if text.String() != line.Text || len(line.Words) != 2 || line.Words[0].Start != 1 || *line.Words[1].End != 4 {
			t.Fatalf("shared offsets/trimming: %+v", line)
		}
	}
}

func TestRichTTMLInheritedWordsAndVoices(t *testing.T) {
	text := `<tt xml:lang="en"><body begin="1s"><div begin="1s"><p begin="1s" end="5s" agent="Lead"><span begin="0s" end="1s">Hello </span><span begin="1s" end="4s">world</span></p><p xml:lang="fr" begin="1s" end="5s" agent="Lead">Bonjour monde</p><p begin="1s" end="3s" agent="Harmony">Other voice</p></div></body></tt>`
	got := parseLyrics(text, "embedded")
	if len(got.Lines) != 2 {
		t.Fatalf("folding removed unrelated voice: %+v", got)
	}
	first := got.Lines[0]
	if first.Text != "Hello world" || *first.Start != 3 || len(first.Words) != 2 || first.Words[1].Start != 4 || *first.End != 7 || first.Translations["fr"] != "Bonjour monde" || got.Lines[1].Voice != "Harmony" {
		t.Fatalf("TTML inheritance: %+v / %+v", first, got.Lines[1])
	}
}

func TestInvalidOptionalRichLyricsRemainReadable(t *testing.T) {
	for _, optional := range []string{
		"words: wrong", "words: [{text: [wrong]}]", "end_ms: nonsense", "end_ms: -1", "end_ms: 500", "voice: [wrong]", "translations: {fr: [wrong]}",
		"words: [{text: 'Hello ', start_ms: wrong}, {text: world, start_ms: 1000}]",
		"words: [{text: 'Hello ', start_ms: 2000}, {text: world, start_ms: 1000}]",
	} {
		got := parseLyricsFormat("version: '1.0'\nlines:\n - text: Hello world\n   start_ms: 1000\n   "+optional, "sidecar", ".yaml")
		if len(got.Lines) != 1 || got.Lines[0].Text != "Hello world" || len(got.Lines[0].Words) != 0 {
			t.Fatalf("%s lost readable lyrics: %+v", optional, got)
		}
	}
	got := parseLyricsFormat("version: '1.0'\nmetadata: {language: [en, fr]}\nlines: [{text: readable, start_ms: 1000}]", "sidecar", ".yaml")
	if len(got.Lines) != 1 || got.Lines[0].Text != "readable" || got.Language != "" {
		t.Fatalf("invalid optional language lost lyrics: %+v", got)
	}

}

func TestEmptyRichLyricsRemainCompatible(t *testing.T) {
	for _, text := range []string{"[lang:en]", "<tt xml:lang=\"en\"><body></body></tt>", "version: '1.0'\nmetadata: {language: en}\nplain: ''"} {
		got := parseLyrics(text, "sidecar")
		if got.Language != "" || len(got.Lines) != 0 || got.Source != "" {
			t.Fatalf("empty response changed: %+v", got)
		}
	}
}

func TestRichTTMLAncestorEndClipping(t *testing.T) {
	for _, tt := range []struct {
		name, text       string
		lineEnd, wordEnd float64
	}{
		{"ancestor clips paragraph and word", `<tt><body end="5s"><p begin="1s" end="10s"><span begin="0s" end="9s">Hello</span></p></body></tt>`, 5, 5},
		{"shorter child remains", `<tt><body end="10s"><p begin="1s" end="5s"><span begin="0s" end="2s">Hello</span></p></body></tt>`, 5, 3},
		{"nested containers retain ancestor bound", `<tt><body end="5s"><div begin="1s" end="8s"><p begin="1s" end="10s"><span begin="0s" end="9s">Hello</span></p></div></body></tt>`, 5, 5},
		{"no ancestor end", `<tt><body><p begin="1s" end="10s"><span begin="0s" end="9s">Hello</span></p></body></tt>`, 10, 10},
		{"malformed optional end retains inherited bound", `<tt><body end="5s"><p begin="1s" end="bad"><span begin="0s" end="bad">Hello</span></p></body></tt>`, 5, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLyrics(tt.text, "sidecar")
			if len(got.Lines) != 1 || len(got.Lines[0].Words) != 1 {
				t.Fatalf("missing readable timed cue: %+v", got)
			}
			line := got.Lines[0]
			word := line.Words[0]
			if line.End == nil || word.End == nil {
				t.Fatal("missing effective end")
			}
			if *line.End != tt.lineEnd || *word.End != tt.wordEnd {
				t.Fatalf("effective ends line=%v word=%v; want %v/%v", *line.End, *word.End, tt.lineEnd, tt.wordEnd)
			}
		})
	}
}

func TestRichTTMLOutsideAncestorInterval(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		texts      []string
	}{
		{"outside only", `<tt><body end="5s"><p begin="6s">Outside</p></body></tt>`, nil},
		{"at exclusive ancestor end", `<tt><body end="5s"><p begin="5s">Outside</p></body></tt>`, nil},
		{"mixed valid and outside", `<tt><body end="5s"><p begin="1s">Inside</p><p begin="6s">Outside</p></body></tt>`, []string{"Inside"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLyrics(tt.text, "sidecar")
			if len(got.Lines) != len(tt.texts) {
				t.Fatalf("active cues=%+v; want texts=%v", got.Lines, tt.texts)
			}
			for i, text := range tt.texts {
				line := got.Lines[i]
				if line.Text != text || line.End == nil || *line.End != 5 {
					t.Fatalf("valid cue/boundary changed: %+v", line)
				}
			}
			if len(tt.texts) == 0 && (got.Synced || got.Source != "") {
				t.Fatalf("outside-only response must remain unavailable: %+v", got)
			}
		})
	}
}
