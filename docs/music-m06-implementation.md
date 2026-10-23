# M06 music and file information

## Approved interaction and data contract (before implementation)

Task `8k3` extends existing U01/M05 Info surfaces. Existing title, tags,
release/credit groups and file codec/bitrate/size/path rows stay available.

Web layout: existing Info modal → title/filename/basic music tags + **Tempo**
(`123.5 BPM (beats/min)`) → existing Release details / Additional music details
→ collapsed **Advanced audio** → existing file/status/raw API information.
Advanced audio contains Sample rate (`48 kHz (48000 Hz)`), Bit depth (`24 bits`),
Channels (`2 (stereo)`), Track ReplayGain / Album ReplayGain (`-6 dB`), and
Track peak / Album peak (`0.89 (1 = full scale)`). Concise help identifies
indexed original-file data and explains that ReplayGain tags are informational.
No playback volume adjustment is introduced. Missing/invalid/nonfinite fields
are omitted; valid zero gain remains visible. Unknown lossy bit depth is omitted,
never inferred from decoder sample format. Empty advanced groups are hidden.

Web keeps `i` / menu Info, Tab then Enter/Space disclosures, j/k/arrows scroll,
Esc close and opener focus restoration. No shortcut is added. Labels/values wrap
and remain readable in dark/light/narrow layouts. Opening/closing Info does not
replace, pause or seek the playing source. Native Music details and saved offline
snapshots reuse selectable metadata; Advanced audio is collapsed. A server audio
player app-bar **Info** action opens **Music and file information**, with scroll,
Close and native Back returning to the same player/source/time. Its asynchronous
load shows loading, readable failure and Retry; stale source/account responses
must not expose obsolete metadata. Offline snapshots identify their saved state.
Local/public playback scopes retain their existing behavior.

Data is additive optional `music` JSON, using existing persistence/rescan/offline
snapshot boundaries; no new endpoint, schema migration or catalog eligibility
change. BPM is positive, ReplayGain gain is signed finite including zero, peaks
are finite nonnegative; unknown fields are absent. Aliases are bpm/tbpm/tempo,
replaygain_track_gain/album_gain (optional dB suffix), and
replaygain_track_peak/album_peak. Sample rate/channels/bit depth come from the
first audio stream. Bit depth uses bits_per_raw_sample then bits_per_sample.
Source fields survive the subsequent structured-comment tag parse.

## Focused checks and integrated acceptance plan

Focused checks cover numeric parsing/zero/malformed values, first audio stream
with artwork, real FLAC/MP3/Opus/M4A fields, JSON snapshots, missing/long selectable
rows and dialog loading/Retry/Back without source mutation. Root owns integrated
full gates, final builds, review and actual installed Chrome/Firefox/native checks.

Use a guarded additive technical fixture namespace (distinct Technical verification
genre), real 24-bit 48 kHz stereo FLAC and lossy unknown-depth audio lasting at
least 120 seconds, zero/nonzero gain/BPM/peaks and long credits. Do not modify old
fixtures or M08's Ambient/2021/Night Transit scenarios. Actual journeys compare
visible fields with source tags/probe, open/expand/close Info during uninterrupted
source/time/audio, check keyboard/focus and native Back, dark/light/narrow content,
offline Info failure/Retry, ACL denial, same-database restart and saved offline
fields. Existing M05 state helpers are reused; focused server tests cover new-field
retag/clear/failed-probe rather than repeating every M05 platform journey.

Implementation is recorded below; actual platform acceptance remains pending.

## Implemented paths and focused verification

The additive music JSON fields are `bpm`, `sample_rate`, `bit_depth`, `channels`,
`replaygain_track_gain_db`, `replaygain_album_gain_db`, `replaygain_track_peak`
and `replaygain_album_peak`. Nullable floating-point tags preserve zero gains;
integer technical fields omit unknown values. The prober retains source fields
after its structured-comment reparse. Existing catalog membership and grouping
consume their existing tag fields only. No service/repository API was changed.

