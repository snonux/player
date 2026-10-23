package service

import (
	"encoding/xml"
	"html"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"codeberg.org/snonux/player/internal/probe"
	"gopkg.in/yaml.v3"
)

const (
	maxLyricsDepth   = 32
	maxLyricsNodes   = 32768
	maxLyricsSeconds = 1e9
)

type lyricBuilder struct {
	lines   []LyricsLine
	bytes   int
	invalid bool
}

type lyricsfileDocument struct {
	Version  string `yaml:"version"`
	Metadata struct {
		Offset       int64 `yaml:"offset_ms"`
		Instrumental bool  `yaml:"instrumental"`
	} `yaml:"metadata"`
	Lines []struct {
		Text  string `yaml:"text"`
		Start *int64 `yaml:"start_ms"`
		End   *int64 `yaml:"end_ms"`
		Words []struct {
			Text string `yaml:"text"`
		} `yaml:"words"`
	} `yaml:"lines"`
	Plain string `yaml:"plain"`
}

type ttmlContext struct {
	start    float64
	timed    bool
	body     bool
	preserve bool
}

type ttmlRates struct{ frame, subframe, tick float64 }

func parseLyrics(text, source string) *Lyrics {
	return parseLyricsFormat(text, source, "")
}

func parseLyricsFormat(text, source, format string) *Lyrics {
	if len(text) > probe.MaxLyricsBytes {
		return emptyLyrics()
	}
	text = strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\ufeff")
	if format == "" {
		trimmed := strings.TrimSpace(text)
		switch {
		case strings.HasPrefix(trimmed, "<?xml"), regexp.MustCompile(`^<(?:[\w-]+:)?tt(?:\s|>)`).MatchString(trimmed):
			format = ".ttml"
		case regexp.MustCompile(`(?m)^version\s*:`).MatchString(text):
			format = ".yaml"
		case regexp.MustCompile(`(?m)^\s*\d{2,5}:\d{2}:\S+\s+-->`).MatchString(text):
			format = ".srt"
		}
	}
	switch format {
	case ".ttml":
		return parseTTMLLyrics(text, source)
	case ".yaml", ".yml":
		return parseYAMLLyrics(text, source)
	case ".srt":
		return parseSRTLyrics(text, source)
	default:
		return parseLRCLyrics(text, source)
	}
}

func emptyLyrics() *Lyrics { return &Lyrics{Lines: []LyricsLine{}} }

func (b *lyricBuilder) add(text string, start *float64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.bytes += len(text) + 64
	if b.bytes > probe.MaxLyricsBytes || len(b.lines) >= maxLyricsNodes || (start != nil && !validLyricTime(*start)) {
		b.invalid = true
		return
	}
	b.lines = append(b.lines, LyricsLine{Text: text, Start: start})
}

func (b *lyricBuilder) result(source string) *Lyrics {
	if b.invalid || len(b.lines) == 0 {
		return emptyLyrics()
	}
	synced := true
	for _, line := range b.lines {
		if line.Start == nil {
			synced = false
			break
		}
	}
	if synced {
		sort.SliceStable(b.lines, func(i, j int) bool { return *b.lines[i].Start < *b.lines[j].Start })
	} else {
		// A mixed document remains readable without creating unsafe seek controls.
		for i := range b.lines {
			b.lines[i].Start = nil
		}
	}
	return &Lyrics{Source: source, Synced: synced, Lines: b.lines}
}

func validLyricTime(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= maxLyricsSeconds
}

func parseYAMLLyrics(text, source string) *Lyrics {
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil || !boundedLyricsYAML(&node, 0, new(int)) {
		return emptyLyrics()
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return emptyLyrics()
	}
	var doc lyricsfileDocument
	if err := node.Decode(&doc); err != nil || doc.Version != "1.0" || doc.Metadata.Instrumental {
		return emptyLyrics()
	}
	if math.Abs(float64(doc.Metadata.Offset)/1000) > maxLyricsSeconds {
		return emptyLyrics()
	}
	b := &lyricBuilder{}
	if len(doc.Lines) == 0 {
		// Plain field is literal text, including bracketed headings, never LRC.
		for _, line := range strings.Split(doc.Plain, "\n") {
			b.add(line, nil)
		}
	} else {
		for _, line := range doc.Lines {
			if line.Start == nil || *line.Start < 0 || (line.End != nil && *line.End < *line.Start) {
				return emptyLyrics()
			}
			value := line.Text
			if len(line.Words) > 0 {
				var words strings.Builder
				for _, word := range line.Words {
					words.WriteString(word.Text)
				}
				value = words.String()
			}
			start := float64(*line.Start) / 1000
			if !validLyricTime(start) {
				return emptyLyrics()
			}
			start = max(0, start-float64(doc.Metadata.Offset)/1000)
			b.add(value, &start)
		}
	}
	return b.result(source)
}

