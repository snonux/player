Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
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
