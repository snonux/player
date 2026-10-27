Stage: VERIFIED IN CHROME, FIREFOX AND NATIVE ANDROID.
One bounded expert review found no blockers; focused acceptance passed on all three platforms.

# M19 shuffle albums implementation

Stage: focused platform acceptance complete. Predecessors 7k3/nj3/oj3/zk3 provide canonical compilation, disc/track ordering, playback and queue infrastructure.

Interaction follows U01 (`music-interaction-design.md`). Now Playing toolbar retains Shuffle toggle and Repeat, adds named secondary Shuffle tracks / Shuffle albums choices (Android: Shuffle options menu). The toggle states Off, Tracks or Albums; Reshuffle remaining uses that mode. Web native buttons support Tab/Enter, existing modal j/k/Enter and Escape focus restoration; existing r library shuffle is unchanged. Android uses labeled 48dp controls in its existing wrapping toolbar and system Back sheet dismissal. Existing typography, theme and narrow wrap apply without new styling.

Transitions: choose Albums → Loading album order → upcoming album blocks shuffled → Undo available; current occurrence, source, position and played prefix stay fixed, as does Play next FIFO. Remaining tracks of the active album follow the current/pinned segment in canonical order; other album blocks randomize. Failed/offline catalog request keeps queue and prior mode, with retry via Shuffle albums. Empty/short queues disable shuffle. Choosing Tracks switches mode; toggling Off restores canonical remaining queue order. Manual insert/move behavior remains explicit queue editing; album grouping is applied when Shuffle albums or Reshuffle remaining is chosen.

Server POST /music/queue-albums bounds input to 10,000 positive safe integer IDs and 200,000 bytes, then maps requested IDs only to canonical caller-visible MusicAlbums identity and rank; compilation/MBID/folder semantics and unknown disc/track ordering exactly reuse catalog rules. Entries outside catalog remain individual occurrence blocks. Duplicate occurrences retain stable rank ties. Portable sessions and local recovery carry additive shuffle_mode, defaulting older sessions to tracks. Repeat traverses the resulting saved queue order.

The next combined full regression gate is due after twenty completed tasks under
the user's ten-task cadence. No release/version/push/deploy.


Focused verification commands:

- `node player-server/web/js/tests/queue-shuffle.test.js`, `queue-storage.test.js`, `queue-sync.test.js`: pass (canonical album continuation, pins, duplicates, unknown singletons, restore and mode validation).
- `go test ./internal/api ./internal/service -run 'TestMusicAlbumsRealPermissions|TestSavedQueue' -count=1` from server: first endpoint ACL/request checks passed; later service mode checks pass, API recompilation temporarily blocked by concurrent artwork GetMusicArtwork mock interface handoff.
- `flutter test --no-pub test/services/music_shuffle_test.dart test/services/saved_queue_sync_test.dart test/widgets/music_queue_sheet_test.dart`: 13 pass; includes album lookup failure with unchanged queue/source, mode snapshot/portable roundtrip, clear/Undo and named options sheet interaction.
- Additional `music_queue_recovery_test.dart` run had an existing async play-timing failure (`paused handler recovery…`, expected plays 1, got 0); retained as integrated verification gap, no baseline retries. Initial new loader local-function declaration compile issue was fixed. Initial toolbar density widget failures were fixed using native progressive disclosure and a shorter hint.
- Scoped Dart analysis and `git diff --check` passed during implementation.
  The later ten-task full regression passed 1749 Flutter tests and the whole
  Go race/coverage suite after this implementation, resolving the earlier
  recovery-test timing gap. The next full gate follows the user's cadence.

## Focused Chrome and Firefox acceptance (2026-10-27)

Installed Chrome 144.0.7559.132 and Firefox 156.0 each passed the two focused
`test/e2e-web/tests/music-shuffle-albums.test.ts` journeys with one worker at a
time. The disposable fixture used `--multi-disc --shuffle-playback` on port
18872 and server 0.8.2, binary revision `c6375805bfd3` (which includes M19).
The focused commands were:

