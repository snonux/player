# Player Monorepo Agent Notes

This repository contains two components: `player-server/` and `player-android/`.

Use `player-server/AGENTS.md` for Go server, web UI, API, database, and deployment work.

Use `player-android/AGENTS.md` for Flutter Android client work.

Keep root changes limited to monorepo orientation, shared ignore rules, and cross-component documentation.

Do not commit local media, generated Android files, local databases, coverage files, or built binaries.

Prefer component-local commands from the relevant README before adding root-level tooling.

## Feature parity and verification

All features must work in the web UI in Google Chrome, the web UI in Mozilla
Firefox, and the native Android client app. Every new feature must be
implemented and verified on all three before it is considered complete.
Use controls appropriate to each platform, and include browser and Android
device or emulator checks that exercise the actual feature. Report any
verification gap explicitly; passing checks on one platform does not verify
the others.

## Shared release version

The server and Android client must always have the same X.Y.Z version.
Keep player-server/internal/version.go and the version name in
player-android/pubspec.yaml synchronized. Android's +N build number is
separate and must increase for every release.

Use python3 player-server/scripts/release-version.py X.Y.Z to bump both
components together. Even when only one component changes, release and build
both with the new version. Use one shared vX.Y.Z git tag and GitHub release;
the Release workflow creates the tag and publishes both the server bundle and
signed Android APKs. Never create separate server and Android release tags.
Update the Android F-Droid changelog before starting that workflow.
