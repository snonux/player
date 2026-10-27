# Optional provider and retained lyrics (dm3 / A03c)

Implementation and two bounded reviews are complete. Final R2 cleared the
provider revalidation, offline caller and source/backing-copy fixes below.
Integrated Chrome/Firefox/native acceptance passed on 2026-10-27 (see below).
Uses accepted nj3/U01, oj3/V01, vj3/A03a and reviewed tl3/M49 code. Rich A03b
data remains intact. Existing local parsers and offline managers own their
established responsibilities.

## Interaction contract (before coding)

```text
Existing player Lyrics region / native expandable Lyrics panel
Source: Local / Embedded / Provider · Saved with download when available
[Find lyrics online]     only configured provider, explicit signed-in action
[Retry lookup] [Remove provider lyrics]     quiet result/error state
Existing language chooser, readable wrapping rows and tap/Enter-to-seek
Offline: Saved lyrics / No saved lyrics; no external lookup actions
```

The action explicitly sends track tags to the operator-configured provider.
Use existing button, text, focus, spacing, theme tokens and Material controls.
No new global shortcut. Browser native button Enter and existing j/k reading,
Space/p transport, Escape hierarchy and language focus stay intact. Android
uses labeled controls and ordinary Back. Loading disables duplicate lookup;
empty/error/permission/offline messages stay inside Lyrics and never block audio.
Local/embedded sources always win. Provider removal is a small reversible cache
action; lookup can restore it. Long text wraps in existing bounded lyric panels.
Light/dark/narrow and actual keyboard/touch results await integrated acceptance.

## Provider, cache and offline contract

Default off. An operator selects one HTTP(S) JSON agent endpoint; callers cannot
select destinations or paths. Only explicit authenticated POST launches lookup,
after media ACL checks. GET reads local/cache only. Public shares remain local.
The provider receives existing track tags, never Player credentials or paths.
Requests have timeout, concurrency and response limits and refuse redirects.
Response is the existing full rich lyric JSON; 204 means no match. Local data is
checked again before returning fetched data. Cache belongs to user/media/provider
and current source content identity; replacement cannot reuse an old result.

Web pin and playback-cache manifests and native download DB/cache index retain
the optional full lyric model alongside completed audio, including words,
languages, translations and voices. Existing account ownership, removal, cache
eviction and saved-source validation apply. Missing/bad lyrics never invalidate
valid audio. No provider lookup occurs during ordinary download/cache/playback.
Existing ZIP export is media export; this task concerns actual in-app offline
playback, and does not add unrelated archive functionality.

Final storage layout keeps bodies lazy: web stores a separate CacheStorage
response referenced by `lyricsKey`/`lyricsSize`; native playback cache uses a
generated `.lyrics` file plus byte count; native downloads use an optional DB
column excluded from ordinary metadata queries. Cache budgets include lyric
bytes; audio file-integrity sizes stay audio-only. Metadata-only eligibility,
collection title and already-downloaded saves preserve existing lyric bodies.
New copies snapshot available lyrics; existing copies keep their original
snapshot, so fetch desired provider lyrics before saving new audio. Removing
the server provider cache does not rewrite completed offline copies.

Provider cache is SQLite account/media-owned, at most 128 × 1 MiB entries per
account with 30-day expiry/pruning and user/media deletion cleanup. Identity
includes source SHA-256, tags and provider configuration; bounded source reads
(512 MiB / ten-second whole operation budget) use at most 128 memoized stat/ctime
checkpoints. Unfetched GET skips hashing. Provider requests have eight-second
HTTP timeout, 1 MiB response bound, no redirects and one concurrent operation.
All access is rechecked before the API returns after lookup. Sources beyond the
hash bound retain local lyrics/audio and cannot use optional provider lookup.

## Integrated acceptance (2026-10-27)

