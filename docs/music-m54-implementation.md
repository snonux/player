# M54 rich music Media Session controls

Task qk3 extends existing metadata, sequencing, collections, queue and recovery.
Root approved this concrete design before product implementation (2026-10-22).
No release/version/deployment work is included.

## Layout and interaction

Web floating/full player: existing cover/visuals, then a compact title line and
artist · album line, followed by the unchanged transport and seek controls.
Minimized Restore bar: small cover, one ellipsized combined title/artist/album
line, and the existing Restore action.
Long text truncates visually with complete accessible labels/tooltips. Empty
artist is “Unknown artist”; missing album is omitted. Filename stem remains the
untagged title fallback. Existing typography, spacing and theme tokens apply.
No shortcuts change: N/P change tracks, h/l and arrows seek; typing and focus
continue through the established keyboard modules.

Android full player: existing artwork, title and artist · album, existing seek
slider and track transport; separate labeled ±15-second actions remain. A shared
Music mini bar on library screens uses 48dp artwork, two text lines, Play/Pause
and Next. It exists only for an actual active Music source, never for a retained
queue while another kind of source owns playback. Text/art opens the current
full player by attaching to the existing handler/lease, without loading a source,
claiming another session or changing position. An obsolete attachment fails
safely. Back returns to the library without stopping the handler. Controls have
48dp targets, full semantic names and existing native theme colors.

## State and ownership

Initial load, manual track changes, natural completion and queue recovery use
the same metadata model for visible full/mini surfaces and an active OS session.
An inactive OS session is subject to the platform preconditions below.
Durable queue snapshots retain identifiers; recovery resolves fresh permitted
Media metadata for the matching unlocked origin/account. Compatibility URI
replacement preserves metadata. Downloaded snapshots supply known text without
requiring online requests; device files retain filename fallback.

Artwork is source/account owned. A source change clears the previous image
immediately; an asynchronous image result must match the source epoch and
current owner. Native protected images become private cached files for Android
MediaItem artUri; credential lookup uses the matching account/origin gate.
Browser artwork is fetched with same-origin credentials and supplied through a
browser-owned decoded blob, so OS clients receive the browser's exported cached
file rather than an unusable protected HTTP URL. Failed/missing artwork falls
back to the existing neutral artwork without blocking audio or showing old art.

The visible player shows the new identity while preparing and retains that
identity with existing error/Retry feedback after failure. OS metadata can be
inactive during preparation or failure, as observed below. Stop, close, signout,
image source and source takeover clear obsolete session metadata/art. No
permission prompt is added. Offline failures use existing retry feedback.

The actual local playback window owns browser MediaSession. Main window
relinquishes ownership while detached or casting. Detached popup publishes its
own identity and forwards system track actions through existing opener protocol;
seek remains a separate session action. Cast receiver metadata gets existing
supported title/artist/album/artwork payload fields; local ghost playback is
cleared while the receiver owns audio. Physical receiver verification remains an
explicit boundary; this task does not add a Cast feature.

## Verification contract

Capability preflight uses generated media and task-only profiles/sink outside
git. Headful installed Google Chrome 144 and Mozilla Firefox 156 export genuine
MPRIS metadata and receive Pause/Play/Next/Previous on this GNOME session bus.
Each bus owner Unix PID is checked against the launched isolated profile before
sending a command. No shared player or desktop screenshot is used.

Final acceptance requires fresh disposable Player fixture and actual source/tone
and media-ID progress evidence after MPRIS transport, plus title/artist/album and
OS-readable, decoded mpris:artUrl artwork. JavaScript MediaSession queries or
mock handlers alone do not count. Native API34 acceptance uses actual
notification/keyguard controls, screenshots and dumpsys MediaController/session
metadata, including artwork and all four transport actions. Capture light, dark,
narrow and long text, keyboard focus/typing, native touch/Back, missing art,
source/account races and restart/recovery. Preserve logs and failed attempts
outside git. Root owns heavy builds/full suites, review launches and final task
completion; any required platform gap keeps qk3 unaccepted.

## Observed Chrome paused-return constraint and supported flow

Real Chrome 144 acceptance found that closing a paused detached window and
loading its paused track in the main window does not restore the main window's
OS audio focus. The main element was ready, paused at the correct position,
with correct JavaScript metadata/artwork, while MPRIS stayed Stopped with no
track/title/artist for 15 seconds. OS Play alone could not reach the main
handler. This failed OS-only boundary is retained in qk3 attempt5 evidence;
it is not reported as passing or generalized to every Chrome version.

The supported paused return keeps playback paused and uses the existing status
and Play controls: “Playback returned here. Press Play to resume.” The hint
waits behind compatibility preparation and clears on actual playing, new source,
close or error. There is no artificial play/pause, autoplay or focus jump.
Chrome's explicit visible Play is the observed activation precondition before
OS controls resume. Firefox also supports resuming the paused return directly
from OS controls. Actual OS metadata/artwork/transport are verified separately
from the JavaScript diagnostic state.