func boundedLyricsYAML(node *yaml.Node, depth int, count *int) bool {
	*count++
	if depth > maxLyricsDepth || *count > maxLyricsNodes || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return false
	}
	for _, child := range node.Content {
		if !boundedLyricsYAML(child, depth+1, count) {
			return false
		}
	}
	return true
}

func parseSRTLyrics(text, source string) *Lyrics {
	b := &lyricBuilder{}
	blocks := regexp.MustCompile(`\n[\t ]*\n`).Split(strings.TrimSpace(text), -1)
	markup := regexp.MustCompile(`<[^>]*>`)
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
			continue
		}
		if _, err := strconv.ParseUint(strings.TrimSpace(lines[0]), 10, 64); err == nil {
			lines = lines[1:]
		}
		if len(lines) < 2 {
			return emptyLyrics()
		}
		times := strings.Split(lines[0], "-->")
		if len(times) != 2 {
			return emptyLyrics()
		}
		start, ok := parseSRTTime(strings.TrimSpace(times[0]))
		end, endOK := parseSRTTime(strings.TrimSpace(times[1]))
		if !ok || !endOK || end < start {
			return emptyLyrics()
		}
		value := html.UnescapeString(markup.ReplaceAllString(strings.Join(lines[1:], "\n"), ""))
		b.add(value, &start)
	}
	return b.result(source)
}

func parseSRTTime(text string) (float64, bool) {
	if !regexp.MustCompile(`^\d{2,5}:[0-5]\d:[0-5]\d[,.]\d{3}$`).MatchString(text) {
		return 0, false
	}
	return parseTTMLTime(strings.Replace(text, ",", ".", 1), ttmlRates{frame: 30, subframe: 1, tick: 1})
}

func parseTTMLLyrics(text, source string) *Lyrics {
	decoder := xml.NewDecoder(strings.NewReader(text))
	rates := ttmlRates{frame: 30, subframe: 1, tick: 1}
	stack := []ttmlContext{}
	b := &lyricBuilder{}
	var line strings.Builder
	var lineStart *float64
	paragraph := 0
	rootSeen := false
	tokens := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return emptyLyrics()
		}
		tokens++
		if tokens > maxLyricsNodes {
			return emptyLyrics()
		}
		switch t := token.(type) {
		case xml.Directive:
			return emptyLyrics()
		case xml.StartElement:
			if len(stack) >= maxLyricsDepth {
				return emptyLyrics()
			}
			ctx := ttmlContext{}
			if len(stack) > 0 {
				ctx = stack[len(stack)-1]
			} else {
				if rootSeen || t.Name.Local != "tt" {
					return emptyLyrics()
				}
				rootSeen = true
				if !rates.read(t.Attr) {
					return emptyLyrics()
				}
			}
			if t.Name.Local == "body" {
				ctx.body = true
			}
			for _, attr := range t.Attr {
				switch attr.Name.Local {
				case "timeContainer":
					if attr.Value != "par" {
						return emptyLyrics()
					}
				case "space":
					ctx.preserve = attr.Value == "preserve"
				case "begin":
					n, ok := parseTTMLTime(attr.Value, rates)
					if !ok || !validLyricTime(ctx.start+n) {
						return emptyLyrics()
					}
					ctx.start += n
					ctx.timed = true
				}
			}
			stack = append(stack, ctx)
			if ctx.body && t.Name.Local == "p" {
				if paragraph != 0 {
					return emptyLyrics()
				}
				paragraph = len(stack)
				line.Reset()
				lineStart = nil
				if ctx.timed {
					n := ctx.start
					lineStart = &n
				}
			} else if paragraph > 0 && t.Name.Local == "br" {
				line.WriteByte('\n')
			}
		case xml.CharData:
			if paragraph > 0 {
				if stack[len(stack)-1].preserve {
					line.Write(t)
				} else {
					// XML indentation collapses to spaces without losing word boundaries.
					for _, ch := range string(t) {
						if ch == '\n' || ch == '\r' || ch == '\t' {
							ch = ' '
						}
						line.WriteRune(ch)
					}
				}
			} else if len(stack) == 0 && strings.TrimSpace(string(t)) != "" {
				return emptyLyrics()
			}
		case xml.EndElement:
			if paragraph == len(stack) {
				value := line.String()
				if !stack[len(stack)-1].preserve {
					for strings.Contains(value, "  ") {
						value = strings.ReplaceAll(value, "  ", " ")
					}
				}
				b.add(value, lineStart)
				paragraph = 0
			}
			stack = stack[:len(stack)-1]
		}
	}
	if !rootSeen || len(stack) != 0 {
		return emptyLyrics()
	}
	return b.result(source)
}

