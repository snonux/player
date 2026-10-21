# M12: real music sequence and track transport

## Interaction contract (revised before continuing feature coding)

U01 §8.0 is authoritative: the existing Sets browse sequence stays live,
including displayed/search/filter/shuffle order, folder traversal, set-level
wrap and saved resume on explicit play and automatic advance. The accepted
M12 change skips images when advancing from audio. Video-started and
image-started browse sequences stay unchanged. B4 (dependence on the visible
browse grid) deliberately remains. Independent frozen queue, no-wrap and
start-at-zero belong to future Music lens/queue actions, not this task.
An earlier implementation sketch proposed those future semantics for Sets;
that sketch was rejected before verification and removed.

Web keeps existing artwork, seek bar and transport row, with accessible
Previous track / Next track labels during audio playback. Existing N/P keys
remain unchanged, as do Space/p, j/k/h/l, arrows, typing, focus and Back.
Android retains artwork and seek bar, replacing primary ±15-second buttons
for music with Previous track | Play/Pause | Next track (48 dp targets).
Separate labeled ±15-second controls and speed remain available. Podcasts
and audiobook source paths/set names retain primary seeking and native seek
transport. Embedded display titles never determine audiobook status. Long
music files remain valid songs. No queue editor/repeat/shuffle setting added.

Native starts from displayed music order, resolving folder edges through
existing permitted browse APIs, with set-level wrap. Its handler owns the
source-set/path/list context, loader and per-item progress after Back. It
never claims video sequencing: native video remains single-item. Playback
of a different source/account invalidates pending transitions and sequence.
Track change flushes old progress before loading/resuming the target.
Completion saves finished before advancing. Rapid skips cannot race source
replacement. Loading disables transport; missing/forbidden/offline or bad
sources show existing readable error/Retry without silently skipping items.
One-song sets replay by existing wrap semantics. Empty lists cannot start.
Long titles truncate in existing chrome, retaining accessible labels. There
are no new permissions or restart persistence (M16).

Verification covers three distinct natural completions, actual source tones,
IDs and saved progress, in-app/native notification/lockscreen transport,
folder wrap/image skips, resume, rapid skips, failure/Retry, podcast seeks,
Chrome/Firefox keyboard typing/focus, Android touch/Back, and light/dark/narrow
screens. Evidence lives outside committed media/build outputs. Parent owns
full suites, review and commit; implementation worker does not complete task.

## Verification

Evidence root: `/home/paul/tmp/1k3-evidence`; media, databases, binaries and
captures stay outside Git. The parent owns full gates, two completed fresh
review rounds, staging and completion. Both review rounds' blocking findings
have been repaired; the final narrow review found no remaining blockers.

Flutter analysis reports zero issues and the final full suite passes all
1338 tests. Focused screen/route/handler/coordinator/sequence checks pass 70
tests, with an additional 8-test sequence run. Runtime Retry stays on the
handler's current song and last valid position after Back/native position
reset; failed folder-edge navigation retains its direction. Retry positions
cannot transfer to a different song. Filtered/sorted grid playback wraps its
displayed results; only FolderBrowser context permits set traversal.

The final full web unit suite, Go build/vet and race/coverage suite pass.
`errcheck` reports 256 existing diagnostics: comparison against HEAD found
zero newly introduced diagnostic lines. Scoped Go formatting is clean.
Scoped M12 TypeScript checking and Python compile/pyflakes pass; the existing
whole-directory TypeScript missing-DOM-library errors were not changed.

Actual installed Chrome 144.0.7559.132 and Firefox 156.0 pass all four M12
journeys (attempt 3, 2.1 minutes). Each plays three distinct songs naturally,
records 440 Hz / 523.2 Hz / 659.3 Hz output for the matching IDs, and verifies
saved finished progress before advancing. Both also verify finished-song
restart, cross-folder wrap with audible output, saved resume, UI handling of injected HTTP 403 and Retry, rapid navigation, keyboard typing/focus and narrow/light plus
desktop/dark controls. Firefox's narrow light capture shows filtered empty
results while current playback continues. Screenshots were visually reviewed.

Actual failures drove two narrow repairs: completion now serializes final
position and explicit finished status before advancing, and flattened browse
items have deterministic filename/path/ID order. Regressions cover both.
Old inactive ended events cannot mutate the active song; local progress
flushes respect Cast/detached progress ownership. All failed attempt logs and
captures remain available.

The fresh x86_64 release APK is version 0.8.2 (code 4022), SHA256
`1354e055308e6eba7d051f2e0f306f74547f45e49b95bd0d760846da843d1ece`.
The fresh fixture server identifies revision
`f89c78549ad4512d8b594c0f5f526e77782001ae-dirty`.
Native API 34 / x86_64 emulator (33.1.24.0, cold boot, SwiftShader) passes
all feature scenarios in attempt 2: 28 recorded checks, zero failures,
exit 0. Touch Next/Previous, notification Next/Previous and actual keyguard
Next/Previous each produce their own matching tone and newly saved progress
for the target ID. Next resumes the saved 3-second position. Natural IDs
7/1/4 complete at 12 seconds, save finished and audibly advance in order.
Back preserves handler-owned playback. A real unavailable fixture file shows
the readable target error and Retry recovers that same song. An audiobook
path with misleading music title retains seek transport on one source:
825 ms → 15825 ms → 825 ms. Actual STREAM_MUSIC readback is speaker 15/15,
unmuted, before audible proof; existing capture tolerances remain unchanged.

The first native setup attempt encountered a Pixel Launcher ANR before
sign-in and supplies no feature proof. Its log and system diagnostics are
preserved; closing that dialog allowed the same boot to continue. Attempt
2's original light and dark PNGs are fully rendered and visually reviewed;
the audio screen intentionally retains its existing dark appearance under
both system modes. A misleading resized tool preview initially appeared
incomplete; reviewing original-size PNGs confirmed the controls and artwork.
The redundant focused themes run also exits 0 and is retained under
`actual-native-themes-recovery/android`. Native feature output is in
`actual-native-attempt2/android`; browser output is in
`actual-attempt3/{chrome,firefox}`. The owned emulator, fixture server and
Pulse sink are stopped; app data and adb reverse mappings are cleared.

## Reproduction commands

From `player-android`, build the tested native artifact:

```sh
flutter build apk --release --split-per-abi --target-platform android-x64
```

The disposable fixture runs the freshly built server on port 18796. From
`player-server/test/e2e-web`, run the installed-browser checks:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/1k3-fixture PLAYER_E2E_PULSE_SINK=player_1k3 npx --no-install playwright test -c playwright.music.config.ts --grep M12 --output /home/paul/tmp/1k3-evidence/playwright-attempt3
```

From `player-android/test/e2e-live`, use the fresh APK and actual QEMU output:

```sh
python3 -u music_sequence_e2e.py /home/paul/tmp/1k3-fixture --apk /home/paul/tmp/1k3-evidence/app-x86_64-release.apk --emulator-wav /home/paul/tmp/1k3-evidence/emulator-output.wav
```

The focused screenshot recovery uses the same command with `--scenario themes`.
Each attempt retains its own log and evidence directory; no production media
or user account is used.