Deterministic lyrics provider on `127.0.0.1:18912` with mode/call logs under
`/home/paul/tmp/codex/`. Disposable fixture at `127.0.0.1:18850` with
`LYRICS_PROVIDER_URL` set; provider-only track `Provider Sky`. Server 0.8.2 /
build `8691244…-dirty`, APK 0.8.2+4022 SHA256
`58d7048ed0ce2c1388141f111688eea6299b5ed10b7313b4de3dfa33a5661f56`. Commands in
the component E2E READMEs. Evidence under `/home/paul/tmp/codex/player-a03c-evidence`.

| Platform | Result | Evidence |
| --- | --- | --- |
| Installed Chrome 144.0.7559.132 | PASS: explicit lookup (no GET tags), cache/error/remove, translation, seek + 440 Hz audio, pin, cold offline rich lyrics, other-account unfetched, narrow light/dark | `1793113392036-538882/chrome/evidence.jsonl` |
| Installed Firefox 156.0 | PASS: same journey | `1793113426462-539321/firefox/evidence.jsonl` |
| Native API34 x86_64 (`Player_Integrated_A_API34`, QEMU wav) | PASS 8: touch lookup/translation/error/remove/retry, album pin, cold offline 440 Hz + French, Back | `native-1793115406305975941/android/evidence.jsonl` |

Offline native audio measured 440 Hz dominant (`bands_dbfs` 440: −12.27 vs 523.25:
−48.89). Screenshots of provider found/French and offline French were reviewed.

## Focused implementation verification / review freeze

Passed:

- `cd player-server && go test ./... -run '^$'` (compile-only all packages).
- `go test ./internal/service ./internal/app ./internal/repository -run 'TestProviderLyrics' -count=1`
  (final service 0.009s, wired app 2.462s, repository 1.967s). Tests use local
  deterministic HTTP fixtures, not real providers. Covers explicit/no-GET egress,
  redirects/bounds/malformed timing, credential isolation, account permissions,
  revocation during lookup, source replacement preserving size/mtime, tag/config
  identity, local priority during lookup, errors/no-match/remove, lazy persistence,
  source bounds/cancellation, expiry/count and user/media deletion cleanup.
- `go vet ./internal/api ./internal/repository` passed. Initial service vet found
  base-HEAD copied-lock diagnostics in `scan.go:90` and
  `scan_automation_test.go:91`. The parent corrected those two scan lines in
  `36f35e2`; service vet and the filtered scan race tests then passed (1.028s).
- `errcheck ./internal/service ./internal/api ./internal/repository` ran and exits
  1 on 245 existing diagnostic lines (`/tmp/dm3-errcheck.log`); no diagnostic in
  the lyric implementation/new tests. No claim that repository-wide errcheck passes.
- `node player-server/web/js/tests/{lyrics,saved-lyrics,offline-pins,offline-browse}.test.js`
  run as four individual Node commands; all pass. Includes explicit GET/POST/DELETE
  controls/focus, lazy rich bodies, metadata pointer preservation, account scope,
  offline reads, audio-only integrity sizes and lyric-byte budget eviction/cleanup.
- Flutter targeted analysis of the nine changed production Dart files plus the
  three changed test files: no issues (3.7s). `dart format` on changed Dart files.
- `cd player-android && flutter test test/services/offline_downloads_test.dart test/services/playback_cache_test.dart test/lyrics_test.dart test/widgets/lyrics_panel_test.dart`
  passes all 47 tests. Covers full rich model, new download/cache snapshot HTTP,
  restart/offline seek state/account guards, lazy bodies and byte eviction,
  M57 eligibility and duplicate-save preservation, optional failure retaining
  audio, and explicit provider versus saved/offline panel actions.
- Go formatting/imports and `git diff --check` pass. Service-worker shell cache
  v82 includes the new saved-lyrics module for cold offline restart.

During implementation checks, fixed an initial duplicate JS `size` identifier
and five small Dart style diagnostics; the final affected checks above pass.
No full suites, final binaries/APKs, Chrome/Firefox/native runtime or external
provider acceptance were run. No live server/emulator mutation, reviews, task
status updates, commit, release, version bump, push or deployment by this worker.

