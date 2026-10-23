# Additional lyric formats (A03a / vj3)

Implementation stage; integrated verification pending. Prerequisites nj3 (U01)
and oj3 (V01) are accepted. Implementation-first workflow defers browser/native
acceptance, full suites and final builds.

## Interaction and data design (recorded before implementation)

Keep the existing Now Playing / share / detached lyrics region and native
LyricsPanel. The format stays out of everyday playback controls. Each accepted
format projects onto the existing `source`, `synced`, `lines[{text,start?}]`
contract: line timing drives highlighting, following and seek; word spans become
ordinary readable line text. No new shortcut, dialog, setup or permission.

```text
Existing Lyrics control -> existing scrolling region
  Previous line                    timed: existing button / native tap-to-seek
  Current line [existing emphasis]  follows playback; reading/focus retained
  Long lyric wraps                 existing narrow/light/dark presentation
```

Loading and failed/offline lookup retain existing behavior: web hides unavailable
lyrics without interrupting audio; native shows its spinner, concise unavailable
message or Retry lyrics. Malformed or oversized format candidates are skipped;
no raw XML/YAML/cue timing is displayed as fallback. If another local candidate
is usable it is shown, otherwise the existing unavailable state applies. Plain
lyrics stay noninteractive. Existing browser keyboard/focus and Android touch/
Back semantics are unchanged.

Source priority is the first usable matching filename-stem sidecar in this order:
`.ttml`, `.yaml`, `.yml`, `.elrc`, `.lrc`, `.srt`, `.txt`, then embedded tags.
Existing `.lrc` still wins over `.txt`; any usable sidecar wins over embedded.
Embedded format tags precede stream tags (stream order); keys within each tag
group sort lexically. Invalid candidates fall through deterministically.

Support scope: TTML paragraphs and inline spans project to line text, including
clock/offset timing and inherited parallel-container timing; sequential TTML
containers are rejected. Lyricsfile version 1.0 accepts timed lines (word text
concatenation when supplied) or its explicit `plain` fallback, with metadata
millisecond offset. Player deliberately applies the existing LRC convention to
`offset_ms`: positive means earlier, negative means later, clamped at zero
(e.g. start_ms 2500 with offset_ms 250 yields 2.25 seconds). The Lyricsfile
concept names an offset without defining its sign; this is Player compatibility
behavior. Enhanced LRC removes word timestamp markers while retaining
line timing. SRT projects cue starts and multiline text, stripping presentation
markup. End times, language/voice/translation selection and word highlighting
are outside this line-display slice (A03b / 5k3).

All inputs retain the 1 MiB bound. Expanded line text plus per-line overhead is
also bounded; structured document depth/counts and numeric times are bounded.
YAML aliases are rejected before typed decoding. No network source, external
entity resolver or file lookup is introduced by parsing.

TTML ticks follow [TTML 1 §6.2.10](https://www.w3.org/TR/ttml1/#parameter-attribute-tickRate):
without explicit tickRate, an explicit frameRate derives ticks from effective
frameRate × subFrameRate after the frame-rate multiplier; otherwise one tick
per second. Explicit tickRate overrides this inference.

Schema reference: [Lyricsfile concept](https://github.com/tranxuanthang/lrcget/blob/main/LYRICSFILE_CONCEPT.md).

## Verification

Targeted checks passed:

- `cd player-server && go mod download gopkg.in/yaml.v3`
- `go test ./internal/service ./internal/probe -run 'Test(TTMLTickRate|ParseLyrics|AdditionalLyrics|MalformedLyrics|LocalLyrics|LyricsExpansionBound|LyricsArgs|LyricsOutputBound|ReadLyrics)' -count=1` (final after R1 fix: 0.919s / 1.178s).
- `go vet ./internal/service ./internal/probe`.
- `goimports` on the six changed lyric Go files; `git diff --check`.

Scoped `errcheck ./internal/service ./internal/probe` exits 1 on existing
package diagnostics (probe/remux read-only closes; service tests, workers,
podcasts, streaming, radio and enrichment). No new format/parser/probe diagnostic.
The touched lyric reader's read-only closes explicitly discard errors; inherited
lyrics safety tests still contain existing ignored Remove/Symlink errors.
A post-check source fix tightens embedded SRT sniffing to timestamp headers so
ordinary plain lyrics containing `-->` remain readable; its regression passes.
R1 found an inferred TTML tick-rate blocker; the fix applies the effective
multiplied frame rate and subframes. Eight exact timing regressions pass for
defaults, explicit overrides, inferred ticks, fractional frame rates and
subframes. Narrow R2 confirmed the blocker fixed with no new blocking defect (2/2 rounds used).
Integrated acceptance must play equivalent format fixtures on installed Chrome,
Firefox and native Android, checking actual timing/line seek/plain fallback,
malformed candidates, long text, keyboard/touch/Back, and light/dark/narrow
presentation. No platform acceptance is claimed by server parser tests.
