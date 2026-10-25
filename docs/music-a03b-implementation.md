# Rich lyrics (A03b / 5k3)

Implementation and bounded review complete; awaiting integrated verification.
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

## Verification plan / gaps

Focused parser, JS model/DOM and Flutter model/widget checks during implementation.
Parent owns full Go race/coverage/static gates, full JS and Flutter suites, final
server/APK builds and actual installed Chrome/Firefox/native acceptance. Integrated
journeys must play synthetic multilingual overlapping-voice/word fixtures, compare
word and cue intervals, forward/back seek and switch languages while paused and
playing, then check simple timed/plain fallback, loaded offline, track replacement,
focus/typing/Back, and light/dark/narrow/text-scale contrast and wrapping. No actual
platform verification is claimed by focused checks.

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
All actual Chrome/Firefox/native runtime/theme/narrow/reduced-motion checks and
full suites/final builds remain explicitly deferred to integrated verification.
No runtime, server, emulator state, version, release or generated build was changed.

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
Integrated verification and task acceptance remain pending.

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
`git diff --check` pass. Only parser/test and this note changed. No additional
review round, full gate, build, runtime or acceptance result is claimed.
