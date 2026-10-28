# Player Monorepo Agent Notes

This repository contains two components: `player-server/` and `player-android/`.

Use `player-server/AGENTS.md` for Go server, web UI, API, database, and deployment work.

Use `player-android/AGENTS.md` for Flutter Android client work.

Keep root changes limited to monorepo orientation, shared ignore rules, and cross-component documentation.

Do not commit local media, generated Android files, local databases, coverage files, or built binaries.

Prefer component-local commands from the relevant README before adding root-level tooling.

## Shared UI design principles

Apply these principles to every Android screen and web view, including dialogs,
players, settings, and administration. Aim for a snappy, uncomplicated listening
and browsing experience: KISS, less is more, and content comes first.

- Aim for at most five visible primary buttons per screen where practical.
  Consider the whole visible screen, including navigation, player chrome, and
  repeated row actions. This is a design target, not a reason to hide essential
  playback controls, form fields, or accessible navigation.
- Preserve every feature when simplifying a screen. Put less-used and advanced
  actions in a clearly labeled hamburger menu, with logical groups or focused
  subpages. Keep frequent actions immediately reachable; avoid duplicate
  buttons and multiple competing menus for the same action.
- Keep playlists, queues, and other lists compact. Prioritize titles, concise
  metadata, and playback status; use small artwork and expandable descriptions
  or details. Avoid repeating full action toolbars on every row. Reclaim space
  through layout and disclosure, not tiny text or smaller touch targets.
- Use consistent labels, icons, and menu placement across screens. Distinguish
  navigation from item actions. All features must remain discoverable even when
  optional shortcuts are hidden; do not bury ordinary music navigation in
  Settings. Group advanced settings into focused categories.
- Design for comfortable touch use and thumb reach on Android and narrow web
  layouts. Preserve at least 48dp Android and 44 CSS pixel web touch targets,
  readable text, visible focus, accessible names, and keyboard navigation.
  Do not make actions available only on hover or through an unlabeled gesture.
- Match controls to context: keep core music transport prominent, and retain
  convenient seek/resume controls for podcasts and video. Separate destructive
  actions from routine playback and preserve confirmation where appropriate.
- Keep interaction responsive. Avoid blocking browsing or playback on optional
  metadata, artwork, or background work. Show clear loading, progress, empty,
  error, and retry states without filling the screen with technical detail.
- Verify UI changes in Chrome, Firefox, and the native Android app. Exercise
  small screens, landscape, long titles, enlarged text, and relevant offline,
  read-only, and error states. Check that menus retain every previous action,
  lists leave useful room for content, and primary journeys remain quick.
  Measure responsiveness before claiming a performance improvement.

## Feature parity and verification

All features must work in the web UI in Google Chrome, the web UI in Mozilla
Firefox, and the native Android client app. Every new feature must be
implemented and verified on all three before it is considered complete.
Use controls appropriate to each platform, and include browser and Android
device or emulator checks that exercise the actual feature. Report any
verification gap explicitly; passing checks on one platform does not verify
the others.

Between broad regression gates, run focused checks for the feature just
implemented on each platform. After every 10 completed tasks, run an overall
end-to-end regression and fix failures before continuing. Parallelize
independent task work and compatible checks when resources permit. Run at most
two Android emulators at once; use one when CPU or memory is under pressure.

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
