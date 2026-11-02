# Android releases for the snonux F-Droid repository

The [snonux F-Droid repository](https://github.com/snonux/fdroid) imports
Player's signed APKs from GitHub releases. The APKs keep Player's signing key;
the F-Droid repository signs only its index. It imports the store text and icon
from `fastlane/metadata/android` at the same tag.

## Signing setup

The first release needs a dedicated Android release keystore. Keep the original
keystore and password in a safe backup: losing them prevents existing installs
from receiving updates. The keystore and `android/key.properties` must remain
outside git. The workflow expects these repository secrets in `snonux/player`:

| Secret | Value |
| --- | --- |
| `ANDROID_KEYSTORE` | Base64 of the release keystore |
| `ANDROID_KEY_ALIAS` | Alias in that keystore |
| `ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_PASSWORD` | Key password |

For a local signed build, create ignored `android/key.properties` with
`storeFile`, `storePassword`, `keyAlias`, and `keyPassword` properties. The
keystore path may be absolute. Without this file, Flutter builds a debug-signed
release APK for development only. The release workflow always requires the
four secrets and checks the APK signatures before upload.

## Cut a release

1. From the monorepo root, run
   `python3 player-server/scripts/release-version.py X.Y.Z` to update both
   components and increment Android's build number. Both version names must
   match the shared tag `vX.Y.Z`.
2. Update the F-Droid changelog in
   `fastlane/metadata/android/en-US/changelogs/default.txt`.
3. Commit and push the changes to `main`, then run
   `gh workflow run release.yml --ref main -f tag=vX.Y.Z`.
   The workflow creates the shared tag; do not create or push the tag yourself.
4. The [release workflow](../../.github/workflows/release.yml) builds the server
   bundle and three signed APKs with `--split-per-abi`, attaching all four to
   the same release.
   Their names are `player-vX.Y.Z-<abi>.apk` for `armeabi-v7a`, `arm64-v8a`,
   and `x86_64`.
5. The F-Droid repo imports the release on its next six-hour run. To refresh it
   immediately, run `gh workflow run publish.yml -R snonux/fdroid`. An optional
   `FDROID_DISPATCH_TOKEN` repository secret can trigger the refresh from the
   release workflow.

The release APK opens the device library without a configured server. The
local-emulator URL can prefill server setup but does not trigger requests on
local startup. It does not claim a public share-link host. Users enter their own
Player server address in Settings. Android share-link handling needs a build tied to
a specific public host and that host's Digital Asset Links configuration.
