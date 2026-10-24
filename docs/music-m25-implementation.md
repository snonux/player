# M25 equalizer implementation

Task gk3 uses the implemented nj3 interaction design, oj3 playback foundation
and 1k3 transport. Implementation commit `fa41d52` received expert R1 approval
with no blockers. Actual Chrome, Firefox and native Android acceptance ran on
2026-10-24 against the disposable integrated fixture.

## Controls and states

Web Now Playing → Equalizer disclosure (under More player controls in a small
floating window): Enable equalizer, Preset, five frequency sliders and their
signed dB values, Reset flat, status and Retry equalizer. This colocates the
feature with existing Loudness and Speed rather than adding a second settings
surface. The existing theme tokens and disclosure styles apply in light/dark;
labels wrap on narrow screens. The first Escape collapses the disclosure and
focuses its summary; the next closes the surrounding player controls. Native
range/select behavior retains arrow control and typing protection. No shortcut
is allocated. Preset selection enables the curve; moving a slider selects
Custom; bypass preserves it; Reset flat disables the effect and clears it.

Android Now Playing → Equalizer opens a safe-area scrolling bottom sheet using
native switches, preset dropdown and labeled sliders. Done/Back closes it while
handler-owned playback/equalization continues. Remote playback shows the
existing receiver controls; local DSP cannot affect a remote receiver.

Both clients offer Flat, Bass boost and Voice and five frequency controls at
60, 230, 910, 3600 and 14000 Hz, each from -12 to +12 dB. Positive curves reserve
headroom by subtracting the largest positive band value from all bands. This
changes relative frequency balance while avoiding an extra positive gain stage;
overall volume can decrease. Preferences are device-local and work offline,
independent of server/account. Bypass is the default. Unavailable processing
shows a reason and retry; ordinary playback remains available. Android uses an
embedded effect, so no installed external effect panel or permission is needed.

## Playback composition

Web reuses the existing AudioContext/media-element source: source → five
peaking filters → existing ReplayGain gain → destination. The existing analyzer
still reads the processed output. No parallel destination path or second
context is introduced. Once captured, bypass sets all filter gains to zero,
keeping the routed source audible. Detached transfer includes equalizer state
in the existing loudness snapshot, including live popup state when storage is
blocked. Browser storage events apply edits across windows. Cast status
explicitly explains receiver scope.

Android attaches an engine-lifetime android.media.audiofx.Equalizer to the
existing just_audio session, independently of the existing LoudnessEnhancer.
Session replacement reapplies the preference; bypass/no session releases the
old effect. Physical device bands receive log-frequency interpolation of the
five requested bands, bounded by supported device levels. Exact frequency
response therefore depends on device support. Allocation/configuration failure
releases the effect and reports unavailable. No media source is replaced.

## Verification stage

The real-output runners are `music-equalizer.test.ts` and
`music_equalizer_e2e.py`; component E2E READMEs give repeatable commands.
Evidence is outside git at `/home/paul/tmp/gk3-eq-20261024`. The live server
reports `463eb32-dirty`, serves current filesystem web modules, and remains
the same disposable instance at `http://127.0.0.1:18766`. Native is the normal
0.8.2 release APK (versionCode 4022, source `369243f`) on Android 14/API 34
x86_64 `Player_FDroid_Test_API34`.

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156 both pass the
real destination-output journey (2 tests, 51.6 seconds). Flat 100/1000/8000 Hz
bands each measure -27.54 dBFS. Setting the 910 Hz slider to -12 through its
native Home key changes the 1 kHz component by -11.73 dB while neighboring
probe tones change by less than 0.4 dB. Bass boost and Voice favor their named
frequency regions; bypass and Reset recover flat output within the unchanged
1.5 dB tolerance. The same Voice output returns after page reload, and buffered
offline Bass output matches online. Escape restores summary focus; preset and
range controls retain native behavior. Light/narrow and dark/wide screenshots
were inspected, with the narrow controls opened through More player controls.

Actual QEMU output from native Three Tones has flat bands
-27.54/-27.55/-28.34 dBFS (spread 0.80 dB). The custom 910 Hz cut measures
-28.32/-39.42/-29.04 dBFS; Bass measures -28.66/-35.80/-39.64 and Voice
-35.43/-28.59/-32.78. The native response differs from the browser's filters
as documented. A touch selected Voice while the actual Three Tones session
reported PLAYING before and after; captured output proves the live update.
Bypass preserves the curve and recovers flat output; Reset clears/disables it.
Home/background output, Retry, light/dark sheets, Back, buffered offline
playback, and persisted Voice on a recreated audio session after cold unlock
and explicit source reopening also pass. No server/emulator restart or app-data
clear was needed.

The external-panel negative case temporarily disabled only the unused
`com.android.musicfx` package. Android reported no activity resolving
`android.media.action.DISPLAY_AUDIO_EFFECT_CONTROL_PANEL`, while Player's
embedded EQ still produced the changed Voice output. Its exact package enabled
state and the temporarily removed adb reverse mapping were restored in
`finally`. This verifies no dependency on an external panel; it does not claim
that this supported emulator is an unsupported DSP device. Allocation-failure
and retry logic also retain their existing targeted unit coverage.

The first cold-start verifier attempt incorrectly assumed Resume queue would
replace the separately retained music queue with the transient file probe.
That failed source assertion is retained in `native-panel-recovery.log`;
`native-recovery.log` exercises the corrected explicit source reopening.
No product change was required. The original EQ setting and saved queue,
including its starting occurrence and offset, are restored at cleanup.

Previously reviewed Node graph/settings/detached tests and the integrated full
Go race/coverage, web JS, Flutter analyzer, 1674 Flutter host tests and release
APK build/install gates are reused: this acceptance slice changes only runners
and documentation. The earlier three sequence-test failures were repaired
before these integrated checks; they are no longer a current gap. Shared
ReplayGain, speed and saved-queue acceptance was completed separately in ll3,
fk3 and 2k3. Cast receiver DSP remains outside local equalizer scope.
