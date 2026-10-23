# M58 — Explicit headset behavior (`9l3`)

Implementation and both bounded review rounds are complete; integrated acceptance remains pending.
Prerequisites `nj3` (interaction design), `oj3` (music foundation), and `qk3`
(system playback controls) are completed. Guest collection integration follows
reviewed M62 commit `1e4a587`. Shared version remains unchanged.

## Interaction and states

Android Settings, after Appearance and presentation preferences, has a
**Headphones** section in both the device library and signed-in settings:

```text
Headphones
On this device · wired headphones and Bluetooth audio.
Phone calls and audio focus still take priority.
[on ] Pause when headphones disconnect
      Turn off to allow playback through the phone speaker.
[off] Resume when headphones reconnect
      Only resumes playback paused by disconnecting.
      Pausing yourself, switching libraries or a sleep timer cancels resume.
```

Each switch saves immediately. Resume is disabled while disconnect pause is
off; the stored resume choice remains available when pause is re-enabled.
Loading/saving disables changes with an inline progress bar. Failed loads use
pause-on/resume-off defaults and explain the failure. Failed writes leave the
active choices unchanged and show a retry message. Preferences work offline,
need no headset/Bluetooth scan permission, and apply device-wide across accounts.
Standard Material switches, wrapping text, existing typography/theme colors,
scrolling and Back remain in use. There are no new shortcuts or dialogs.
Light/dark, 320 px widget checks verify readable wrapping and working switches;
actual screenshots, screen-reader and touch/Back device checks remain pending.

Playing → disconnect → paused is the default. Opting into reconnect allows
paused-by-disconnect → same-source resume once. Connecting initially, opening
the app, restoring a queue, or connecting while already paused never starts
playback. Explicit pause, stop, source/account/library replacement, sleep expiry,
and share closure cancel the ticket. Deferred source preparation cannot restart
playback after a disconnect or focus pause. Cast receiver playback is not
controlled by native headphone events; existing cast handoff pauses local audio.

Chrome and Firefox have no dependable matching native headset output API in
this feature's baseline. Their current playback controls are preserved; no
headset settings or synthetic browser integration are added. Actual shared
play/pause/source/settings regression journeys are deferred with the rest of
the integrated platform checks.

## Native ownership and dependency evidence

Installed just_audio 0.9.46 `lib/just_audio.dart:302–351` already owns noisy
pause and focus handling under **one** `handleInterruptions` switch. It was
not missing. Custom unplug behavior therefore replaces those listeners together
with per-player `HeadsetPolicy`; `handleAudioSessionActivation` stays enabled,
so just_audio still obtains audio focus before playing. Existing music versus
speech audio-session configuration remains unchanged. See the pinned
[just_audio documentation](https://pub.dev/packages/just_audio/versions/0.9.46)
and [audio_session documentation](https://pub.dev/packages/audio_session/versions/0.1.25).

`audio_session` remains at the already installed 0.1.25; it is now a direct
dependency. Its `core.dart:249–287` maps transient loss to pause, permanent loss
to unknown, and subsequent gain to pause. Only transient loss may arm automatic
focus resume. Unknown loss cannot gain that permission later. Native duck
events keep music playing, retaining OS ducking; game-mode explicit attenuation
restores only its own unchanged volume, preserving intervening user/gain edits.
Speech's pause-when-ducked mapping still arrives as a pause event.

Shared `HeadsetPreferences` persists one coherent pair. The background audio
handler and page-local guest collection each own separate policy tickets and
subscriptions, using the same preferences. Handler source generation, ownership,
sleep and transport revision guard resume across asynchronous loudness work.
Guest resume requires its current public-share lease, ready player, no error,
and mounted screen; its policy is disposed with the page. Source URLs, public
token boundaries, progress and queue data retain their existing owners.

Connection events accept only output wired headphones/headsets and Bluetooth
A2DP. Input, speaker, SCO call routes and generic USB outputs cannot autostart.
The stream observes changes, and an existing device at initialization has no
resume ticket. No physical-device behavior is inferred from these filters.

## Focused checks and remaining acceptance

On 2026-10-23, implementation based on HEAD `1e4a587`:

- `flutter pub get --offline`: only audio_session dependency classification changed.
- Scoped `flutter analyze --no-pub` on all 13 changed Dart source/test files:
  no issues (1.5 s).
- `flutter test --no-pub test/services/headset_policy_test.dart test/services/headset_handler_test.dart test/widgets/headset_settings_test.dart test/widgets/public_share_headset_test.dart test/screens/settings_screen_test.dart test/services/audio_handler_progress_test.dart test/services/sleep_timer_test.dart test/services/gapless_playback_test.dart`:
  58 tests pass (6 s). Tests cover persisted safe defaults, restart/corrupt data,
  output filters, single-use reconnect, initial-connect silence, manual pause,
  source/lock/stop/sleep cancellation, pending resume versus manual pause,
  transient/permanent focus, call/unplug overlap, duck volume preservation,
  guest lease/close boundaries, and light/dark narrow settings.
- `git diff --check`: passes. Existing URL-field widget tests now scroll the
  field into view before tapping after the added settings section.

An initial new widget-test compile failed on an unnecessary `await` applied to
a void reset method; corrected before the passing run above. An earlier source
self-check found and corrected permanent-loss/GAIN mapping and deferred-start
ownership; both have focused regressions. These are not review rounds.

Round 1 review found a remaining direct native start in `_moveTrack`: a held
track loader could finish after disconnect/focus/manual pause and start audio.
Four new held-loader regressions reproduced this before the fix. Transitions
now capture a transport revision before I/O and require the same revision plus
policy permission at the final direct start. Explicit Next/Previous/selection
begins a new policy intent only after the boundary no-op check; natural
completion preserves the existing intent. Regressions assert the newly loaded
track stays paused and a subsequent deliberate Next still plays. A fifth test
queues natural completion after unplug, holds its loader, and verifies the
existing blocked policy and sleep intent remain unchanged.
After the fix, `flutter analyze --no-pub lib/services/audio_handler.dart
test/services/headset_handler_test.dart` reports no issues (6.4 s), and
`flutter test --no-pub test/services/headset_handler_test.dart
test/services/music_sequence_test.dart test/services/gapless_playback_test.dart`
passes all 48 tests (2 s). Round 2 confirmed this blocker is resolved and found no new blocking defect in the fix; it was read-only and did not rerun checks.

Full regression suites, final builds, actual installed Chrome/Firefox regression
journeys, Android preference restart/touch/Back/screenshots, emulator noisy,
connect/disconnect and focus/call events, and authoritative audio/output checks
are explicitly deferred under the implementation-first workflow. Actual wired
and Bluetooth device events remain unverified. No task completion, release,
version bump, push or deployment is claimed.
