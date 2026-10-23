# M25 equalizer implementation

Task gk3 uses the implemented nj3 interaction design, oj3 playback foundation
and 1k3 transport. This slice is IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One expert review found no blockers; integrated acceptance remains required.

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

Cheap Node checks cover bounded settings, presets/headroom, production graph
composition and bypass, one source/context across replacement, and detached
state transfer with blocked storage. Native targeted tests cover safe settings,
session reattachment, bypass/reset, ordered persistence and unavailable retry.
Scoped analyzer (`flutter analyze --no-pub` on the five affected Dart files)
passes with no issues; `flutter test --no-pub test/services/equalizer_test.dart
test/services/audio_handler_errors_test.dart
test/services/audio_handler_progress_test.dart` passes 12 tests. Existing
music_sequence_test.dart has three failures at lines 320, 699 and 1139
(`player.running` remains false after retry/jump). Removing all EQ handler
integration reproduced exactly the same three failures (28 pass/3 fail);
restoring it retains the same failures (combined initial run 40 pass/3 fail).
These inherited sequence failures remain for integrated verification; they are
not a passing regression gate. These are coherence checks, not evidence of
actual audible platform behavior.

Deferred: full suites, final server/APK builds, Chrome/Firefox and emulator
interaction/spectrum captures, light/dark/narrow screenshot review, actual
unsupported-device handling, restart/offline and keyboard/touch/Back journeys.
The integrated phase must measure changed frequency levels during real audio,
then bypass/reset and verify ReplayGain/speed/queue/sleep/detached and output
routing regressions. Task remains pending until required platform acceptance.
