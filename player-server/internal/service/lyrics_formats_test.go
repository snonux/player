package service

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
)

func TestAdditionalLyricsFormats(t *testing.T) {
	for _, tt := range []struct {
		name, format, text string
		synced             bool
		texts              []string
		times              []float64
	}{
		{"plain arrow", "", "First --> second\nNext line", false, []string{"First --> second", "Next line"}, nil},
		{"enhanced LRC", ".elrc", "[00:02.25]<00:02.25>Hello <00:02.75>world<00:03.00>\n[00:01]早安", true, []string{"早安", "Hello world"}, []float64{1, 2.25}},
		{"existing lrc enhanced", ".lrc", "[00:01][00:03]<00:01>one <00:02>two\n[offset:500]", true, []string{"one two", "one two"}, []float64{.5, 2.5}},
		{"SRT", ".srt", "\ufeff2\r\n00:00:02,250 --> 00:00:03,000\r\n<b>Hello</b> &amp; world\r\nnext\r\n\r\n1\r\n00:00:00.500 --> 00:00:01.000\r\n早安", true, []string{"早安", "Hello & world\nnext"}, []float64{.5, 2.25}},
		{"SRT no index", "", "00:00:01,000 --> 00:00:02,000\ntext", true, []string{"text"}, []float64{1}},
		{"TTML", ".ttml", `<?xml version="1.0"?><tt xmlns="http://www.w3.org/ns/ttml"><body><div><p begin="00:00:02.250" end="3s"><span begin="0s">Hello </span><span begin=".5s">world</span><br/>next</p><p begin="500ms">早安 &amp; friends</p></div></body></tt>`, true, []string{"早安 & friends", "Hello world\nnext"}, []float64{.5, 2.25}},
		{"TTML inherited", "", `<tt><body begin="1s"><div begin="2s"><p begin="500ms">relative</p></div></body></tt>`, true, []string{"relative"}, []float64{3.5}},
		{"TTML frames and ticks", ".ttml", `<tt xmlns:ttp="http://www.w3.org/ns/ttml#parameter" ttp:frameRate="25" ttp:tickRate="10"><body><p begin="00:00:01:10">frames</p><p begin="20t">ticks</p></body></tt>`, true, []string{"frames", "ticks"}, []float64{1.4, 2}},
		{"TTML plain", ".ttml", `<tt><body><div><p>  Hello  <span>world</span> </p><p>next</p></div></body></tt>`, false, []string{"Hello world", "next"}, nil},
		{"YAML", ".yaml", "version: '1.0'\nmetadata:\n  offset_ms: 250\nlines:\n  - text: replaced\n    start_ms: 2500\n    words:\n      - text: 'Hello '\n        start_ms: 2500\n      - text: world\n        start_ms: 3000\n  - text: 早安\n    start_ms: 0\nplain: ignored", true, []string{"早安", "Hello world"}, []float64{0, 2.25}},
		{"YAML sniff", "", "# Lyricsfile\nversion: '1.0'\nlines: [{text: line, start_ms: 1000}]", true, []string{"line"}, []float64{1}},
		{"YAML literal plain", ".yml", "version: '1.0'\nplain: |\n  [Verse 1]\n  [00:01]literal", false, []string{"[Verse 1]", "[00:01]literal"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLyricsFormat(tt.text, "sidecar", tt.format)
			if got.Source != "sidecar" || got.Synced != tt.synced || len(got.Lines) != len(tt.texts) {
				t.Fatalf("result %+v", got)
			}
			for i, line := range got.Lines {
				if line.Text != tt.texts[i] {
					t.Errorf("line %d %q want %q", i, line.Text, tt.texts[i])
				}
				if tt.synced {
					if line.Start == nil || math.Abs(*line.Start-tt.times[i]) > 1e-9 {
						t.Errorf("time %d %v want %v", i, line.Start, tt.times[i])
					}
				} else if line.Start != nil {
					t.Errorf("plain line has time")
				}
			}
		})
	}
}

func TestTTMLTickRate(t *testing.T) {
	for _, tt := range []struct {
		name, attrs, begin string
		want               float64
	}{
		{"default", "", "50t", 50},
		{"subframes without explicit frame rate", `ttp:subFrameRate="2"`, "50t", 50},
		{"inferred", `ttp:frameRate="25"`, "50t", 2},
		{"explicit override", `ttp:frameRate="25" ttp:tickRate="10"`, "50t", 5},
		{"inferred multiplier", `ttp:frameRateMultiplier="1000 1001" ttp:frameRate="30"`, "30t", 1.001},
		{"inferred subframes", `ttp:subFrameRate="2" ttp:frameRate="25"`, "50t", 1},
		{"multiplier and subframes", `ttp:frameRate="30" ttp:subFrameRate="2" ttp:frameRateMultiplier="1000 1001"`, "60t", 1.001},
		{"explicit override with multiplier", `ttp:tickRate="10" ttp:frameRate="30" ttp:subFrameRate="2" ttp:frameRateMultiplier="1000 1001"`, "50t", 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text := `<tt xmlns:ttp="http://www.w3.org/ns/ttml#parameter" ` + tt.attrs + `><body><p begin="` + tt.begin + `">line</p></body></tt>`
			got := parseLyricsFormat(text, "sidecar", ".ttml")
			if !got.Synced || len(got.Lines) != 1 || got.Lines[0].Start == nil || math.Abs(*got.Lines[0].Start-tt.want) > 1e-9 {
				t.Fatalf("tick timing %+v; want %v seconds", got, tt.want)
			}
		})
	}
}

