# M65 share encoding and bitrate (9m3)

Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Round 1 found no blocking issues; no second review round warranted.

Design recorded before coding. Extend M62's existing share dialog and My Shares
editor, using U01 typography, spacing and theme tokens. A disclosure labeled
**Audio quality** contains the same vocabulary as private playback: Auto
(compatibility when needed), Original (no conversion), Balanced AAC 128 kbps,
High AAC 256 kbps, Compatible MP3 192 kbps, Data saver Opus 96 kbps. The note
explains that the choice applies to shared playback and downloads, bitrates
are nominal targets, and Original needs a compatible decoder. Existing
expiry/viewing fields and Create/Save/Cancel remain primary. No new shortcut.
Native uses a labeled dropdown inside the existing scrollable dialog, with
ordinary picker/Back behavior and Material theme colors. Web uses native
selects, existing modal focus restoration, Tab and Escape behavior. Collection
snapshot text and existing copy/revoke flows remain unchanged.

States: selected policy is retained on inline create/save errors and retry;
busy controls disable during submit. Offline/permission errors leave the form
open. Unknown policy is rejected by the service, never silently converted to
Original. Guest pages and My Shares display the saved quality with their
existing metadata. Long labels wrap; controls fit narrow layouts, and native
content scrolls. Dark/light contrast inherits existing tokens. Actual visual,
keyboard and touch/Back acceptance remains pending integrated verification.

Contract: `audio_quality` is empty for legacy Auto, `original`, or an M50
allowlisted profile (`aac128`, `aac256`, `mp3192`, `opus96`). Create persists it
atomically; PATCH omission preserves it. Explicit policies apply only to audio.
All public stream/download/compat routes and collection aliases reload stored
policy, ignoring anonymous profile overrides. Explicit Original refuses audio
compatibility conversion; forced encodes use M50's bounded cache and anonymous
requester budget. Downloads use the rendition MIME and extension. Existing
viewing credentials, HEAD, expiry/revoke and collection creator ACL checks stay
in force. Transcoding failures never fall back to original bytes. Collection
download remains per item. Existing video and image shares keep their policy.

Pending integrated acceptance: Chrome and Firefox/native create/edit original
and each profile, inspect actual format/nominal bitrate and saved policy;
forged profile/quality queries across every alias, signed viewing URLs,
HEAD/Range/seek, expired/revoked/lost ACL, encoder failure, legacy omission,
light/dark/narrow and keyboard/touch/Back. No runtime acceptance is claimed.

Focused implementation verification (2026-10-25; source based on `ec67810`):

- `GOMAXPROCS=2 go test -p 1 ./... -run '^$'` compiles all Go packages and
  interface implementers. No regression tests are implied by that command.
- From player-server: `GOMAXPROCS=2 go vet ./internal/api ./internal/repository
  ./internal/service` passes. `GOMAXPROCS=2 go test -p 1 ./internal/api
  ./internal/repository ./internal/service -run
  'TestShareQuality|TestShare|TestShared|TestCompat' -count=1` passes. The new
  real-SQLite/HTTP route test uses a deterministic rendition provider, covering
  all explicit create/PATCH policies, omitted edit/reset, single and collection
  aliases, signed URLs, forged overrides, HEAD/Range, viewing counts, preparing
  and failed conversion, MIME/extension, active-policy change, ACL, expiry and
  revocation. It does not prove actual ffmpeg bytes or device playback.
- The upgrade test reproduces current M62 nullable `media_id`/ordered `media_ids`
  storage without `audio_quality`, reopens it, and preserves original and
  collection shares, repeated items, descriptions, expiry, counts and signing
  key. Older pre-M62 migration tests also pass.
- Node `share-quality`, `share-edit`, `share-collection-player`, `stream-quality`
  and `playback-source` tests pass. Syntax checks and `git diff --check` pass.
- From player-android: scoped `flutter analyze --no-pub` of the ten changed/new
  implementation paths and two share-dialog tests is clean. Focused
  `flutter test --no-pub test/screens/create_share_dialog_test.dart
  test/screens/edit_share_dialog_test.dart` passed 17 tests before the final
  editor-choice case was added; its editor-only rerun passes all 3 (18 distinct
  tests across the current two files). Controls are exercised through actual
  Flutter dropdowns, including create failure retention and editing a saved
  profile absent from a refreshed list. These are widget tests, not emulator
  acceptance.
- Scoped `GOMAXPROCS=2 errcheck ./internal/api ./internal/service
  ./internal/repository` is not clean: 246 existing unchecked-error diagnostic
  lines, retained at `/tmp/player-m65-errcheck.txt`; no new M65 diagnostic.
  Existing `rows.Close` calls in share.go are among them. No unrelated cleanup.

Initial focused-check corrections: new route fixture needed the normal seven-day
expiry configuration; Original `/compat` uses HTTP 400 rather than the test's
initial 409 expectation. A broader compat check exposed changed legacy error
text; the implementation now confines actionable encoder errors to explicitly
selected share conversions. All affected focused checks pass after correction.

Current-ACL checks for explicit single-file policies are additive; empty legacy
single-file policies retain their prior access semantics. Conversion completion
rechecks the stored policy and creator access before exposing prepared bytes.

Full suites, final server/APK builds, real Chrome/Firefox
and native Android codec/bitrate/output/seek, visual and keyboard/Back acceptance
are still pending. Existing M50 evidence is prerequisite context, not proof for
M65. Live servers/emulator, versions, .serena/, reports/ and research_notes/
were untouched. Parent owns review, commit and eventual acceptance/closure.

Round 1 advisory follow-up: configuration in the shared single-song/collection
creation dialog now locks expiry, maximum viewings, quality and profile reload
while the request is pending and after success. Close and Copy remain usable;
the readonly link stays selectable. A failed request restores configuration and
Submit without discarding the chosen policy. Late profile responses preserve
both the selected policy and disabled state. No other form was changed.
`node player-server/web/js/tests/collection-share.test.js` passes the pending,
success, failure and late-profile-response cases for both creation endpoints;
`share-quality.test.js`, syntax and diff checks also pass. Parent inspects this
small advisory fix without launching a second review round. Actual platform
acceptance remains pending.
