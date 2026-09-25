# Player Android

Flutter Android client scaffold for the Player server.

## Quickstart

When Flutter is installed, finish or refresh the generated Android project files:

```sh
cd player-android
flutter create --org zone.foo --project-name player_android --platforms=android --description 'Player Android client' .
flutter analyze
flutter build apk --debug
```

The REST API contract lives in [../player-server/docs/api.md](../player-server/docs/api.md).

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

The VIEW filter makes the app eligible to handle matching links. Automatic
opening on Android 12+ requires the HTTPS server to publish a matching
`/.well-known/assetlinks.json` for the app's signing certificate, or the user
to enable supported links for the app in Android settings. Without verification,
ordinary taps may open the browser. The Player server does not currently
publish this file.
