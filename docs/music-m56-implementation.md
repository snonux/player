Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; Kotlin/launcher/platform acceptance remains pending.

# M56 Android launcher playback widget (8l3)

Implemented, awaiting fresh review and integrated verification. Predecessors
nj3/oj3/qk3 supply existing playback ownership, queue and rich identity. No server,
web playback, release or version changes. Reports/research notes are untouched.

## Interaction specification

Root approved listener-only implementation before feature edits. Android's
launcher widget picker exposes **Player playback** without configuration or new
permissions. A single widget resizes horizontally/vertically (default 4×2 cells,
minimum 180×132dp). Header: 52dp cover when width permits, bold 16sp single-line
title, 13sp artist · album (two lines, ellipsized). Below: centered 48dp Previous,
Play/Pause, Next, each with explicit screen-reader labels. Under 250dp available
width the cover hides and text keeps the space. The complete identity receives
an accessible description. Header opens existing Player activity; it does not
reload a source or claim playback. Background uses native light/dark colors
matching Player's slate palette, with high contrast text and transparent touch
feedback. Disabled transports dim and disable. No drag gesture is required for
playback. Android launcher placement/resizing follows native launcher controls.

State transitions: no active source → **Open Player to choose music**, neutral
art and disabled controls; preparing/buffering → **Loading…**, current identity;
ready → artist/album and current Play/Pause; failure → **Open Player to retry**,
transports disabled, header opens app for existing Retry; next track → fresh
identity and art or immediate placeholder; account/source stop → neutral state.
Offline cached/local playback retains existing handler behavior; network failures
use the same error path. No extra permission prompt or competing queue UI.
Long text ellipsizes while the accessible identity includes both lines.

## Lifecycle and boundary implementation

A Dart observer listens to the existing audio handler's mediaItem/playbackState,
deduplicates widget snapshots, and sends only display identity, private artwork
path and advertised controls. No source URL, credentials, account IDs or saved
queue are sent. Native engine plugin keeps state only in memory, bounds artwork
to an app-cache canonical path and 5MiB file, samples decoding to at most 256px,
and publishes RemoteViews updates to every widget. State-only updates reuse that
small bitmap. No asynchronous artwork worker can publish an obsolete generation.
Art disappearance or metadata removal replaces the previous bitmap immediately.
MainActivity registers the plugin on the shared engine; it is not activity-owned.
Normal background updates continue with the existing audio service engine.

Explicit auth and server-provider listeners refresh the observer independently
of playback events. Server and account-owned offline metadata are allowed only when the active
request's origin/user matches current providers and the credential gate is enabled.
Offline requests carry their download repository owner; absent owner fails closed. Existing
authentication paths stop the source and clear the retained queue. Device-document and public-share playback
are independent of sign-in. Private downloaded playback may remain locally
available, but launcher identity and transports hide after account switch/lock. The widget does not persist metadata/artwork and clears
on engine detach. System periodic update (30 minutes), resize and placement draw
only live in-process state; after process death they draw neutral state. Android
hosts may retain an old RemoteViews bitmap until update/tap after an abrupt kill;
no persisted snapshot or cold background resume is claimed.

Explicit immutable PendingIntents target this non-exported provider and existing
activity. Every transport delivery rechecks live in-memory advertised support
before forwarding a media-button event to the existing audio_service receiver.
Old PendingIntents cannot restore stale playback in a fresh process; they clear
the view and open Player. No session reimplementation or audio-core edit.

Reference APIs checked against Android's [App widgets documentation](https://developer.android.com/develop/ui/views/appwidgets)
and installed audio_service 0.18.18 Android MediaButtonReceiver/AudioService source.
The receiver uses AndroidX MediaButtonReceiver; its existing browser service and
session route Next/Previous/media click into the established Dart handler.

## Checks and deferred acceptance

Passed: `flutter test test/services/playback_widget_observer_test.dart` (5 tests),
`flutter analyze --no-pub lib/services/playback_widget_observer.dart lib/services/playback_request.dart lib/services/offline_download_repository.dart lib/main.dart test/services/playback_widget_observer_test.dart`
(no issues; 1.5s), Dart formatting. A test fixture initially passed a string
for the existing integer localMediaId; corrected to integer before the five-test
passing run. Covers identity/art replacement, advertised controls,
auth invalidation without a playback event, offline A→B/server switch/lock/logout,
unknown offline owner, retained device-local/public identity, loading/error/null metadata and refusal
to send network artwork URLs. XML well-formedness and whitespace checks performed.
These are implementation checks, not launcher or platform proof. No Kotlin/APK
build, full suites or real Chrome/Firefox/Android acceptance ran for this slice.

Integrated acceptance must place/resize the widget on the actual emulator launcher,
exercise previous/play/pause/next and distinct audible tracks/art, background it,
change account and source, log out/lock, exercise loading/failure/offline retry,
restart/reboot/abrupt process death, remove/re-add, test missing/oversize artwork,
and inspect original light/dark/narrow/TalkBack views. Check live metadata and saved
progress/audio, not just successful broadcast delivery. Chrome and Firefox have no
launcher widget; verify their shared playback controls and source/account clearing
remain coherent during the integrated regression phase. Feature stays unaccepted
until applicable native and shared browser checks pass.
