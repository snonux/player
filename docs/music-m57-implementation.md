# M57 — Tasker / Locale music automation

Implementation and two bounded reviews are complete; integrated verification
and acceptance remain pending. Final R2 cleared the catalog-eligibility and
listening-session fixes described below.
Prerequisites nj3, oj3, ck3 and reviewed tl3 (`7f62601`) are retained.

## Interaction design

Android Settings → Music automation expands concise setup/help, last result and
Revoke saved actions. Everyday playback screens gain no controls. In an installed
automation host, add Player music → choose Start queued music, Shuffle queued
music, Use offline music only, or Allow online music → Save action. The native
configuration screen uses the device day/night theme, a scrolling vertical layout,
48dp minimum controls, readable wrapping help and explicit Save/Cancel. System
Back cancels without changing the host configuration. Editing retains the action.

First open Player, select and unlock the intended server account, and queue music.
Start resumes that account's selected queued song at its current position;
shuffle explicitly reshuffles upcoming songs using the existing queue action and
starts/resumes music, without toggle semantics. Both retain label, occurrence IDs,
canonical order, repeat and Play next state. Offline source conversion uses the
normal queue recovery API and retains position and these queue settings.
These actions never choose a podcast, video, radio, device
file or public share. No queue / missing music metadata / busy playback produces
an actionable failure. Offline-only affects subsequent automation playback for
that account: all queued songs must have valid downloads or playback-cache copies;
missing copies fail before replacing the queue. Allow online keeps normal cache,
network and authentication policy. Policy selection itself does not start audio.

Saved actions belong to the account selected at configuration. Switching account,
locking or signing out fails with an instruction to open/unlock the matching
account. Cold app startup requires opening Player and unlocking first; automation
does not launch an Activity or unlock credentials in the background. Foreground
service/audio-focus restrictions are reported as failure, never queued success.
Revoking actions invalidates their private capability records; the host must save
new configurations. Last result remains visible in Settings, including errors
from asynchronous Locale hosts that cannot receive completion.

## Protocol and ownership

Use the small base Locale Intent protocol directly: exported EDIT_SETTING
Activity and FIRE_SETTING receiver, standard BUNDLE and STRING_BLURB extras.
The bundle contains a random opaque capability only; action/account records stay
in app-private preferences. Validate action, bundle shape, token and bound account;
never accept URLs, credentials or media paths from a caller. No new dependency or
vendored SDK is needed for four fixed actions with no variable replacement.
Tasker's ordered execution uses documented requested timeout/result constants and
error-variable bundle. Result is completed within a bounded receiver lifetime;
timeout cancels the Dart intent so delayed reads cannot later start playback.

The existing Flutter engine/provider graph owns credentials, playback coordinator,
queue and cache/download readers. The native plugin forwards bounded commands;
it never creates another player or media service. Account, playback ownership,
transport revision and explicit music intent are checked after asynchronous reads.
Saved configurations remain account-bound across credential refresh, while each
invocation captures fresh credential epochs. A newer interaction cancels stale work.