Exact combined 53-path implementation/review manifest (unrelated `.serena/`, `reports/`
and `research_notes/` remain untouched):

```text
docs/music-a03c-implementation.md
player-android/README.md
player-android/lib/api/dio_player_api_client.dart
player-android/lib/api/player_api_client.dart
player-android/lib/models/lyrics.dart
player-android/lib/screens/audio_player_screen.dart
player-android/lib/services/lyric_snapshot.dart
player-android/lib/services/offline_download_repository.dart
player-android/lib/services/offline_downloads.dart
player-android/lib/services/playback_cache.dart
player-android/lib/widgets/lyrics_panel.dart
player-android/test/services/lyric_snapshot_test.dart
player-android/test/services/offline_downloads_test.dart
player-android/test/services/playback_cache_test.dart
player-android/test/widgets/lyrics_panel_test.dart
player-server/AGENTS.md
player-server/README.md
player-server/docs/api.md
player-server/internal/api/handlers_compat.go
player-server/internal/api/handlers_lyrics.go
player-server/internal/api/server.go
player-server/internal/api/stream.go
player-server/internal/app/app.go
player-server/internal/app/lyrics_snapshot_revision_test.go
player-server/internal/app/provider_lyrics_test.go
player-server/internal/config.go
player-server/internal/fileversion/version.go
player-server/internal/fileversion/version_test.go
player-server/internal/repository/orphans.go
player-server/internal/repository/provider_lyrics.go
player-server/internal/repository/provider_lyrics_test.go
player-server/internal/repository/schema.go
player-server/internal/repository/time_utc.go
player-server/internal/service/browse.go
player-server/internal/service/lyrics.go
player-server/internal/service/media.go
player-server/internal/service/provider_lyrics.go
player-server/internal/service/provider_lyrics_test.go
player-server/internal/service/service.go
player-server/internal/service/streamer.go
player-server/internal/transcode/cache.go
player-server/internal/transcode/cache_test.go
player-server/web/css/player.css
player-server/web/js/compactControls.js
player-server/web/js/lyrics.js
player-server/web/js/offline-pins.js
player-server/web/js/playback-cache.js
player-server/web/js/playback.js
player-server/web/js/saved-lyrics.js
player-server/web/js/tests/lyrics.test.js
player-server/web/js/tests/playback-nav-skip-images.test.js
player-server/web/js/tests/saved-lyrics.test.js
player-server/web/sw.js
```

## R1 blocking fixes — approved by final R2 (2/2 review budget)

R1 identified three blockers. First, production playback callers still erased
media identity in Offline mode. They now retain it, suppress live radio, and
wait for the existing byte resolver before loading saved lyrics. The real
`initPlayer → selectMedia → offline byte resolver` regression now displays the
retained line. Unknown/null callers fail closed. The earlier direct-load test
was tightened to the actual offline library caller because direct URLs bypass
saved byte resolution.

Second, provider failure used cached data before rechecking local priority and
source/request identity. Both success and failure now recheck access, audio type,
current local/embedded data, and content/request identity before any fallback.
Deterministic wired HTTP regressions add a sidecar or replace source bytes while
the provider returns 503; local data wins, and old-source provider data is omitted.

Third, size/mtime-only audio validators could attach B lyrics to completed A
audio after same-size replacement preserving mtime. The shared fileversion
checkpoint hashes stat device/inode/change time/size/mtime, excluding atime and
exposing no paths/raw inode data. Original HTTP captures it from the opened
descriptor. Transcode keys, before/after encode checks and stale-source cleanup
use that same revision. Current HEAD never labels an old rendition with new
source identity. Original/rendition ETag, HEAD and Range remain usable; date-only
validation is disabled. No additional per-GET media hashing. Unsupported stat
platforms omit verified validators/no-store and skip optional snapshots while
keeping audio playable.