The native player Info action captures the source session, account/credential
epoch and origin before loading; stale/unknown scope fails closed. Its dialog
reuses selectable details and does not call playback mutation methods. Existing
inline details and downloaded music snapshots gain the same advanced disclosure.
The existing M05 cleared-tags browser assertion now checks absent release/credit
groups plus retained 44.1 kHz/16-bit source fields.

Focused parser/real-container verification passed (2.134 s, exit 0):

```sh
cd player-server
command -v ffmpeg ffprobe
GOMAXPROCS=2 PLAYER_REQUIRE_FFMPEG=1 go test ./internal/probe -run 'Test(Audio|MusicTags|MusicProbeStreamTags|MusicRealContainers)' -count=1
```

Both tools resolved to `/usr/bin`. Logs:
`/home/paul/tmp/vk3-8k3-evidence/m06-implementation/go-probe.log`.
Existing and new Node metadata mapping tests and JS syntax checks passed.
Three lightweight fixture guard tests passed; new fixture/native Python sources
compile and pass pyflakes. Playwright `--list` discovers four M06 cases across
Chrome and Firefox; this is discovery only, not browser acceptance.

## Integrated commands (root-owned; not executed by this worker)

Root creates the new marked disposable fixture at
`/home/paul/tmp/vk3-8k3-fixture`, port 18816, using `--no-doubles --release-metadata`,
then augments it once without touching earlier fixtures:

```sh
python3 player-android/integration_test/support/music_info.py /home/paul/tmp/vk3-8k3-fixture
```

From `player-server/test/e2e-web`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/vk3-8k3-fixture npx --no-install playwright test -c playwright.music.config.ts tests/music-info.test.ts --workers=1
```

From the monorepo root, with the final APK and root-owned emulator/QEMU output:

```sh
python3 player-android/test/e2e-live/music_info_e2e.py /home/paul/tmp/vk3-8k3-fixture --apk FINAL_APK --emulator-wav QEMU_WAV --resume-device
```

Native `--scenario journey|errors|saved` allows focused affected continuation.
The fixture uses 180-second sources, restarting the source for each native
layout pass. Both runners retain actual Info/Back playback/audio, source units,
zero gain, long credits and lossy omission checks. Error scenarios exercise
real offline failure/online recovery, actual denied Info while buffered playback
continues and same-database restart. Native saved checks remove the server route
and read the downloaded snapshot. No fixture/server/emulator/browser runtime,
full regression suite or final build was launched by this worker. All actual
three-platform acceptance remains pending.

Focused native checks passed: `flutter test test/models/music_metadata_test.dart
test/widgets/music_metadata_details_test.dart test/widgets/music_info_dialog_test.dart
test/screens/offline_downloads_screen_test.dart` (11 tests, exit 0, about 7 s).
Final scoped analysis of the five owned source/test files passed with no issues
(1.5 s, exit 0). Three initial brace-style analyzer notices were corrected;
initial/final logs remain under the external implementation evidence directory.
The scope guard explicitly rejects loading and unauthenticated state as well as
account, credential generation, origin, source identity and session changes.

Focused scanner state verification passed (3.683 s, exit 0):

```sh
cd player-server
GOMAXPROCS=2 PLAYER_REQUIRE_FFMPEG=1 go test ./internal/scanner -run '^TestScan_MusicRefreshPreservesIdentityAndState$' -count=1
```

This exercises real retag/clear/failed-probe transitions, preserving valid zero
gain, source fields and existing identity/progress/counts. External log:
`/home/paul/tmp/vk3-8k3-evidence/m06-implementation/go-scanner.log`.
Implementation is frozen for root review; actual platform acceptance and full
gates/builds remain pending.

## Implementation-first workflow stage

The latest user workflow (2026-10-22 17:53 UTC) defers full regression suites,
final server/APK builds and actual Chrome/Firefox/native Android journeys until
the remaining backlog is implemented. First expert review found no confirmed
M06 blocker. This is reviewed implementation awaiting integrated verification;
task `8k3` remains pending. Previously authored fixtures/runners are retained
for that later phase, and no actual platform acceptance is claimed here.
