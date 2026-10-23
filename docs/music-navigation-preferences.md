# Music navigation preferences (rk3 / M69)

Implementation stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded review rounds completed; delayed native landing blocker fixed.
Uses U01 (nj3) navigation and Settings surfaces, V01 (oj3) acceptance helpers,
and existing M02/M03/M26 catalogs (yj3/zj3/3k3).

Pre-code layout/state plan: existing web Settings modal and native Settings
receive Navigation beside Appearance: Initial collection selector (Library,
Albums, Artists, Playlists), three labeled Show … shortcut switches, and Reset
navigation. All music views always exposes every catalog, including hidden ones.
Controls use existing theme tokens, native selects/switches and normal modal
focus/typing/Escape behavior; no new shortcuts. Native keeps its ordinary touch
and Back controls. This extends navigation presentation, never authorization.
Preferences are device-local, explicitly shared across accounts on the browser
origin/native installation, and require no server round trip or permission.

Changing visibility keeps the current view and playback. Hiding the initial
collection resets the default to Library; choosing a hidden collection shows
its shortcut. Reset affects navigation only. Missing/corrupt values fall back
to supported defaults. Save failures have inline web status/native snackbar;
native rolls back to saved values. Preference loading disables native controls.
Catalog empty, offline, denied and loading states remain their existing states.
Long labels wrap with existing controls; layout uses the existing responsive
Settings grid and scrollable native Settings screen.

Web default applies on a bare cold entry, after URL/queue restoration, without
changing the recovered paused queue. Query/hash links, browser history entry,
and Back during startup suppress the default. Browser Back/popstate never
reapplies it. Native applies once after authenticated server destination and
preferences settle, pushes the collection above Library, and retains Library
underneath for Back. Explicit routes, setup/local destinations and subsequent
Library visits bypass this default.

Integrated checks still required: installed Chrome/Firefox and native emulator,
light/dark/narrow Settings, keyboard/select/checkbox typing and focus return,
hide currently open and initial catalogs, reset, cold restart, corrupt/stored
preferences and storage failure, explicit album/playlist/media links while
hidden, Back to Library without bounce, local library/podcast paths, restored
paused queue, and account scope labeling. Unit/widget checks are not platform
acceptance. No final build, full suite or platform acceptance is claimed here.

Focused implementation checks: `node web/js/tests/navigation-preferences.test.js`
and `node web/js/tests/navigation-url.test.js` (from player-server) passed;
`node --check` app.js/navigation-preferences.js/sw.js passed. SW cache v49
retains the existing asset list and adds the new module; all asset paths exist.
`flutter test --no-pub test/widgets/navigation_settings_test.dart` passed 3 tests
(persistence/fallback, controlled selector rollback on save failure, hide/reset).
`flutter test --no-pub test/screens/sets_list_screen_test.dart` passed 23 tests,
including initial catalog push/Back without bounce, hidden catalog access through
All music views, plus existing drawer navigation. Scoped Dart analysis of the
changed provider/widget/home/settings/router and tests: no issues. `git diff
--check` passed. Native cold-router explicit-link/device-destination boundaries
still need integrated acceptance; the focused Home test does not prove those.

Review round 1 found that GoRouter 14.8.1 skips rebuilding Home on an unchanged
route after async preference/auth resolution. The cold landing now travels
through a router-owned notifier observed by Home, with an explicit frame for
the post-frame push. Home consumes the value before pushing, so reconstructing
Home later cannot replay it. Listeners are removed on widget replacement and
dispose. Actual `routerProvider` regressions delay both auth and navigation
preferences, verify the visible album route, Back, refresh and replacement via
`go` away/from Home, and verify explicit route selection during startup wins.
The top route is asserted through `router.state.uri`, since GoRouter deliberately
retains the underlying URL in its route-information provider for imperative push.