func TestMalformedLyricsFormats(t *testing.T) {
	for _, tt := range []struct{ name, format, text string }{
		{"broken XML", ".ttml", `<tt><body><p begin="1s">partial</p>`},
		{"XML entities", ".ttml", `<!DOCTYPE tt [<!ENTITY secret SYSTEM "file:///etc/passwd">]><tt><body><p>&secret;</p></body></tt>`},
		{"XML deep", ".ttml", `<tt>` + strings.Repeat(`<div>`, maxLyricsDepth) + `x` + strings.Repeat(`</div>`, maxLyricsDepth) + `</tt>`},
		{"XML sequential", ".ttml", `<tt><body timeContainer="seq"><p begin="1s">wrong</p></body></tt>`},
		{"XML NaN", ".ttml", `<tt><body><p begin="00:00:NaN">wrong</p></body></tt>`},
		{"XML head not lyrics", ".ttml", `<tt><head><p begin="1s">metadata</p></head></tt>`},
		{"SRT malformed", ".srt", "1\n00:00:NaN --> 00:00:02,000\nwrong"},
		{"SRT backwards", ".srt", "1\n00:00:02,000 --> 00:00:01,000\nwrong"},
		{"SRT partial", ".srt", "1\n00:00:01,000 --> 00:00:02,000\nvalid\n\nbroken"},
		{"YAML version", ".yaml", "version: '2.0'\nplain: wrong"},
		{"YAML alias", ".yaml", "version: '1.0'\nplain: &p wrong\nextra: *p"},
		{"YAML deep", ".yaml", "version: '1.0'\nplain: wrong\nextra: " + strings.Repeat("[", maxLyricsDepth) + "x" + strings.Repeat("]", maxLyricsDepth)},
		{"YAML duplicate", ".yaml", "version: '1.0'\nplain: one\nplain: two"},
		{"YAML documents", ".yaml", "version: '1.0'\nplain: wrong\n---\nplain: other"},
		{"YAML missing time", ".yaml", "version: '1.0'\nlines: [{text: wrong}]"},
		{"YAML negative", ".yaml", "version: '1.0'\nlines: [{text: wrong, start_ms: -1}]"},
		{"YAML huge time", ".yaml", "version: '1.0'\nlines: [{text: wrong, start_ms: 9223372036854775807}]"},
		{"YAML no vocals", ".yaml", "version: '1.0'\nmetadata: {instrumental: true}\nplain: wrong"},
		{"YAML too many nodes", ".yaml", "version: '1.0'\nplain: wrong\nextra: [" + strings.Repeat("x,", maxLyricsNodes) + "]"},
		{"oversized", ".srt", strings.Repeat("x", probe.MaxLyricsBytes+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseLyricsFormat(tt.text, "sidecar", tt.format); got.Source != "" || got.Synced || len(got.Lines) > 0 {
				t.Fatalf("malformed source leaked: %+v", got)
			}
		})
	}
}

func TestAdditionalLyricsSidecarPriority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "not real audio; sidecars must avoid ffprobe")
	media := &model.Media{AbsPath: path, Type: model.MediaTypeAudio}
	formats := []struct{ ext, text, want string }{
		{".ttml", `<tt><body><p begin="1s">ttml</p></body></tt>`, "ttml"},
		{".yaml", "version: '1.0'\nlines: [{text: yaml, start_ms: 1000}]", "yaml"},
		{".yml", "version: '1.0'\nplain: yml", "yml"},
		{".elrc", "[00:01]<00:01>elrc", "elrc"},
		{".lrc", "[00:01]lrc", "lrc"},
		{".srt", "1\n00:00:01,000 --> 00:00:02,000\nsrt", "srt"},
		{".txt", "plain", "plain"},
	}
	for _, f := range formats {
		write(filepath.Join(root, "song"+f.ext), f.text)
	}
	for _, f := range formats {
		got := localLyrics(context.Background(), root, media)
		if got.Source != "sidecar" || len(got.Lines) != 1 || got.Lines[0].Text != f.want {
			t.Fatalf("%s priority: %+v", f.ext, got)
		}
		// Leave malformed structured candidates in place to prove actual fallthrough.
		if f.ext == ".ttml" || f.ext == ".yaml" || f.ext == ".yml" || f.ext == ".srt" {
			write(filepath.Join(root, "song"+f.ext), "invalid")
		} else if err := os.Remove(filepath.Join(root, "song"+f.ext)); err != nil {
			t.Fatal(err)
		}
	}
}