Primary references consulted: [Tasker protocol](https://tasker.joaoapps.com/plugins.html),
[modern library](https://tasker.joaoapps.com/pluginslibrary.html),
[Locale specification](https://github.com/twofortyfouram/android-monorepo/blob/master/docs/Plug-in%20API%20Specification.md),
[Tasker constants](https://tasker.joaoapps.com/code/TaskerPlugin.java),
[Android background restrictions](https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start).

## Integrated acceptance (2026-10-27)

Local Locale-compatible host APK lives outside git at
`/home/paul/tmp/codex/locale-fire-host` (package `zone.foo.localefire`,
EDIT_SETTING launcher + `FIRE_NOW` rebroadcast). Native journey:
`player-android/test/e2e-live/music_automation_e2e.py` against disposable fixture
port 18850 on API 34 emulator with QEMU WAV. Seeds the interactive Now Playing
queue via **Play album** (folder Play Audio does not own that queue), saves and
fires Start / Shuffle / offline-only / allow-online through the host, and checks
media-session PLAYING plus Settings “Last automation result”. Evidence under
`/home/paul/tmp/codex/player-m57-evidence` and `player-m57-native-run16.log`.

Play-confirm now accepts decoder motion or a short playing+ready stretch so
tracks that restart at 0:00 are not cancelled; shuffle no longer treats an
in-flight transport bump during upcoming reshuffle as supersession; native
receiver timeout is 15s.

Shared web queue (no Android plugin): Chrome
`tests/music-queue.test.ts` 4 passed / 1 skipped; Firefox keyboard, ACL/offline
edit recovery, and narrow themes passed. One Firefox natural-order case hit the
known floating-player overlay flake (`player-restore` intercept) and is not
treated as an M57 product regression. Logs:
`/home/paul/tmp/codex/player-m57-web-queue-chrome.log`,
`player-m57-web-queue-firefox-ok.log`.

## Focused implementation checks

On base HEAD `a06a2af`, Flutter targeted analysis of the provider, services,
Settings widget/composition and two new test files passed. Commands run from
`player-android` (Flutter `/home/paul/flutter/bin/flutter`):

```
flutter analyze lib/providers/music_automation_provider.dart lib/services/music_automation.dart lib/services/music_automation_queue.dart lib/widgets/music_automation_settings.dart lib/screens/settings_screen.dart lib/main.dart test/services/music_automation_test.dart test/services/music_automation_queue_test.dart
flutter test test/services/music_automation_test.dart test/services/music_automation_queue_test.dart --timeout 15s
```

Ten focused tests passed: lock/account/source refusal, stable account scope,
owned-lease cancellation vs newer manual playback, late lease acquisition,
held disk-read cancellation, missing copies, actual handler paused/repeated Start,
explicit non-toggle shuffle, and offline recovery preserving queue context.
The decoder is a test substitute; this proves adapter behavior, not audible output
or installed host compatibility. An initial adapter test run was stopped because
widget fake time held the normal handler stop; using real test time passed.

One bounded native compile plus its metadata retry: from `player-android/android`,
`JAVA_HOME=/home/paul/jdk17 ANDROID_HOME=/home/paul/Android/Sdk ./gradlew :app:compileDebugKotlin --no-daemon --max-workers=2 '-Dorg.gradle.jvmargs=-Xmx3G -XX:MaxMetaspaceSize=1G -XX:ReservedCodeCacheSize=256m'`
passed in 37s (log `/tmp/player-m57-kotlin-compile.log`). The first `--offline`
attempt failed before source compilation because existing `integration_test`
needed uncached AndroidX runner dynamic-version metadata. No native source changed
after the successful compile. No APK build/install, live runtime change, full
suite, local host implementation, browser/emulator feature journey, screenshots,
physical-device or light/dark/narrow visual acceptance was performed in this stage.

## R1 corrections

Automation now checks server media IDs against the authenticated music catalog,
including IDs reconstructed from legacy queue snapshots. The production
media-detail Play now action can legitimately queue a tagged podcast for manual
playback; its tags no longer qualify it for automation. Downloads and cache copies
retain catalog eligibility in private metadata (download schema 8, existing copies
default unvalidated). Offline-only uses only that saved classification and local
file validation. With online allowed, Start validates old saved IDs and upgrades
only matching origin/account metadata, without replacing audio or changing
listening state. The existing already-downloaded save path also upgrades metadata.
Unknown offline-only copies explain how to allow online and validate once.

Download preflight now validates bytes without creating a listening occurrence.
Deferred playback requests expose a begin-listening callback used only while
loading a replacement source. Repeated Start on an active offline queue preserves
its existing source callbacks/session, and a missing later copy aborts before any
session mutation. Ordinary manual playbackRequest behavior remains unchanged.

Red replay restored the two prior behaviors temporarily (tag presence bypassing
catalog validation, and unconditional session creation during preflight). The two
new regressions failed as expected: the tagged production Play now request was
accepted by automation, and the repository listening session ID changed after
repeated preflight. Log: `/tmp/player-m57-r1-red.log`; no temporary replay changes
remain. Green focused run passed 45 tests across music_automation_test,
music_automation_queue_test, music_automation_catalog_test, offline_downloads_test
and playback_cache_test (`/tmp/player-m57-r1-green.log`). It covers actual
insertServerSongs/handler playback, repeat-Start credit reaching 60 accumulated
seconds, missing-copy/session isolation, catalog-free offline validation and
metadata-only upgrades without an extra HTTP media request. These are focused
implementation regressions, not actual host/platform acceptance. Kotlin is
unchanged, so the prior native compile result is reused. No full suite, new build,
host/emulator/browser journey or live runtime change was performed in this stage.
