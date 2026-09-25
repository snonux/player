# Player Android Agent Notes

This directory is the Flutter Android client for the Player server.

- Keep the scaffold simple until the app has real UI requirements.
- Do not add a state-management framework without a concrete need.
- Keep API client methods aligned with `../player-server/docs/api.md` and server routes.
- Android is touch and menu driven: keep actions easy to find and give controls
  clear labels and usable tap targets. The web client's vi-style keyboard
  preference does not apply to Android navigation.
