Stage: integrated platform acceptance passed; final verifier/documentation commit
and task closure remain parent-owned.
Two bounded expert reviews completed; both R1 native blockers fixed, R2 clear.

# M50 selectable streaming quality

Design: extend the existing audio controls with a labeled Quality native select
(web) and labeled Quality button opening a native bottom sheet (Android). Keep
current theme typography, spacing and focus behavior; introduce no shortcuts.
Auto keeps existing compatibility policy; Original requests the original stream
and reports decoder failure normally, without silently converting. Named choices
include codec/bitrate for clarity. A change is saved for this browser or this app
server profile and applies on the next opened track, preserving active playback
and position. The panel explains this transition and the independent policies
for local files, offline downloads, public shares, and Cast. Narrow layouts use
existing controls wrapping; native content scrolls and Back closes the sheet.

Loading: profile list status is announced; failure keeps Auto/Original usable
with a retry action. Unsupported saved profiles remain visible and may fail
explicitly; selecting Auto recovers. Storage failures are visible. Existing
conversion readiness/retry/error controls cover preparing, offline, auth/ACL,
space exhaustion and unavailable encoders. No conversion is represented as
Original. These are intended light/dark/narrow states, pending actual review.

Implementation uses bounded server profiles through the authenticated compat
endpoint, retaining existing Range/ETag, cache budgets, requester limits and
source-root access checks. Explicit encodes always re-encode at the chosen
bitrate; profile keys separate cached bytes.

Verification stage: implementation first under the October 22 user update.
Actual Chrome/Firefox/native emulator journeys, light/dark/narrow captures,
codec/bitrate response proof, seek, offline, unsupported-profile recovery and
full regression/build gates remain deferred to integrated verification.

Focused implementation checks: new Node routing/disclosure keyboard checks
pass, as does the existing playback-source test file. Targeted Go profile
cache coexistence/budget/eviction and compatibility service checks pass;
API compile initially met a concurrent ratings implementation gap and needs
rerun. Scoped Dart analysis initially reported five brace-style infos, repaired
before final focused recheck. No platform acceptance is implied.

Review round 1 identified two native blockers: Cast inferred conversion from
an old source URI plus current preference, and prebuilt queue entries pinned
old quality URLs. Cast now always asks underlying media metadata; both live
sequence and restored queue loaders resolve the current preference immediately
before opening each track and recheck source ownership after the awaited
lookup. Existing current playback remains uninterrupted. Focused regressions
exercise old source snapshots and Original → Opus → Auto between openings.

Final focused checks after review fixes: `flutter analyze --no-pub` on ten
changed native source/test paths: clean; `flutter test --no-pub
 test/services/playback_source_resolver_test.dart
 test/services/cast/cast_controller_test.dart --concurrency=1 --reporter=expanded`:
19 pass. A malformed flat Cast test response was corrected to the real
`{media:{...}}` API envelope before that passing run. `GOMAXPROCS=2 go test
-p 1 ./internal/transcode ./internal/service ./internal/api -run
'TestSelectedQuality|TestHandleCompatStream|TestCompatStream' -short`: all three
packages pass, including profile cache and encoder arguments. These targeted
checks do not verify actual codecs or platform runtime acceptance.

## Integrated acceptance, 2026-10-24

The new disposable fixture `/home/paul/tmp/player-quality-fixture` was generated
with a fresh database and scanned absolute media paths on free port 18771:

```sh
python3 player-android/integration_test/support/music_fixture.py up player-server/player \
  --port 18771 --no-doubles --dir /home/paul/tmp/player-quality-fixture \
  --evidence /home/paul/tmp/player-quality-evidence
```

It uses server 0.8.2 built from c7b6aea-dirty, containing current Go product code;
the acceptance checkout started at 87b5a2a. Shared fixture 18766 was not restarted
or modified. Installed Chrome 144.0.7559.132 and Firefox 156 each passed all five
quality choices. The actual player blobs matched the authenticated selected
response bytes, ffprobe identified their codecs/containers, exact 206 byte ranges
matched the complete responses, visible seek advanced to the middle of the song,
and captured output was audible. Select typing/focus, Escape and real p Pause/
Resume worked; changing a preference preserved the current source until reopen.

The deterministic Three Tones input produced these actual muxed rates in both
browsers. These are measurements, not promised nominal target rates or ceilings:

| Choice | Codec/container | Channels | Actual kbps |
| --- | --- | ---: | ---: |
| Original | FLAC | 1 | 176.9 |
| AAC128 | AAC/MP4 | 2 | 130.4 |
| AAC256 | AAC/MP4 | 2 | 222.0 |
| MP3192 | MP3 | 2 | 193.2 |
| Opus96 | Opus/Ogg | 2 | 187.5 |

The tonal input makes VBR particularly different from nominal targets. Separate
Loud-source API checks measured FLAC 145.8, AAC128 130.4, AAC256 217.7, MP3192 193.3
and Opus96 148.4 kbps. Both API prefixes, HEAD lengths, prefix/suffix/middle ranges,
ETag 304, unknown-profile 400, image 415, auth 401 and private warmed-rendition ACL denial
passed; details are in external `api/results.json` and `api.log`.

