# Standalone local playback plan

Task: `zn2`. Status: implementation proposal, 2026-10-01.

## Outcome and scope

Player should open and play audio and video stored on an Android device without
a server, account, or network connection. Keep the server client available in
the same app. The first release should offer an **On this device** library,
**Add files**, recent items, resume positions, and the existing audio background
playback, notification, seek, and speed controls. Video uses the existing
Chewie controls. Local settings and library remain accessible when a configured
server is unavailable or credentials expire.

Start with explicitly selected files. Folder scanning, MediaStore discovery,
playlists, image browsing, server downloads, uploads, local tags/notes, and
cross-device progress synchronization are later work. Local media remains on
the device; removing an entry removes its library record, not the original file.
Supported formats depend on device decoders; no server transcoding is available.

## What needs to change

The current code already has Riverpod, SQLite, just_audio, audio_service, and
video_player. Reuse those dependencies and keep the server API contract intact.

| Existing area | Coupling to address |
| --- | --- |
| `lib/main.dart` | Always restores server auth and initializes the server progress queue before rendering. |
| `lib/providers/settings_provider.dart` | Missing settings resolve to the emulator server URL; absence of a configured server is not represented. |
| `lib/router.dart` | Most routes require auth; `_RouterRefreshNotifier` subscribes to `firstRunProvider`, which calls `countUsers()`. |
| `lib/screens/audio_player_screen.dart` | Reads API and credential providers even with an explicit source URL; progress normally uses a numeric server media ID. |
| `lib/screens/video_player_screen.dart` | Uses a network controller, server metadata/progress, and periodic server-queue writes. |
| `lib/services/audio_handler.dart` | Already accepts progress callbacks and owns their lifetime independently of the screen. |
| `lib/providers/auth_state_provider.dart` | Unauthorized cleanup instantiates the server queue and stops the shared audio handler. |
| `android/.../MainActivity.kt` | Preserves `AudioServiceActivity` integration and validates incoming share URLs against the build-time origin. |

## App startup and navigation

Persist an optional server origin and the last library destination (`local` or
`server`). Fresh installs open the local library immediately, with **Add files**
and **Connect to server** actions. Global settings expose appearance and server
configuration without requiring login. A visible library selector lets existing
server users reach **On this device**.

Migration rules:

- Preserve a valid saved `server_base_url` and existing credentials; retain the
  server destination for those users.
- For legacy installs without a saved URL but with a persisted auth origin,
  preserve that valid origin so default/build-time server users are not stranded.
  An expired credential still preserves the configured origin, requiring login.
- Otherwise start locally with no configured server. A build-time URL may
  prefill Connect to server, but must not initiate requests automatically.
- Invalid saved settings fall back to local mode with an editable configuration
  error. Persist migration atomically or idempotently and cover restart midway.

Resolve settings before deciding which providers to initialize. The local path
must not construct Dio clients, restore server auth, subscribe to server
first-run checks, or initialize the server progress queue. Gate both router
redirects and refresh subscriptions, not just the initial route. Initialize
server services lazily on entering the server library. Its existing login and
bootstrap behavior remains, with a usable return to local files during errors.

Add explicit local library/player routes using a durable local ID. Resolve
records from storage so restoration works without `state.extra`. Keep server
routes protected. Public shares keep their existing build-time origin policy
and are an explicit network action, independent of local/server selection.

Switching libraries finalizes and stops the current playback session before
opening the other destination. Merely browsing away from a local audio player
continues background audio. Suspend server synchronization when entering local
mode; preserve queued server data under its existing origin/user scope and
resume only for the matching session. Explicit logout/server replacement keeps
the existing server cleanup semantics. Late 401 responses must not redirect a
local screen, stop local audio, or erase local records. Keep remote cleanup
working, but scope navigation and playback side effects to server sessions.

## Selecting and retaining files

Use Android's Storage Access Framework via a small injectable Dart interface
and Kotlin method channel. Start with `ACTION_OPEN_DOCUMENT`, `CATEGORY_OPENABLE`,
audio/video MIME filters, optional multiple selection, and `EXTRA_LOCAL_ONLY`.
This avoids broad storage access and confines selection to documents chosen by
the user. Preserve `AudioServiceActivity` and existing share-intent checks.
External **Open with** intents are deferred; this picker must not introduce a
generic VIEW filter that bypasses the current share validation.

Request read access and take the returned persistable read grant before adding
the record. Treat a content URI as an opaque identifier, never as a filesystem
path. Read display name, MIME type, and optional size through ContentResolver;
unknown size/duration is valid. Cancellation leaves the library unchanged.
Deduplicate exact URIs; distinct provider URIs for the same bytes may remain
distinct. If persistence is unavailable, explain that the file cannot be kept
and leave it unindexed in this first version.

