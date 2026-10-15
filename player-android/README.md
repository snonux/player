# Player Android

Android audio/video player for device files and the Player server.

The installed app version and build number appear at the bottom of **Settings**.

## Screenshots

| On this device | Empty library | Audio player |
| --- | --- | --- |
| <img src="../docs/screenshots/android-local-library.png" alt="Local library with audio and video files" width="240"> | <img src="../docs/screenshots/android-empty-library.png" alt="Empty library with Add files and Connect to server" width="240"> | <img src="../docs/screenshots/android-audio-player.png" alt="Audio player with seek bar and speed choices" width="240"> |

## Play files on this device

A fresh install opens **On this device**. No server, account, or network
connection is needed. Tap **Add files**, choose audio or video, then select a
file in Android's document picker. Tap its library entry to play it again.

Player retains permission to read the selected document and stores its resume
position locally. It does not copy the file. Audio keeps playing after Back or
Home, with Android notification and lock-screen controls. Video saves progress
when leaving the player or putting the app in the background. Replaying a
finished item starts from the beginning. Force-stopping the app can lose
progress since the last save.

Use an entry's menu for **Choose file again** or **Remove from library**.
Removal stops that item if it is playing and removes its local history and
retained permission; the original file stays on your device. If a file moves,
becomes unreadable, or cannot be decoded, the player may show **Retry**. Return
to the library to choose a replacement or remove the entry.

Formats depend on Android's decoders. MP3 audio and H.264/AAC MP4 video were
verified with the normal app on Android 14/API 34 using the Downloads provider.
Physical devices, other document providers, and removable storage have not
been verified. See the [verification results](docs/standalone-playback-verification.md)
for the exact platform checks and automated coverage.

Folder scanning, playlists, local tags, and syncing local
history across devices are outside this feature.

## Keep server files offline

Open a server file's details and tap **Keep offline**. Audio, video and images
are saved in Player's private storage. For formats Android cannot play directly,
Player downloads the server's converted playback copy. Keep the app open while
downloading; the action shows the byte count and lets you cancel. A failed or
cancelled download is never listed as ready. Downloading uses available device
storage without imposing a fixed movie-size limit.

Open **Downloads** from the server navigation menu or the download icon in
**On this device**. Downloads work after an app restart, without a server
connection or login. Saved copies are identified by server origin, account and
media ID, so matching IDs on another server or account stay separate. All copies
kept on this device remain accessible in Downloads, including after logout.
Resume positions for these copies stay on the device and do not sync to the
server. Offline images include SVGs supported by the same safety limits as
server images. Public share links cannot be kept offline from the app.

Use **Delete offline copy** in Downloads to stop that file's playback and remove
its downloaded bytes, local progress, notes, bookmarks and favorites. The remote media and files you added
through Android's document picker stay available. Uninstalling Player or clearing
its app storage removes downloads. An interrupted download is discarded when
Downloads next opens; it can be downloaded again from the server file's details.

See [offline verification](docs/offline-downloads-verification.md) for automated
coverage and the repeatable Android device test.

## Bookmarks, notes and favorites

In audio or video playback, tap **Bookmarks**, optionally enter a label, then
**Save current position**. The list shows each timestamp and label. Tap a saved
position to jump there, or its **Delete bookmark** button to remove it. Saved
positions are separate from automatic resume progress.

Tap **Notes** in playback to edit your personal note. Notes also appear in a
device file's menu and beside each download. Edits save automatically; Back
waits for the latest edit to save. A failed save keeps the draft open for retry.
Images use **Add to favorites** to save the whole item and also have notes.
Public shares do not expose account notes, bookmarks or favorites.

Server notes, bookmarks and favorites belong to the signed-in account and
are stored on that server. Position bookmarks require a server version that
provides the bookmarks API. Device files and offline copies store their notes,
bookmarks and favorites on this device, available without a login or network.
Offline copies from different servers or accounts stay separate; their personal
data does not sync to the server. Choosing a device file again preserves its
notes and bookmarks, even when starting its resume progress over. Removing a
device entry or deleting an offline copy also removes its personal data.

## Connect to a server

Tap **Connect to server**, select a saved server or enter another server origin
and save it. The app keeps a separate saved login for each HTTP/HTTPS origin
(including its port). Once a server is saved, every app start asks you to select
a server or **Use files on this device**. A fresh install still opens the local
library directly.

Create the first admin account if the server is new, or sign in to an existing
account. To reuse a saved login, tap **Unlock saved login with fingerprint** and
complete the device's biometric prompt, or enter your server account password
for a fresh sign-in. Device PIN/password fallback is disabled: the password
option is your server password. Cancelling or failing biometrics leaves the
login locked; password sign-in also works without biometric hardware. The local
library remains available without a network connection. Saved bearer tokens live
in Android's secure storage; account passwords are never saved. Switching servers locks the current
login and preserves each server's saved credential. Logout removes the selected
server's credential. Expired/revoked tokens require password sign-in.

Credential gating and background audio integration are covered by automated
client checks. Biometric hardware behavior has not been verified on a physical
device. Other enrolled biometrics may be offered alongside fingerprint.

The server library retains streaming, server progress, and public shares. Open **On this
device** from the server navigation menu to return to local files.

Switching libraries stops the current playback session. Browsing away from the
audio player within the same library keeps audio playing. Local settings remain
available without a connection. Server logout or expired credentials preserve
local files and history; a delayed server error cannot stop a local session.

