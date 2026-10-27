# M55 Android Auto music browser

Implementation reviewed in round 1 with no blocking findings; task 7l3 remains pending integrated verification.
Prerequisites nj3/oj3/yj3/zj3/3k3/qk3/ck3 supply the approved design, fixtures,
permission-aware catalog/search/playlist APIs and shared occurrence queue.

## Interaction contract

The Android Auto host supplies typography, light/dark rendering, focus, touch
targets, Back, voice entry and playback chrome. Player supplies four browsable
root tabs (the host's usual limit): **Albums**, **Artists**, **Playlists** and
**Queue**. No driving-time account chooser or editable settings are added.

```text
Albums → album → Play all / ordered songs → existing Now Playing + queue
Artists → artist → albums → album → Play all / ordered songs
Playlists → playlist → Play all / ordered song occurrences
Queue → existing ordered occurrences → jump to that same occurrence
Search → songs / album / artist → play song or open collection
```

Each browse response contains at most 40 content rows plus **More…**. Search
returns at most 20 songs, 10 albums and 10 artists. Long titles remain ordinary
host text, with artist/album metadata on songs. Album and playlist playback
retains order and duplicate occurrences; selecting a song keeps its containing
ordered queue block and starts the selected index. Collections exceeding 1000
songs expose **Play first 1000 songs**; the resulting queue label identifies
the included song range. The host session queue contains a window of at most
100 occurrences around the current song; the Queue browse tab pages the full
queue. Transport uses the existing handler's play/pause/previous/next/seek and
queue ownership, loader, compatibility preparation and progress callbacks.

Loading is represented by the host's detached browse request. Catalog requests
and initial native proxy connection wait are bounded to 15 seconds. Empty collections
say **No music available**. Connection/access errors provide a concise retry
message without server exception details. Reopening the collection fetches
again. Locked/signed-out/server-unselected accounts show **Open Player on your
phone to sign in or unlock**, with no cached private tree. Stale account IDs or
changed song ordinals are rejected; users browse again. Source/account changes
discard pending work. Offline server catalogs require reconnection; this slice
does not advertise downloads or a local-device catalog as Auto music.

This is a native integration. Existing Chrome/Firefox catalog, search, touch
and keyboard journeys remain the shared browser behavior; browsers do not
emulate a car host. No additional web controls or shortcuts are appropriate.

## Boundary and discovery

The existing audio_service 0.18.18 service owns the sole player/session.
Its upstream native implementation hardcodes `AudioService.class` for Flutter
binding and foreground starts, so substituting a subclass is unsafe. An
app-owned `AutoBrowseService` validates UID/package membership and AndroidX
trusted-media-controller status (or the exact Android Auto package signed by
one of the two production certificate digests in the
[official UAMP allowlist](https://github.com/android/uamp/blob/main/common/src/main/res/xml/allowed_media_browser_callers.xml)),
then forwards browse/search/item reads to the
internal service and exposes its original session token. The original service
is nonexported and has no discovery filter; the proxy is the only public browse
filter, preserving AndroidX media-button service discovery. The proxy bounds
subscriptions and pending connections, shares concurrent reads, forwards
invalidation and unsubscribes old callbacks when refreshing.

Catalog media IDs contain a digest of server origin/account/credential epoch,
canonical entity keys and (for selected collection songs) ordinal plus expected
canonical media ID. They contain no bearer token, session cookie, stream URL,
filesystem path or access grant. Playback resolves through the currently
authenticated API; native queues and artwork do not contain credential URLs.

Generic MediaBrowser support and the automotive descriptor add no Google
runtime SDK or proprietary dependency. This repository currently has no
Android product-flavor split; the FLOSS build can expose the same browser
service to compatible trusted hosts. Android Auto itself is a separate host.
Do not infer host compatibility or distribution approval from this descriptor.

Design references: [Android content hierarchy](https://developer.android.com/training/cars/media/create-media-browser/content-hierarchy),
[official MediaBrowserService sample](https://android.googlesource.com/platform/developers/samples/android/+/refs/heads/master/media/MediaBrowserService/).

## Verification stage

On 2026-10-23, focused implementation checks passed on shared version 0.8.2+22,
based on 291aaad plus the M55 working slice:

```sh
# From player-android/
flutter test --no-pub test/services/android_auto_catalog_test.dart test/services/android_auto_handler_test.dart test/services/headset_handler_test.dart test/services/audio_handler_errors_test.dart
# 33 passed, including 12 catalog and 4 handler adapter tests.
dart analyze lib/services/android_auto_catalog.dart lib/services/audio_handler.dart lib/providers/android_auto_provider.dart lib/main.dart test/services/android_auto_catalog_test.dart test/services/android_auto_handler_test.dart
# No issues found.
flutter pub get --offline
# Success; rxdart remains 0.28.0, promoted from transitive to direct dependency.
```

Both manifest/descriptor XML files parse; `git diff --check` passed. These
commands did not compile Kotlin or build an APK. Focused catalog checks cover locked state, safe IDs, paging, artist/playlist
browse, occurrence ordering, stale collection ordinals, account changes, late
responses, concise failures, search and queue limits. They are implementation
checks, not host or platform acceptance. Handler adapter checks exercise
no-screen selection through the shared queue/coordinator/loader with a fake
player, Next, exact repeated-occurrence jump, paused active-window indices,
truthful existing playback after selection errors and bounded shared browse
invalidation. No fake player is treated as Android audio or host proof.

Review noted a nonblocking upstream retention limit: audio_service 0.18.18
keeps a Dart listener and map entry for each parent ever loaded. Player uses
two shared invalidation subjects and bounds native proxy subscriptions to 128,
but proxy eviction does not cancel those upstream Dart registrations. Later
invalidations can still notify historical parents. The stream-identity test
does not establish a bound on that upstream listener count; assess traversal
impact during integrated host verification. No vendored dependency was edited.

## Integrated verification (2026-10-27)

Shared Chrome catalog journeys: `music-albums.test.ts` + `music-search.test.ts`
7/7 passed against fixture 18850
(`/home/paul/tmp/codex/player-m55-web-chrome.log`). Firefox ordered-collection
was later unblocked by removing a merge-duplicate `initRadio` import that
aborted `app.js` before login bound, plus e2e overlay hardening
(`71799dd`); re-run both projects against the music fixture when regressing M02.

Android Auto Desktop Head Unit package is installed at
`Android/Sdk/extras/google/auto/desktop-head-unit` (`sdkmanager
'extras;google;auto'`). On the phone API 34 Google APIs AVD, DHU TCP-connects
to `adb forward tcp:5277` then exits because
`com.google.android.projection.gearhead` is a **stub**
(`versionName=1.2.531830-stub`) without HeadUnitServerService.

An Automotive x86_64 AVD (`Player_Automotive_API34`, system image
`android-34-ext9;android-automotive;x86_64`) boots and runs
`com.android.car.media`. Phone DHU remains blocked by the gearhead stub; the
Automotive host is the accepted Auto path for M55 (task `7l3`).

AAOS `MediaSource` skipped Player until the browse service declared
`androidx.car.app.launchable=true` (same opt-in as Local Media Player). The
application also declares `com.android.automotive` → `automotive_app_desc`
alongside the existing Android Auto `com.google.android.gms.car.application`
entry. With those present, logcat shows `AutoBrowseService is opted in` and
Player appears in Switch apps next to Bluetooth / Local / News / Radio.

Sign-in on the car UI works after `wm size 1280x800` (physical 320×640 clips
the form): choose the saved server URL button, enter credentials, move focus
to the Sign In `Button` with TAB (do not KEYCODE_BACK — that leaves the app),
then submit. Browse tabs Albums / Artists / Playlists / Queue / Search load
fixture titles; album play opens Now Playing; `media_session` for
`zone.foo.player_android` toggles PLAYING/PAUSED on MEDIA_PLAY_PAUSE; search
`Aurora` returns matching tracks/artists. Evidence under
`/home/paul/tmp/codex/player-m55-evidence/` (`m55-g-*` … `m55-j-*`,
`m55-automotive-acceptance.txt`).

## Phone DHU / non-stub Android Auto host (follow-up `7s3`)

M55 acceptance does **not** require phone Desktop Head Unit. The Automotive
AVD exercises the same `AutoBrowseService` MediaBrowser contract through
`com.android.car.media`. Use phone DHU only when you need projection-specific
behavior (gearhead template chrome, wireless/USB transport, DHU input modes).

### Detect the stub

```sh
adb shell dumpsys package com.google.android.projection.gearhead \
  | grep -E 'versionName|versionCode'
# Stub (cannot host DHU media): versionName=…-stub  (e.g. 1.2.531830-stub)
# Real Auto: a non-stub Play Store build with HeadUnitServerService present.
```

Google APIs emulator system images ship the stub on purpose. Installing DHU
via `sdkmanager 'extras;google;auto'` does not replace it. Sideloading a
Play-signed gearhead APK onto an AVD is not a supported or redistributable
path for this repo — do not commit or fetch unofficial APKs.

### Obtain a usable phone Auto host

1. **Physical device (preferred).** A phone/tablet with Google Play, Android
   Auto installed from Play (not the emulator stub), developer mode, and USB
   debugging. Enable Developer settings inside the Android Auto app
   (tap Version ~10 times) → **Start head unit server**.
2. **Connect DHU from the workstation:**

```sh
adb forward tcp:5277 tcp:5277
"$ANDROID_HOME/extras/google/auto/desktop-head-unit" -a
# Optional: -i touch|rotary|hybrid   -c /path/to/dhu-config.ini
```

3. **Install Player** on that device (same debug/release APK used for phone
   acceptance), sign in against the music fixture (`adb reverse` as usual),
   then open Player from the DHU media launcher and repeat browse / search /
   play checks.

Emulator phone AVDs without Play Store cannot satisfy this path. A Play Store
system image still often ships stub gearhead until a real Auto update is
applied on-device; prefer hardware.

### When Automotive is enough

For MediaBrowser tree, search, queue, trusted-caller gating, and
`MediaSource` opt-in, keep using `Player_Automotive_API34` (or successor AAOS
AVDs). Record evidence under a dated directory next to
`player-m55-evidence`. Only open a new Auto regression on phone DHU when a
change is projection-specific or a reviewer explicitly requires gearhead.