Browser boundaries passed saved defaults across reload and account changes,
actual profile-list transport failure with Original/reload recovery, narrow
light/dark controls, and real browser-offline buffered output while changing
quality without replacing the active source. The narrow player uses its existing
More player controls popover; clicking outside dismisses it normally.

The API 34 x86_64 native app 0.8.2/versionCode 4022, source 369243f, installed SHA256
`acbab6c02fda8e3f3aeabbb9fecffbe9f7eeca370c9f3c37245ea9b873e21c63`, passed
all five profiles through native touch controls and existing-queue Next/Previous.
A transparent local proxy captured sanitized actual rendition requests. Real
QEMU WAV output contained the three expected bands for 3.5–3.8 seconds; each
profile then sought to approximately 6000 ms and produced further audible output.
The live OS media position is a sparse anchor, so assertions use actual PCM and
the settled Pause position. Native light/dark/Back, removed reverse mapping,
unavailable profile list with usable Auto, buffered offline playback, and restored
mapping/reload passed. No preferences were written by the driver.

A real missing encoder was exercised by restarting only the owned 18771 fixture
with an empty rendition cache and a server-only PATH wrapper that forwards real
ffmpeg while replacing libopus with a nonexistent encoder. The real ffmpeg exit 8
and server 500 for Harbour are retained in the server log. Chrome/Firefox both
showed the failure and recovered audible original playback after choosing
Original and reopening. The ordinary server configuration must be restored after
this scenario using the owned external `worker_restart.py normal` helper.

This boundary exposed an Android defect: its error body hid the only Quality
control, leaving Retry against the failed selected profile. The focused repair
keeps the existing server-only Quality sheet available in a centered, scrollable
error body, and refreshes a changed quality before generic Retry. Existing queue
Retry already resolves quality when opening. Local/download/share/Cast source
policies remain separate. The refreshed same-widget request remains the retry
base after a transient load failure; stale generations cannot consume the
refresh, and route changes clear it. The same guarded refresh handles a failed
local return from Cast/renderer playback and retains its queue/index/occurrence.
Widget coverage proves changed-quality
Original Retry uses `/stream` even when its first load fails and Retry is tapped
again, Retry/Quality remain reachable at 300 px height, and local/public
share failures omit the server Quality control. The full 56-case audio-screen
file passes, plus isolated public-share guard and focused analysis with no issues.
The renderer-return widget regression also proves Original recovery preserves
the existing occurrence, index and paused return offset; physical receivers
are not claimed by that regression.

The focused recovery fix is committed as `c2650c1`. Final full Flutter analysis
passed with no issues, all 1676 tests passed in 2m56s, and the normal release
x86_64 APK built in 63.8s. The installed APK SHA256 is
`8bcb951e4bde90da6e77bbe8af258d21da4a057a7cbce2f806748b8b7e6d59ca`.
It was installed with `adb install -r`, without clearing data. The real Opus
failure was reproduced after explicit saved-server unlock, proving the chosen
profile survived app replacement and cold session recreation.

On that final APK, both Original and Auto recovery passed independently. Each
native trace records `/media/5/compat?profile=opus96` returning actual HTTP 500,
then `/media/5/stream` succeeding after Quality and Retry. Original produced
3.08 seconds of the expected Harbour tone (659.2 Hz measured), and another
1.70 seconds after seeking to 6000 ms. Auto produced 3.31 seconds (659.2 Hz),
then 1.72 seconds after the same real seek. Final native evidence directories
are `native-fixed-{original,auto}-recovery`; `native-fixed-versions.json` records
installed provenance. Full gate logs are `full-analyze-recovery.log`,
`full-flutter-recovery.log` and `apk-recovery-build.log`.

The normal server configuration is restored: owned PID 3190653, start tick
67960192, fault disabled, with a successful normal Opus response independently
ffprobed. Original port 18766 admin, Library selection, EQ off/flat, loudness
Off/0/0, unchanged quality defaults and exact three-occurrence queue are restored.
The queue remains `[(3,3),(1,3),(2,5)]`, index 2, position 4.69, canonical
`[3,1,2]`, repeat Off and shuffle false. Scoped saved state matches the snapshot
exactly; no preferences were edited and no CAS repair was necessary.
The OS session is NONE, with stale Harbour metadata; that metadata does not
identify a loaded source. Only the original reverse mapping for 18766 remains.
Restoration evidence is `original-shared-scope-restored.json` and
`native-restored-library.png`.

The two feature-review rounds remain used; the actual runtime defect received
focused parent inspection without another full feature review. No version bump,
release, push or deployment is involved.

External evidence retains failed attempts: initial blob-URL assumptions, native
select-popup Escape handling, reload player occlusion, compact-popover dismissal,
dynamic keyboard-access summary labels, sparse native position anchors, and the
real missing recovery control. Final browser logs are `browser-core-keyboard-final`,
`browser-boundaries-attempt5`, `browser-offline-attempt1`, and
`browser-encoder-attempt3`. Native positive results are `native-aac128-attempt1`,
`native-{aac256,mp3192,opus96,original}-final`, and `native-boundaries-attempt1`.
