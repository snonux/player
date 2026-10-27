# Rich lyrics (A03b / 5k3)

Implementation, bounded review and integrated platform acceptance complete.
Prerequisites nj3/U01, oj3/V01 and vj3/A03a are accepted. This extends their
existing line model, parsers and player surfaces; no new playback owner or API.

## Layout and state contract (recorded before feature coding)

```text
Web existing audio stage           Android existing expandable Lyrics panel
Lyrics [hide/show]                 [Hide lyrics]
Language [Original (en) v]         Language [Original (en) v]  native chooser
Voice: Lead                       Current: Voice: Lead · Hello world
Hello WORLD [current underline]   Voice: Lead
Voice: Harmony                    Hello WORLD [current underline]
Hello there                       Tap to seek
```

Use the current theme surface/text/accent tokens, system type, wrapping centered
web rows and Material list tiles on Android. Keep the existing lyric toggle,
scrolling bounds and playback chrome. Only show Language when translations exist;
the initial choice is Original, and each new track resets it. Missing translations
show the original line with its original language label so partial translations
remain understandable. Voice is a visible text prefix, never color alone. Multiple
voices remain separate seekable rows. All rows whose intervals contain playback
are highlighted, including overlaps and identical starts; the existing current
banner/follow anchor uses the last currently active row. End is exclusive; absent
end uses the next strictly later line start, or remains active for the final cue.

Word text retains spaces/punctuation. Current word has bold underlined text,
without fading, animation or scrolling per word; reduced motion uses the same
instant treatment. Recompute from current playback on every tick and forward/back
seek, clearing all stale highlights. A translated line uses line highlighting
only: translation text has no invented word timing. Line buttons retain line seek
and current pause/play state. No nested word buttons or new global shortcuts.
Native selects retain arrows/typeahead, Tab/Enter and current focus. Existing
j/k reading, Space/p playback, Escape/back hierarchy and automatic following
remain; language selection retains focus and does not seek or restart playback. On narrow
floating web windows Language joins the existing More player controls popover;
its ordinary native-select change dismisses that popover and returns focus to
More, consistently with the other player selectors.
Android has a labeled native dropdown, 48dp controls and normal Back dismissal.
Long voices/translations/words wrap; no horizontal-only interaction.

Loading, unavailable, permission, malformed-source, error and offline behavior
remain A03a/F01: independent optional lookup, web hides missing lyrics, Android
spinner/No lyrics available/Retry lyrics. Loaded words/translations remain usable
offline while loaded media permits seeks. No new permission or network provider.
Invalid optional rich data drops to complete readable line text. Unsupported
structured timing retains existing safe parser rejection rules.

## Additive data and parser contract

Existing source/synced/lines/text/start stay compatible. Optional top-level
language and line end/voice/words/translations carry richness. All times are
absolute playback seconds. Words are {text,start,end?}; text concatenates to the
complete line. Translations map language codes to complete plain line text.
YAML 1.0 adds metadata.language and line voice/translations plus word start_ms/
end_ms. ELRC inline word timestamps and LRC offset use existing sign/clamping.
TTML retains inherited xml:lang/agent, paragraph/span begin/end timing; alternate
language paragraphs at the same start and voice can supply translations. Voice
IDs are displayed verbatim as text. Sequential containers remain unsupported.
Bounded bytes/depth/node counts, safe local paths, YAML alias rejection and
existing source priority are preserved. Invalid optional word timing falls back
to complete text; invalid optional end is omitted.

## Verification plan

Focused parser, JS model/DOM and Flutter model/widget checks supported the
implementation review. Integrated acceptance below plays synthetic multilingual
overlapping-voice/word fixtures in Chrome, Firefox and the native app; it checks
forward/back seek, language choice, line-only fallback, loaded offline lyrics,
focus/touch/Back and light/dark/narrow presentation. Large native text wrapping
is covered by the widget test rather than a separate device font-scale run.

## Focused implementation checks

- `cd player-server && go test ./internal/service -run 'Test(Rich|InvalidOptionalRich|EmptyRich|ParseLyrics|AdditionalLyrics|MalformedLyrics|LyricsExpansionBound|TTMLTickRate)' -count=1` passed (final 0.026s, including empty rich-response compatibility).
- `go vet ./internal/service` passed. `errcheck ./internal/service` ran and reports
  inherited diagnostics in tests/workers/podcasts/streaming/radio/enrichment;
  no diagnostic in the changed lyric implementation/new rich tests.
- `node player-server/web/js/tests/lyrics.test.js` passed: model intervals,
  forward/back word/overlap state, partial translation including Object prototype
  names, native-select focus/no seek, detached source guard and simple fallback.
- `cd player-android && flutter analyze lib/models/lyrics.dart lib/widgets/lyrics_panel.dart test/lyrics_test.dart test/widgets/lyrics_panel_test.dart` passed, no issues.
- `flutter test test/lyrics_test.dart test/widgets/lyrics_panel_test.dart` passed
  all 11 tests, including native chooser, overlapping voices, current-banner word
  spans, seek preservation, partial translation, invalid richness and existing
  simple/long-text/loading/error/source-replacement behavior.