This follows the UA audio-focus selection boundary described by Chromium's
[media playback control design](https://chromium.googlesource.com/chromium/src/+/HEAD/services/media_session/controlling_media_playback.md)
and W3C Media Session active-session selection; setting declared paused state
is not an API for claiming OS audio focus.

A second actual Chrome 144 boundary appears when OS Next selects a new source
while the browser is offline: the visible player retains the failed target and
error, but the OS session becomes Stopped with default artwork and no music
metadata. It does not expose the previous track as the failed target. The
existing visible Play retries after reconnect and reacquires focus; real OS
metadata and transport are then verified. Failed preactivation OS metadata and
OS-only retry are not claimed to work in that observed Chrome environment.
Firefox's actual failed-target metadata and OS Play retry are verified through
its retained active session. These are explicit supported flows, not artificial
play/pause or hidden playback used to manufacture OS focus.

## Final verification record, 2026-10-22

Scoped acceptance completed on installed Chrome 144, Firefox 156 and the native
API34 x86_64 emulator. Each platform exercised actual OS metadata and artwork,
Previous, Next, Play and Pause against real Player media. This statement includes
the supported activation and retry flows described above; it does not mean all
attempts passed. Failed attempts and their original evidence were retained.
No physical Cast receiver or headset was exercised. Sender payload/ownership
checks passed, but do not establish physical receiver or headset behavior.

Two fresh review rounds completed. Round one found three web integration
blockers: Cast handback did not restore the local session, OS Pause did not
cancel pending autoplay, and OS Play bypassed failed-source retry. Production
wiring tests reproduced them before repair. Round two found no blocking issue.
The later paused-return hint received root self-review and targeted/full web
checks; it did not change Android product code.

The serialized full gates passed: `flutter analyze --no-pub` reported zero
issues; `flutter test --no-pub --concurrency=1 --reporter expanded` passed 1,402
tests in 227 seconds. Server `go build ./...`, `go vet ./...`, and
`GOMAXPROCS=2 PLAYER_REQUIRE_FFMPEG=1 go test -race -cover -count=1 ./...`
passed, with the required FFmpeg integration coverage enabled for the 261-second Go test run. `mage webTest` passed and was
repeated successfully after the paused-return hint. Errcheck retained 256
existing diagnostics, with zero new or resolved diagnostics; it was not clean.
Exact commands/timings are in `/home/paul/tmp/qk3-evidence/full-gates-results.json`.

Normal builds passed: `go build -o /home/paul/tmp/qk3-evidence/player-final
./cmd/player` and `flutter build apk --release --target-platform android-x64
--split-per-abi`. The final web-only hint server was rebuilt as
`/home/paul/tmp/qk3-evidence/player-paused-return-final`. Both components remain
0.8.2; Android is 0.8.2+22, version code 4022. The final x86_64 APK SHA256 is
`db92e0d82b055ab4ebb38d04e1ae95bbb8a9a94d529e049f18059c471541bc2a`.
Root also matched the installed base APK to that hash. No release was made.

Browser acceptance used owned headful profiles and a dedicated audio sink.
Chrome attempt 1 and Firefox attempt 3 established normal all-four transport,
real distinct source tones, fresh per-ID saves and decoded blue/orange OS art.
Chrome attempt 7 established supported paused return after explicit visible
Play, all-four OS controls after activation, and routed 403 artwork fallback.
Firefox attempt 8 established detached ownership, OS-only paused-return resume,
strengthened routed 403 fallback and actual offline OS-only retry. Chrome
attempt 9 completed the remaining offline/clear phase: stopped OS session with
no previous identity while offline, visible Play after reconnect, actual new
metadata/artwork/audio/progress, real OS controls, and logout clearing.

The browser logs are `web-actual-attempt*.log` beneath
`/home/paul/tmp/qk3-evidence`; original screenshots, exported OS art, PCM and
metadata remain in `actual-attempt*/chrome` or `actual-attempt*/firefox`.
Retained failures include the initial fixture's 12-second audio despite a
45-second manifest (corrected only in a fresh fixture), popup-close driver
acknowledgement, progress sampled before its timer fired, and the explicit
Chrome OS-only activation limitations. The current fixture's actual A/B/C
sources were verified with ffprobe at 45 seconds. The denied-art check is an
injected HTTP 403, not a claim of real ACL revocation.

Native acceptance is indexed in
`/home/paul/tmp/qk3-evidence/m54-native-acceptance-index.json`. Attempt 2 proved
full/mini attachment without loading/resetting the existing source and actual
OS artwork. Attempt 4 proved every visible notification and keyguard transport
control, distinct artist/album metadata, 600-pixel OS bitmaps, measured 415/440 Hz
audio, and fresh reset per-ID progress. Attempt 5 proved light/dark mini contexts
and untagged filename/Unknown artist with absent album and artwork. The full
audio player retains its established black presentation in both theme contexts.
Attempt 6 completed real reverse-cutoff failure, visible Retry recovery with
orange artwork and 415.28 Hz audio, fresh media ID 39 progress at five seconds,
recovered system Play/Pause, and logout with no active OS music session.

The native failed target was identified by the visible full-player error and
Retry while the reverse tunnel remained removed. The OS session was inactive,
so no previous track or artwork remained. Reconnecting alone was not counted as
recovery: the existing visible Retry loaded the target, then real OS metadata
and controls were checked. System Play while failed was not used as a recovery
path; existing native Retry semantics remain unchanged.

Native failures were retained: standalone probe initialization required the
real Android media-service manager; animated system cards prevented idle UI
dumps; an old shared driver read stale app XML; a standalone-label assertion
missed merged Flutter semantics; and an early outage assertion observed only
preparation. The qk3 runner now requires fresh SystemUI XML or an individually
inspected current screenshot plus recorded coordinates. Shared driver repair is
separate pending task `bo3`. None of those failed phases is reported as passing.

Component E2E READMEs contain runnable commands, ownership requirements and
bounded resume flags. Product and unit tests were frozen before final device
acceptance; subsequent changes affected only diagnostic runners and records.

Cleanup completed after root independently inspected the accepted originals.
Only verified task server PID 1919295, emulator PID 1926303 and Pulse module
536870916 were stopped/unloaded after identity checks. All recorded task browser
roots and earlier task servers were absent; port 18812 was free, adb had no
devices, and the task sink/module was absent while shared Pulse still responded.
Both fixture directories, databases, media, authentication files, APKs and all
evidence were preserved. External `stop-only-results.json` and
`independent-stop-audit.json` record the cleanup and independent audit.
