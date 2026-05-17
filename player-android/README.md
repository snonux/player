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