func (r *ttmlRates) read(attrs []xml.Attr) bool {
	var explicitFrame, explicitTick bool
	for _, attr := range attrs {
		switch attr.Name.Local {
		case "frameRate", "subFrameRate", "tickRate":
			n, err := strconv.ParseFloat(attr.Value, 64)
			if err != nil || !validLyricTime(n) || n == 0 {
				return false
			}
			switch attr.Name.Local {
			case "frameRate":
				r.frame = n
				explicitFrame = true
			case "subFrameRate":
				r.subframe = n
			case "tickRate":
				r.tick = n
				explicitTick = true
			}
		}
	}
	for _, attr := range attrs {
		if attr.Name.Local == "frameRateMultiplier" {
			parts := strings.Fields(attr.Value)
			if len(parts) != 2 {
				return false
			}
			numerator, err1 := strconv.ParseFloat(parts[0], 64)
			denominator, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 != nil || err2 != nil || numerator <= 0 || denominator <= 0 {
				return false
			}
			r.frame *= numerator / denominator
			if !validLyricTime(r.frame) || r.frame == 0 {
				return false
			}
		}
	}
	// TTML 1 section 6.2.10 derives ticks from the effective frame rate only
	// when frameRate is explicit and tickRate is omitted; without either it is 1.
	if explicitFrame && !explicitTick {
		r.tick = r.frame * r.subframe
		if !validLyricTime(r.tick) || r.tick == 0 {
			return false
		}
	}
	return true
}

func parseTTMLTime(text string, rates ttmlRates) (float64, bool) {
	parts := strings.Split(text, ":")
	if len(parts) == 3 || len(parts) == 4 {
		h, err1 := strconv.ParseUint(parts[0], 10, 32)
		m, err2 := strconv.ParseUint(parts[1], 10, 8)
		sec, err3 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil || err3 != nil || m >= 60 || sec >= 60 || sec < 0 {
			return 0, false
		}
		n := float64(h)*3600 + float64(m)*60 + sec
		if len(parts) == 4 {
			frames := strings.Split(parts[3], ".")
			f, err := strconv.ParseUint(frames[0], 10, 32)
			if err != nil || float64(f) >= rates.frame || len(frames) > 2 {
				return 0, false
			}
			n += float64(f) / rates.frame
			if len(frames) == 2 {
				sub, err := strconv.ParseUint(frames[1], 10, 32)
				if err != nil || float64(sub) >= rates.subframe {
					return 0, false
				}
				n += float64(sub) / rates.subframe / rates.frame
			}
		}
		return n, validLyricTime(n)
	}
	match := regexp.MustCompile(`^((?:\d+(?:\.\d+)?|\.\d+))(ms|h|m|s|f|t)$`).FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	switch match[2] {
	case "ms":
		n /= 1000
	case "h":
		n *= 3600
	case "m":
		n *= 60
	case "f":
		n /= rates.frame
	case "t":
		n /= rates.tick
	}
	return n, validLyricTime(n)
}
