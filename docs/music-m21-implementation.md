Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; paused successor-gain blocker fixed and R2 clear.
Actual audio continuity/codec/output acceptance has not run.

# M21 / 0l3: gapless music queues

Stage: implementation present on web and native Android; fresh review and
integrated verification pending. No release, version bump, build or actual
Chrome/Firefox/emulator audio acceptance is claimed here. Prerequisites are
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
decoder treatment of codec priming/padding still needs measured acceptance.

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

Full regression gates/final server+APK builds and actual continuous adjoining
sample captures on installed Chrome, Firefox and Android are deferred by the
implementation-first instruction. Integrated acceptance must measure gap/click
and audible before/after tones, including seek and queue edits; verify metadata/
artwork/progress timing, controls and source/account/sleep cancellation; exercise
resource/decode/network/offline and remote/rate fallbacks. No task acceptance
until those platform checks pass.