The adjacent backing check is also fixed: web source selection reports the
actual cache name, URL/profile and served ETag through an optional callback.
Audio selection itself is unchanged. Lazy bodies carry the audio ETag; reads
require exact backing proof instead of searching by media ID. An older pin,
another profile, a same-URL copy in another store, a replaced body or a missing
selected-copy body cannot supply unrelated lyrics. Native server-request cache
lyrics use the actual loaded file URI/account instead of a later quality
preference. Unknown backing fails closed on both clients. Existing M57 metadata
and playback ownership callbacks remain intact.

Red evidence: production caller initially showed no retained line; wired failing
provider returned cached provider text over a newly added sidecar; original GET
reused the ETag after preserved-mtime replacement; all five audio rendition kinds
reused old paths/ETags. Each now passes. During harness refinement, corrected the
minimal DOM fragment/text replacement behavior, a Dart UTC-versus-local DateTime
assertion and a test's lazily-created cache directory; final checks are green.

Focused R1 checks:

- `go test ./internal/fileversion ./internal/service ./internal/app ./internal/repository ./internal/transcode -run 'Test(RevisionChange|LyricsSnapshot|ProviderLyrics|Cache_PreservedMtime|Cache_UnverifiedSource|Cache_SourceChangedDuringTranscode|Cache_SourceChanged|Cache_ETagChanges|Cache_Cleanup)' -count=1`
  passed: 0.001s / 0.012s / 8.816s / 2.895s / 0.015s. Includes actual A→B response
  bytes for original stream/download, default compatibility and all four selected
  profiles; changed HEAD validators, old If-Range full response, current If-Range
  partial response and ETag 304. Preserved-mtime change *during* encoding is rejected.
- Compile-only `go test ./... -run '^$'` passed after shared StreamResult changes.
  Scoped vet for fileversion/service/API/repository/transcode passed; scan vet is
  now clean after parent `36f35e2`. `errcheck ./internal/fileversion ./internal/transcode`
  ran with one existing test cleanup diagnostic at `cache_test.go:1224`
  (`defer os.RemoveAll(dir)`), no new implementation diagnostic.
- Six individual Node suites pass: playback-nav-skip-images, lyrics, saved-lyrics,
  playback-cache, offline-pins, offline-browse. Two-copy/store/profile/ETag fixtures
  exercise actual cache selection; local HTTP fixtures reject A/B snapshot mixing
  for original, cache stream and selected AAC/Opus URLs without altering valid A audio.
- Flutter scoped analysis of loaded-source cache/screen and the affected tests is
  clean. The five focused model/panel/download/cache/snapshot files pass 51 tests;
  four local HTTP snapshot cases reject same-size/preserved-mtime replacement.
  Two saved native quality copies resolve only through the actual file URI/account.
- Formatting/imports and `git diff --check` pass. No full suites, final builds,
  installed browser/native acceptance, live runtime changes, commits or task changes.

Exact R1 delta is these 23 paths (the combined manifest above is the original
41-path slice plus 12 newly included paths):

```text
docs/music-a03c-implementation.md
player-android/lib/screens/audio_player_screen.dart
player-android/lib/services/playback_cache.dart
player-android/test/services/lyric_snapshot_test.dart
player-android/test/services/playback_cache_test.dart
player-server/docs/api.md
player-server/internal/api/handlers_compat.go
player-server/internal/api/stream.go
player-server/internal/app/lyrics_snapshot_revision_test.go
player-server/internal/app/provider_lyrics_test.go
player-server/internal/fileversion/version.go
player-server/internal/fileversion/version_test.go
player-server/internal/service/provider_lyrics.go
player-server/internal/service/service.go
player-server/internal/service/streamer.go
player-server/internal/transcode/cache.go
player-server/internal/transcode/cache_test.go
player-server/web/js/lyrics.js
player-server/web/js/playback-cache.js
player-server/web/js/playback.js
player-server/web/js/saved-lyrics.js
player-server/web/js/tests/playback-nav-skip-images.test.js
player-server/web/js/tests/saved-lyrics.test.js
```