```sh
GOMAXPROCS=2 python3 player-android/integration_test/support/music_fixture.py up /home/paul/tmp/codex/player-m49-server --port 18872 --no-doubles --multi-disc --shuffle-playback --dir /home/paul/tmp/codex/player-m19-18872 --evidence /home/paul/tmp/codex/player-m19-evidence
cd player-server/test/e2e-web
GOMAXPROCS=2 PLAYER_MUSIC_FIXTURE=/home/paul/tmp/codex/player-m19-18872 PLAYER_E2E_PULSE_SINK=player_e2e_null npx --no-install playwright test -c playwright.music.config.ts tests/music-shuffle-albums.test.ts --project=chrome --workers=1
# The same Playwright command then ran with --project=firefox.
```

The mixed 14-song playlist spans five catalog albums, including three with
tracks on two discs and a compilation with different credited artists. Across
repeated reshuffles, the current song, an inserted Play next occurrence, and
the remaining active album songs retained their positions. Every other album
stayed in one block with its server-ranked disc and track order, and album
block order changed. Undo restored the prior occurrence order. Switching to
Tracks and then Off restored the canonical remaining order while preserving
the current audio source. Reload restored Albums mode, occurrence IDs and seek
position; Resume and Next played the expected source, confirmed by captured
audio tone.

The journeys used pointer and Enter activation, checked focus and native text
entry, and captured dark/light desktop and narrow queue layouts. Offline album
lookup retained the previous Tracks mode and queue; online retry switched to
Albums. The catalog endpoint excluded restricted songs for the viewer, returned
no membership after real Music permission revocation, and isolated the queue
across account changes. Evidence and screenshots are under
`/home/paul/tmp/codex/player-m19-evidence`; the disposable fixture ran from
`/home/paul/tmp/codex/player-m19-18872` and was removed after acceptance.
Early Chrome runner setup failures
were corrected before the final focused passes and remain in the append-only
evidence log.

## Focused native Android acceptance (2026-10-27)

One read-only Android 14/API 34 emulator (33.1.24.0) ran the x86_64 release
APK `0.8.2+4022` (SHA-256 `e6f805dcb1076258d7691d2fdb6fb7a0ab4d115633a8dddf2b79b05b2067b838`)
against an isolated `--multi-disc` fixture on port 18822. The exact setup
commands and structured artifacts are in
`/home/paul/tmp/codex/player-m19-evidence/native-summary.md`; the emulator
was stopped afterward. UI actions used the installed app's touch controls and
Android Back, with QEMU WAV output for source checks.

A 14-song playlist deliberately interleaved five albums, including two M04
two-disc releases and a compilation. The named Albums option grouped each
album contiguously and restored server-ranked disc/track order without changing
the current occurrence, paused source or position. A Play next occurrence
stayed first, followed by the remaining active-album songs. Reshuffle changed
later album blocks; Undo restored exact occurrence order, mode and pin.
Playing the pinned M04 Album 1-1 track produced four stable one-second 730 Hz
output windows and saved 6.31 seconds at its queue index. First Light output
also matched its expected 440 Hz tone. Shuffle Off restored the canonical
remaining order while retaining the current source.

Removing the emulator's server reverse mapping made Shuffle albums show a
retryable connection failure while the queue, mode, pin, paused source and
position stayed fixed. Restoring the mapping made retry succeed. A cold
relaunch and saved-server sign-in restored album mode, order, pin, index and
position. Light and dark controls were readable; Resume queue played the
saved M04 track from its position with matching 730 Hz output. Signing into
the second fixture account showed an empty queue and Shuffle Off, then
returning to the first restored its scoped queue. Native permission revocation
was not repeated separately; browser and server ACL checks covered the same
endpoint, while the native offline check exercised its failure handling.