Some formats (for example WMV, FLV, WMA, or AC-3 audio) cannot be decoded by
Android. The server converts those, and the app plays the converted copy. While
it checks for that copy, the player shows **Preparing playback…**; the first
play of a long file can take minutes, later plays start right away. Whatever was
playing before stops as soon as preparation starts. Leaving the player while it
is still preparing cancels the wait (the server keeps converting, so opening the
item again continues where it left off). If the file cannot be played, the
player names it and the reason, with a **Retry** button. Files in **On this
device** are always played directly; no server is involved.

## Lyrics

In the server audio player, tap **Lyrics** to show available plain or timed
lyrics. Timed lyrics keep the current line visible as playback moves or seeks;
tap a line in the scrollable list to seek to it. Lyrics are also available for public audio shares.
Missing lyrics or a failed lookup leave playback available; **Retry lyrics**
tries the lookup again. Changing tracks clears the previous lyrics.

Lyrics come from the server's sidecar files or embedded audio metadata.
**On this device** does not read lyrics or adjacent sidecar files: selecting
an audio document only grants access to that document.

## SVG images

SVG files from the server are shown in the image viewer and as thumbnails.
The app accepts simple vector drawings only and treats every SVG as
untrusted, because one can arrive through a public share link.

Supported: paths and basic shapes, groups, gradients, clip paths, `<use>`
of plain content, and short text. Not supported, and shown as "Image
unavailable" instead of being drawn wrongly: style sheets, patterns,
embedded bitmaps, masks, filters, dashed strokes, blend modes, markers,
symbols, animation, references outside the file, and drawings beyond the
size and complexity budgets in `lib/services/svg_limits.dart` (for example
more than 2,000 elements, 256 KB of path data or 1 MB in total).

A drawing is turned into a bitmap once, sized for the place it is shown in
and never larger than 2048 pixels a side. Zooming in the viewer enlarges
that bitmap; it does not redraw the vectors.

Making the bitmap is the only expensive step. To bound it, the app counts
the drawing operations of a file (fills, strokes, gradients, layers,
characters of text, how often outlines cross the drawing, and extra for
anything drawn under a clip path) and chooses the bitmap so that operations
times pixels stays within a fixed budget. A drawing with many operations is
therefore shown at a lower resolution, down to 256 pixels, and one that is
over budget even then (about 4,600 weighted operations) is not shown. Clip
paths must be simple shapes; a clip with a long, winding outline is
refused, because everything drawn under it becomes many times slower.

The weights behind that count are measured, not proven. With the software
rasteriser used by `flutter test`, which is slower than a phone's GPU, the
slowest accepted cases in `test/services/svg_hostile_input_test.dart` are:
970 oversized glyphs, 0.55 to 0.9 s for a full-screen bitmap and 0.1 s for
a list thumbnail; 51 full-screen repeating radial gradients at full
resolution, 0.5 to 0.65 s; 700 full-screen rectangles under the most
complex clip that is accepted, 0.35 to 0.5 s (0.15 s as a thumbnail). Every case
there must stay under 2 s. That time is spent once per drawing and size. A
file that the weights misjudge could still take longer.

## Quickstart

When Flutter is installed, finish or refresh the generated Android project files:

```sh
cd player-android
flutter create --org zone.foo --project-name player_android --platforms=android --description 'Player Android client' .
flutter analyze
flutter test
flutter build apk --debug
```

The REST API contract lives in [../player-server/docs/api.md](../player-server/docs/api.md).

The [standalone local playback design](docs/standalone-local-player.md) records
the implemented scope and its acceptance results.

## Install from F-Droid

Add the [snonux F-Droid repository](https://github.com/snonux/fdroid) to the
F-Droid app, then install Player. Releases contain separate APKs for
armeabi-v7a, arm64-v8a, and x86_64, so each device downloads only its ABI.

Releases are built from `vX.Y.Z` tags by
[`release.yml`](../.github/workflows/release.yml). The tag must match the
version name in `pubspec.yaml`; increment its build number for every release.
The server uses the same version and tag, even when only one component changed.
Use the [shared release command](../README.md#releases) to bump both together.
The APKs use the release keystore supplied through GitHub Actions secrets.
See [the release setup guide](docs/fdroid-release.md) for the one-time setup.

To build the same split APKs locally:

```sh
flutter build apk --release --split-per-abi
```

Without `android/key.properties`, local release builds use the debug key and
cannot update an app installed from F-Droid.
The published APK does not claim a public share-link host. Enter your own
server address in Settings after installation.

## Share links on Android

Build with `--dart-define=PLAYER_BASE_URL=https://player.example.com` to register
that server's `/s/{token}` share links as Android VIEW intents. The default
debug build registers `http://10.0.2.2:8080` for the local emulator server.
The app always loads public shares from the build-time origin, even if the
server address in Settings changes later. A new build is needed to claim links
for a different hostname or scheme; a non-default explicit port is matched
exactly. The default HTTP and HTTPS ports are normalized to match generated
share URLs without an explicit port.
If Android forwards a link with the same host but a different port, the app
shows a server-mismatch message before sending the share token anywhere.

Opening a share in the app is one "viewing" and uses the link once, however
long playback takes: the app fetches the share's metadata a single time and
plays the URLs from it, which carry the server's viewing credential
(`?view=…`). Opening the same link again later (or tapping Retry after an
error) is a new viewing, so a link limited to one use can be opened once.
App versions up to 0.2.2 do not know the credential: with a current server
they need one use to open the link and one more for every request their
player makes, so links with a use limit may stop early there.

The VIEW filter makes the app eligible to handle matching links. Automatic
opening on Android 12+ requires the HTTPS server to publish a matching
`/.well-known/assetlinks.json` for the app's signing certificate, or the user
to enable supported links for the app in Android settings. Without verification,
ordinary taps may open the browser. The Player server does not currently
publish this file.