Track grants acquired by an import so cancellation or database failure releases
only newly acquired, unreferenced grants. Removing a record stops it if active,
removes its progress, and releases its grant when no record/playback uses it.
Moved, deleted, or permission-revoked files show **Choose file again** and
**Remove from library**. Relinking preserves the local ID; confirm whether to
keep resume position if the replacement is a different file. Never delete the
source document. Provider errors or unavailable removable storage are recoverable.

Android documents persistent grants and their limits in its
[shared-document guide](https://developer.android.com/training/data-storage/shared/documents-files).
The [Intent reference](https://developer.android.com/reference/android/content/Intent)
defines the picker flags, including local-only selection. A provider can still
fail while opening a selected file; actual offline playback must be verified.

## Media identity, playback, and persistence

Introduce a small typed playback request with distinct `server`, `local`, and
`publicShare` variants. Each resolves its source, display metadata, resume
position, and progress callbacks before shared controls load it. Do not model
local playback as a public share or invent server IDs such as zero/negative IDs.
Keep the existing server `Media` JSON model and API client server-specific.

Use `local:<id>` for local media-session identity; server identity must include
origin, user, and media ID. The handler tracks the active source kind and a
session generation. Async metadata/source/resume completion checks that
generation so an older load cannot overwrite a newer selection or restart a
stopped session. Apply this to video controller initialization/disposal too.

Audio should pass the content URI to just_audio with no HTTP headers. Video
should use `VideoPlayerController.contentUri`, not `networkUrl`. First validate
both with the repository's resolved plugin versions and real Android providers.
Flutter's [video player source](https://github.com/flutter/packages/blob/main/packages/video_player/video_player/lib/video_player.dart)
exposes a content-URI constructor, but this is not proof of device compatibility.
The first implementation step below is a compatibility gate; do not silently
copy entire files into memory or a cache if URI playback fails. A bounded,
disk-backed import would be a separate fallback design with storage limits and
cleanup rules. No decoder/plugin upgrades are assumed by this plan.

The emulator compatibility check and its limitations are recorded in
[`content-uri-compatibility.md`](content-uri-compatibility.md). Physical-device
and removable-storage checks remain open.

Store local records in a separate SQLite database using the existing sqflite
dependency. A minimal schema contains `local_media` (stable generated ID,
unique URI, display name, MIME type, optional size/duration, added/last-opened
timestamps) and `local_progress` (local ID foreign key, position, finished flag,
updated timestamp). Use transactions for import/removal, versioned schema
migrations, finite nonnegative progress validation, and seek clamping once
duration is known. Treat unknown duration as unfinished until actual completion.

Reuse `PlayerAudioHandler.startProgress` callbacks and its session helper for
local writes. Save position periodically, on pause, source replacement, stop,
and lifecycle transitions where possible; process death may lose the last
interval. Video needs equivalent final saves as well as its ticker. Capture
the old source's identity before asynchronous writes and serialize updates so
late ticks cannot resurrect removed progress. Replaying a completed item starts
at zero and clears completion. Local storage never uses `ProgressQueue` and
must survive login, logout, server changes, and server queue cleanup.

## Implementation sequence and acceptance

These are proposed implementation slices, not additional tasks started by this
planning task. Execute them in order; each adds its own focused regression tests.

1. **Validate Android document playback.** Exercise real persisted content URIs
   with the locked audio/video plugins, pause/seek, background audio, process
   restart, and revoked access. Check internal storage and removable storage
   where available. Record versions/device results and decide on a fallback
   before committing to the source adapter. No mock-only compatibility claim.
2. **Make server setup optional.** Change settings/migration, startup wiring,
   router guards/subscriptions, and settings navigation; add the local empty
   library. Acceptance: fresh offline launch renders immediately with zero
   server requests; existing configured accounts and public links still work.
3. **Add the document library.** Implement picker bridge, durable grants, local
   database/repository, list, import, relink, and removal. Cover picker cancel,
   duplicate URI, grant/DB failures, absent metadata, and revoked/missing files.
4. **Connect audio/video playback and local progress.** Add typed source
   adapters and shared controls, isolate auth side effects, and implement
   final-save/session ownership rules. Verify local resume after restart and
   no credentials or local progress reaching server APIs. Preserve remote and
   public playback regression tests.
5. **Verify complete user journeys and document usage.** Test fresh install,
   legacy migration, airplane mode, restart, background/lock-screen audio,
   server/local switching during an in-flight load, delayed 401, logout during
   local playback, unsupported/corrupt media, and removed storage. Run the full
   Flutter unit/widget suite, analyzer, and debug APK build. Exercise platform
   picker/grants and decoding on Android; unit/widget tests alone cannot cover
   these. Update README with the shipped behavior and any proven format limits.

No server API or deployment changes are required. Completion of this document
does not claim these future behaviors have been implemented or device-tested.