During focused checks, the minimal JS DOM double lacked querySelector for the
new language-focus restoration path; its boundary was added and the test rerun.
An attempted compact-controls test path does not exist; no such result is claimed.
The first scoped Dart analysis reported two missing braces; fixed, analysis clean.
At this implementation stage, Chrome/Firefox/native runtime checks remained
deferred. The integrated record below supersedes that stage.

## Bounded review and fixes

R1 identified one blocking P2: TTML child `end` replaced the inherited ancestor
end, allowing cues to remain active beyond a parent container. Effective ends
now take the minimum usable parent/child end at every context level. Exact
regression first failed for direct and nested ancestors, then passed: body end
5 seconds clips paragraph end 10 and its span end to 5. Additional cases retain
a shorter child end, absent ancestor bounds and malformed optional-end fallback.

R1 also identified a small nonblocking native label inconsistency: a word-timed
current banner with a missing selected-language translation omitted Original.
The rich banner now appends the same Original language label as the plain banner
and list row. Its native widget regression chooses French while an untranslated,
word-timed Harmony cue is active and checks both `Original: en` and retained word
underlining without a seek.

Post-fix focused checks: the documented Go lyric regex (including the new
`TestRichTTMLAncestorEndClipping`) passes in 0.034 seconds; scoped service vet
passes. Scoped Flutter analysis of the two changed native files is clean; all
11 lyric model/widget tests pass. `git diff --check` passes. No full gates,
server/APK builds, Chrome/Firefox/emulator acceptance or runtime changes. Parent
completed narrow final R2 (2/2 review budget), which approved these fixes.
Integrated verification and task acceptance are recorded below.

### Parent-directed boundary confirmation after R2

R2 approved the R1 delta (2/2 review rounds used). A subsequent narrow parent
inspection identified an adjacent zero-active-interval boundary: a paragraph
starting at or after its inherited ancestor end reached optional-end sanitization,
which removed the bound and made the final cue unbounded. Three exact regressions
confirmed this before the fix: outside-only paragraph at 6 seconds under body
end 5, paragraph exactly at the exclusive end 5, and valid+outside mixed cues.
TTML paragraph emission now excludes those empty active intervals before optional
metadata sanitization. The mixed case retains its Inside cue and exact end 5;
outside-only documents use the existing unavailable response. Existing shorter
child/no-parent/malformed optional-end fallback regressions remain green.

Post-boundary focused Go lyric regex passes (0.037 seconds); service vet and
`git diff --check` pass. Only parser/test and this note changed at that stage.
No additional review round was used.

## Integrated acceptance — 2026-10-27

The owned disposable fixture at `127.0.0.1:18849` used server 0.8.2, build
revision `8691244ad0d7b45f3403bd56981ee2e81e404f2d-dirty`. The same YAML
word/voice/translation document was placed on a copied 523.25 Hz browser track
and the base album's 440 Hz native track; an existing LRC track supplied the
line-only fallback. The x86_64 release APK was 0.8.2+4022, SHA256
`58d7048ed0ce2c1388141f111688eea6299b5ed10b7313b4de3dfa33a5661f56`.
Only one API34 emulator ran at a time. Commands are in the two component E2E
READMEs. Evidence and synthetic media stay outside git in
`/home/paul/tmp/codex/player-a03b-evidence`.

| Platform | Result | Evidence |
| --- | --- | --- |
| Installed Chrome 144.0.7559.132 | PASS: actual 523.25 Hz audio, two active voices, word emphasis at 1/2 seconds, French and untranslated Original, keyboard/pointer seek, narrow More language choice/focus, reduced motion, disconnected loaded seek, light/dark screenshots and LRC fallback. | `1793111691247-522648/chrome/evidence.jsonl` |
| Installed Firefox 156.0 | PASS: the same journey, including the narrow native-select popover path. | `1793111693040-522649/firefox/evidence.jsonl` |
| Native API34 x86_64 | PASS 10: touch overlap, 440 Hz audio and visually reviewed underlined current word, translated/fallback labels, forward/back seek, loaded text/language choice during real server-route loss, light/dark, line-only LRC and Back. | `native-1793111349265633258/android/evidence.jsonl` |

The latest Chrome/Firefox narrow light/dark screenshots were visually reviewed:
the Settings transition had completed, rows remained readable, and no horizontal
overflow appeared. Native screenshots show the underlined “world” span and
readable French and Original labels. The player uses its existing dark media
surface under both Android system modes. Twelve focused Flutter model/widget
tests, the JS lyric model/DOM test and `go test -p 1 ./... -race -cover`
passed. The Go log is `/home/paul/tmp/codex/player-a03b-go-race-cover.log`.

The browser offline check seeks within a loaded, buffered media source. The
native disconnected check retains loaded lyric text and language selection;
seeking an uncached streamed source during server loss leaves media buffering,
so no offline stream-seek claim is made here. A03c separately covers durable
offline lyric snapshots for downloaded audio. An external lyric provider and
a large native device font-scale run are outside this focused A03b journey;
the large-text widget test covers layout behavior.
