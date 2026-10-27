Stage: ACCEPTED (2026-10-27).
Two bounded expert rounds complete; paused successor-gain blocker fixed and R2 clear.
Chrome, Firefox and native Android audio continuity acceptance passed.
Cast/detached/share retain ordinary streaming by design; live Cast handoff is
out of this task's required Chrome/Firefox/emulator E2E scope.

# M21 / 0l3: gapless music queues

Stage: implementation present on web and native Android; focused browser and
native output acceptance passed. Remote receiver handoff remains unverified.
No release or version bump is claimed here. Prerequisites are
1k3 (transport), ck3 (queue), nj3 (interaction design), oj3 (audio fixtures).

## Interaction contract

Web: existing player controls → **Gapless** disclosure → **Gapless music
queue** checkbox, explanatory limits and a live status. The control uses the
existing playback disclosure style/theme. It starts off, saves the browser
preference and applies on the next queue song opened. Native: the current
audio screen gains an adaptive **Gapless music queue** switch and status below
Speed; its preference is saved on the device. Both leave the main play/seek/
track controls in place. No new shortcut or drag gesture is required.

States: Off → enabled for the next open → preparing current/next → ready →
boundary identity/progress update. A missing next song is a final-song state.
Preparation/decode/network/resource/rate/remote limits explicitly say normal
streaming/advance. Disabling clears the prepared successor while retaining
current position. Native queue edits may briefly pause/resume while replacing
an already buffered successor. A stale boundary pauses and asks the listener
to choose a track. Existing keyboard/Back/seek/lyrics controls remain the
transport controls. Light/dark, narrow screen and real keyboard/touch checks
remain in the integrated acceptance matrix.

## Decoder paths

Web uses full decoded AudioBuffers, scheduling the successor at exactly the
current buffer's end on the **same AudioContext** and EQ/ReplayGain graph as
existing playback. The stable audio transport facade delegates to either the
unchanged DOM audio element or that scheduler; no DOM property monkeypatching
or second AudioContext is used. The queue atomically commits an occurrence
(including duplicate media IDs) only while scope, source revision, successor
and repeat mode match. Boundary metadata, lyrics, artwork and reporting update
without loading another media element. Seeking/pause cancels and reschedules
the pending native audio nodes. ReplayGain changes are scheduled on the audio
clock. The graph's output still supplies visualization data.

Web scope: local Music queue, 1×, songs with known duration up to five minutes,
32 MiB encoded input and at most 128 MiB retained PCM for current plus next
(initial current buffer capped at 64 MiB). Encoded reads are bounded; PCM is
checked after decode. Decoder temporary allocation is browser owned, so this
is not a hard process-memory ceiling. Failed/late preparation uses ordinary
advance. Other rates retain the existing pitch-preserving HTML audio path.
Detached/share/Cast playback explicitly retains normal streaming. Queue cache
warming remains a separate feature; it is not described as gapless. Browser
decoder treatment of codec priming/padding was measured below for FLAC and MP3.

Native uses just_audio 0.9.46's ConcatenatingAudioSource with eager source
preparation (Android ExoPlayer), retaining the current item and one successor.
Its prepare-next callback resolves authenticated stream/quality URLs without
canceling cache transfers, changing the playing cache URI, or acquiring a new
session lease. Natural currentIndex changes synchronously move metadata,
queue occurrence and the existing lease; outgoing progress uses an explicit
duration snapshot so it cannot save the successor's position under the old
media ID. Completed children are removed. Queue edits/source/account/sleep
intents invalidate pending work. Spoken/resume items and remote playback keep
ordinary advance. Codec/device compatibility still requires real output proof.

No server protocol or schema change is needed: both paths reuse authenticated
stream/compat/profile endpoints and their existing permission/range behavior.

