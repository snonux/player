# Additional lyric formats (A03a / vj3)

Reviewed implementation `c5dbe68`; integrated platform acceptance passed.
Prerequisites nj3 (U01) and oj3 (V01) are accepted. The original two review
rounds are complete; acceptance does not restart that budget.

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

### Integrated browser acceptance (2026-10-24)

Retained fixture/evidence: `/home/paul/tmp/vj3-evidence/`. The additive files
are under the owned normal backend's `Music/A03a Formats` folder on port
18771; no shared server restart or database edit. `cases.json` contains exact
sidecar bytes; `indexed.json` maps the eight cases to media IDs 48–55.
`/home/paul/tmp/vj3-setup.py` records generation/rescan. All MP3s copy the
same 12-second 523.25 Hz source with distinct titles. Timed lines start at
0, 2 and 8 seconds, including Unicode and a wrapping final line.

Cases: TTML (explicit 25 fps, inferred 50 ticks = 2 seconds), Lyricsfile
YAML 1.0 (250 ms positive offset), Enhanced LRC word markers and SRT markup;
YAML explicit plain text; malformed TTML/YAML-alias plus empty ELRC falling
through to LRC; TTML winning over valid YAML/text/embedded; and malformed
structured sidecars with no embedded lyrics producing unavailable. Literal
`[bad] nope` remains compatible plain LRC/ELRC text, so it is not used as a
malformed structured candidate.

Installed Chrome 144.0.7559.132 and Firefox 156 passed all eight cases with
the shared browser verifier (`final-browsers.log`: 24.0s / 51.4s). Each timed
case proves actual stream identity, advancing playback, Enter seeking with
retained focus, `p` pause, pointer seeking while paused, current-line state,
and loaded lyrics seeking offline. Plain YAML has no line action; unavailable
lyrics leave audio playing. Each case has narrow 390×844 light/dark screenshots.
Settings Escape restores focus. Rendered current and inactive lyric text both
meet 4.5:1 contrast against the panel in light/dark. The acceptance fix pairs
the panel's existing theme text/accent with `--bg-surface`; previously light
theme inactive text was nearly invisible against the permanent black stage.
The normal server is 0.8.2 / `c7b6aea-dirty`; browser assets came from the
current checkout, with that CSS fix committed separately as `86cb296`.
Independent real API acceptance (`parent-api-results.json`) records 30 passing
checks across both route aliases, exact timing/text, priority/fallback and
authentication/access boundaries. `mage webTest` passes (`web-test.log`).

Run from `player-server/test/e2e-web`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/player-quality-fixture \
PLAYER_LYRIC_FORMAT_CASES=/home/paul/tmp/vj3-evidence/indexed.json \
PLAYER_LYRIC_FORMAT_EVIDENCE=/home/paul/tmp/vj3-evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
  npx playwright test -c playwright.music.config.ts music-lyric-formats \
  --project=chrome --project=firefox --workers=1
```

The browser assertions use actual media element state rather than claiming
that a parser unit test proves playback.

### Integrated native acceptance (2026-10-24)

The same eight cases pass in the installed normal release APK on the owned
API 34 / Android 14 x86_64 emulator (`native-platform.log`, `android/results.json`).
APK 0.8.2 / code 4022, source `bd478ab`, installed SHA-256
`c783857aa3e8f4058ff1f80c703d170bd2ec36b13a28c48ac0d7d2e485ce7a7c`;
physical size 1080×2220, density 440 (phone layout). No test APK, data clear,
preference-file write, emulator restart or server restart was used.

Every timed case, including priority and malformed fallback, proves native
touch seeking to 2, 8 and 2 seconds while paused, the selected/current lyric
state, then actual playback from the earlier line and real QEMU WAV output
at 523.2–525.3 Hz (expected 523.25 Hz) for 2.46–2.66 seconds. Settled paused positions and
fresh UI dumps are used; sparse playing MediaSession positions are not treated
as exact timing. The long final line wraps visibly in the current-line banner.
Back returns to the Library after every case. Plain YAML has no seek action
and a tap leaves the paused position unchanged; malformed-only input shows
the concise **No lyrics available.** result while playback remains available.

Retained native runner command, from the repository root:

```sh
PYTHONPATH=player-android/test/e2e-live:player-android/integration_test/support \
  python3 -u /home/paul/tmp/vj3-evidence/native.py
```

The runner uses the supported **Play now → Open player → Lyrics** controls
with the disposable admin account. A persistent queue Undo snackbar on this
accessibility-enabled emulator is dismissed with its native downward swipe
before opening the bottom Lyrics button. Earlier setup/driver attempts remain
in the evidence directory; the final eight-case run passes cleanly. Existing
F01 native offline/buffered-seek evidence is reused for the unchanged line
client; this run does not claim new uncached streaming works offline.

Native TTML was also captured under System light and dark (`system-light.png`,
`system-dark.png`); its established black player and readable lyric panel are
retained in both. OS night mode was restored to its original **yes**. The
retained `restore.py` uses public queue CAS and explicit native **Continue
session → Replace**, then switches/unlocks the original 18766 admin account.
All four saved local queues compare exactly with the pre-run JSON, including
18771 at media 38 / 2.687s and the original repeated-song 18766 queue at 4.69s.
System theme, Teal/Circle/Fit/keep-awake, visuals Off and loudness Off remain
unchanged; transport is NONE with no audio. Only the original reverse mapping
18766 remains. Proof: `restoration.log`, `android/restoration.json`,
`restored-preferences.json` and `final-reverse.txt`.

No format-specific platform acceptance gap remains. Shared full Go race/coverage
and server-build results from `c7b6aea`, plus clean Flutter analysis, all 1685
tests and the `bd478ab` APK build, are reused because their production code is
unchanged. The CSS-only fix passed the full web JavaScript suite and actual
browser checks without requiring another server or Android build. No release,
version change, push or deployment.
