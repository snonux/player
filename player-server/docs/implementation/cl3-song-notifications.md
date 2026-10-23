Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; BFCache lifecycle blocker fixed, R2 clear.

# cl3 — optional desktop song notifications

Implementation stage only; awaiting integrated platform verification and bounded review. Depends on completed U01/nj3, V01/oj3 and M54/qk3. No API or Android behavior changes.

Design follows U01 §5.6 and M70: Settings → Notifications, between navigation and appearance, uses the current modal typography, theme tokens, wrapping text, button and focus restoration. No new shortcut. Native Tab/Enter and pointer/touch activate the same button; Escape uses the existing modal hierarchy.

```
Settings                                            Close
…
Notifications
Optional song announcements when the player window is in the
background. Enabling asks for browser permission. Track names
may appear on your desktop or lock screen.
[Enable song notifications]
Off. Enable to announce songs only while the player window is
in the background.
…
```

Enable → browser permission request → On / dismissed / blocked. On replaces the same button with Disable; Disable closes any visible announcement. Blocked explains browser site settings; unsupported/insecure contexts disable Enable with an explanation. Storage failure explains that enabling could not be saved. Controls remain in place during permission requests and expose loading through their label plus status text. Long metadata is capped in the OS message; page explanation wraps naturally with no new fixed widths or colors. Existing light/dark tokens apply. Actual light/dark/narrow rendering and keyboard focus journeys remain unverified.

Preference defaults off and is scoped to this browser/origin and account. No artwork, stream URL or credential is included in a notification. The owner window announces only confirmed audio `playing`; foreground starts are consumed so hiding later does not produce an old announcement. Resume, buffering and duplicate events do not repeat the same song. An ID-only shared consumed-track key avoids repeating a song during detach/reattach. The next different song replaces the prior announcement silently. Before exposing title/artist/album, the controller checks the still-active source, visibility, opt-in, permission and current authenticated account. Notification errors never block playback. A track change cancels old announcement work without cancelling the user’s pending permission request. Disable takes effect in the current window even when storage cannot save it, with an explicit persistence warning. Logout invalidates pending callbacks and clears announcements in other same-origin player windows. Page exit also clears the owning window's notice. A persisted BFCache exit cancels pending announcements and closes its notice while retaining the document's account preference, so browser Back remains usable; restored announcements still recheck the current authenticated account. Normal exit clears the account. Cast uses confirmed remote-playing callbacks; Firefox has the normal browser implementation without assuming Cast support.

Android already has the foreground-service playback notification, lock-screen/media controls and handler ownership. This task preserves that existing integration rather than adding an unrelated desktop opt-in screen to Android. Native notification changes on next/previous and unchanged Back/background controls require integrated emulator checks.

Cheap checks run during implementation:

- `node --experimental-default-type=module player-server/web/js/tests/song-notifications.test.js` — PASS: default off, explicit permission, deny/dismiss, hidden-only announcements, same-song dedupe, replacement, ownership transfer, account preference separation stale authentication response, permission prompt surviving track changes and fail-closed Disable when persistence fails, and production lifecycle wiring for BFCache pagehide/pageshow (including pending cancellation and account-change/logout privacy).
- `node --experimental-default-type=module --check player-server/web/js/{playback,app,detachPopup}.js` — each file checked separately, PASS.
- `git diff --check` — PASS.

These are coherence checks, not runtime proof. Deferred integrated acceptance must exercise installed Chrome and Firefox permission grant/deny/dismiss, foreground/background track changes, pause/resume/seek/buffering, failed track without an announcement, Disable, reload/account switch/logout, storage unavailable, detached ownership and Cast where available. Review light/dark/narrow Settings, native text/control keys and Escape focus return. Check actual OS notifications and actual playing source/audio; browser support alone is not acceptance. Native Android needs its existing playback notification/next/previous/background regression journey. Full suites, final builds and screenshots are deferred under the implementation-first workflow.

Review: R1 found one BFCache lifecycle blocker, corrected with persisted-page suspension and a focused lifecycle regression. Narrow self-review of that fix found no further blocker. Actual browser BFCache behavior remains part of integrated verification.