API references checked: [AudioBufferSourceNode.start](https://developer.mozilla.org/en-US/docs/Web/API/AudioBufferSourceNode/start),
[decodeAudioData](https://developer.mozilla.org/en-US/docs/Web/API/BaseAudioContext/decodeAudioData),
installed just_audio 0.9.46 README “Working with gapless playlists” and Android
AudioPlayer.java ConcatenatingMediaSource/offload requirements.

## Focused coherence checks

`flutter analyze --no-pub` on handler, gapless part, progress session, cache
provider, audio screen and the new native test: clean. `flutter test --no-pub
test/services/gapless_playback_test.dart test/services/audio_progress_session_test.dart
test/services/music_sequence_test.dart`: 41 passed. New native cases cover
source-free natural handoff, outgoing progress snapshot, queue reorder during
delayed preparation, account/source invalidation and disabling a buffered next
song. Service doubles are not emulator audio proof.

Node tests `gapless-transport`, `queue-prepared`, `playback-source`,
`playback-resume`, `audio-visuals`, `playback-speed` passed using
`node --experimental-default-type=module web/js/tests/<name>.test.js` from
player-server. New tests cover exact scheduled times, seek/pause rescheduling,
stale queue boundaries, delayed play cancellation, input/PCM bounds, transport
event ownership, duplicate occurrences, account/revision/repeat/order changes.

R1 gain-automation fix: `gapless-gain.test.js` drives the production graph,
controller and loudness/EQ controls through pause past the cancelled boundary
and resume with different current/next ReplayGain values. Settings preserve
the current multiplier while paused; only a scheduled successor node receives
future automation at the new deadline. This focused Node regression passes.

## Focused browser acceptance (2026-10-27)

The installed Google Chrome and Mozilla Firefox passed the focused M21 runner
`test/e2e-web/tests/music-gapless.test.ts` on a new disposable fixture. Its
three-song FLAC and MP3 sets contain one continuous 880 Hz tone cut at exact
six-second sample boundaries. An AudioWorklet captured the actual graph output
for 13 seconds per run, covering two automatic queue transitions; captures
with dropped worklet frames were rejected. The normal streaming run provides
the same browser, codec, fixture and output path as a measured comparison.

| Browser | Codec | Normal boundary silences | Gapless boundary silences | Gapless phase clicks |
| --- | --- | --- | --- | --- |
| Chrome | FLAC | 200, 319 ms | 0, 0 ms | 0 |
| Chrome | MP3 | 69, 37 ms | 0, 0 ms | 0 |
| Firefox | FLAC | 123, 91 ms | 0, 0 ms | 0 |
| Firefox | MP3 | 81, 81 ms | 0, 0 ms | 0 |

Both browsers also passed visible pause/seek, successor queue reorder, natural
metadata and outgoing progress changes, pitch-preserving 1.5× streaming
fallback, Sets source and account isolation, narrow light and desktop dark
layouts, and audible ordinary playback after injected over-limit response
headers and invalid decoder bytes. The narrow popover's Gapless checkbox was
also toggled through its visible controls in both browsers. The latter two use
controlled browser responses, not oversized media files. Evidence, WAV
captures and screenshots are in `/home/paul/tmp/codex/player-m21-evidence`.
The isolated fixture ran from
`/home/paul/tmp/codex/player-m21-18858` on port 18858 and was removed after
acceptance. Recreate it with
`python3 player-android/integration_test/support/music_fixture.py up /home/paul/tmp/codex/player-m49-server --port 18858 --no-doubles --dir /home/paul/tmp/codex/player-m21-18858 --evidence /home/paul/tmp/codex/player-m21-evidence`. Run the focused suite
with `PLAYER_MUSIC_FIXTURE` pointing there, `GOMAXPROCS=2`, one Playwright worker,
and each `--project=chrome` and `--project=firefox` serially.

## Focused Android acceptance (2026-10-27)

The native x86_64 release APK (`0.8.2+4022`) ran on the API 34 Google APIs
emulator (`33.1.24.0`) against the disposable Music fixture on port 18846.
One emulator ran at a time. `adb reverse tcp:18846 tcp:18846` provided the
local server. The app was signed in through its real UI, and playlists were
opened and played through native controls. QEMU's WAV backend captured the
device's actual 44.1 kHz stereo output. Evidence is in
`/tmp/player-m21-native` outside the repository.

Both three-song FLAC and MP3 playlists naturally advanced across two
six-second boundaries while the media session remained PLAYING and progress
for the outgoing IDs reached completion. The FLAC trace had one 10 ms quiet
frame away from either boundary; the MP3 trace had no measured silence. A
seek to about 4.35 seconds before the next MP3 transition and removal of the
prepared middle successor before the first-to-third transition likewise had
no detected silence. With Gapless switched off for the same edited queue,
the native output contained an 802 ms silence between songs. The enabled
control read “Gapless ready · native decoder playlist”; the disabled control
read “Gapless off.” Light and dark player screenshots were captured.

The QEMU trace contains several isolated sample discontinuities away from
track boundaries. Those cannot establish a click-free physical output route;
the browser AudioWorklet captures provide the stronger click measurement.
The native comparison establishes the intended no-pause behavior on this
emulator. Opening a song through wrapping folder traversal correctly used
normal advance, and an unknown network with Mobile prefetch off visibly
reported preparation unavailable. Enabling that policy changed the paused
playlist to ready. Disabling it again removed the prepared successor while
the current song stayed PAUSED at the same 334 ms position.

A disposable 12-entry MP3 playlist kept music active past the native sleep
timer's shortest one-minute setting. The timer expired during song ten at
3.099 seconds; the media session remained PAUSED at that same position in
five subsequent samples through 67 seconds after starting. The UI read
“Sleep timer ended · paused.” The saved session timeline is
`/tmp/player-m21-native/sleep-boundary.json`.

With the three-song MP3 playlist prepared and PAUSED at 334 ms, logging out
through Settings changed the Android media session to NONE. A hardware media
PLAY command did not restart it. Signing into the fixture's second account
left the session at NONE and its visible Now Playing queue empty; another
media PLAY command still could not revive the first account's music. The
second account's empty queue screenshot is
`/tmp/player-m21-native/account-b-empty-queue.png`.

The focused browser runner passed 3/3 in Chrome `144.0.7559.132` and 3/3 in
Firefox `156.0`. `GOMAXPROCS=2 go test -p 2 ./... -race -cover` passed while
the emulator was stopped. Native source policy, sleep and account boundaries
passed. Cast/detached/share paths keep ordinary streaming by design (see Decoder
paths). Required Chrome/Firefox/emulator gapless measurement is complete.
The shared ten-task full regression gate passed before this task and is next
due after twenty completed tasks, as requested by the user.
