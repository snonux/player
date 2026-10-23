# M08 music filter facets

Approved pre-code interaction contract (vk3, baseline 2651e66).

Albums and Music search show a compact Genre and Year/decade row followed by
More filters. More exposes Role, Release type, Compilation (Any / Marked /
Not marked), Mood, Grouping and Label. Active removable chips and Clear filters
remain visible above results. Controls wrap on narrow screens and use existing
Player theme tokens. Native selects own typing/arrows; no new global shortcuts.
Escape closes More before existing navigation. Applying retains control focus;
opening a result and Back restore filters and selection.

Android uses a labeled Filters button with active count and visible removable
chips. Its scrollable native sheet edits a draft; Apply saves, Clear resets the
draft, Back discards. Filters persist per saved server/account across restart
and remain separate on server switches. Native touch targets retain Material
sizes; long labels expose full semantics. Existing mini player/queue stay present.

Dimensions combine with AND on the same authorized song; repeated values within
a dimension combine with OR. Values use exact Unicode case/trim normalization,
retaining punctuation. Year and inclusive decades use indexed music.year, not
release date. Unknown years do not match. Compilation false/absent means Not
marked. Four existing M03 roles are reused. Options derive from the full
ACL-authorized Music catalog before active filters, without per-option counts.
Album/artist result counts describe matching songs; opening or playing a detail
uses the full authorized collection. Search song playback uses matching songs.
Empty search text plus facets browses; generic file filters remain separate.

Loading, empty and error states retain chips. No matches offers Clear filters;
connection errors offer Retry. Offline server catalog uses the existing
connection-error flow. Missing or forbidden details retain unavailable messages.
Unknown text values give empty matches; invalid year/role/compilation returns
400. Web URLs use namespaced facet keys and survive refresh/back.

Implementation and focused checks pending. Integrated Chrome, Firefox and
native Android acceptance, light/dark/narrow screenshots, cold restart and ACL
journeys remain pending; unit checks do not establish platform acceptance.

Approved screen layouts (annotations identify the actual controls):

```text
Web Music · Albums / Search
[A Genre: Any v] [B Year / decade: Any v] [C More filters >]
[D Genre: Ambient x] [D Year: 2021 x] [E Clear filters]
[F 1 matching album / matching song counts]
[existing artwork/card or search result rows]
[existing playback chrome]
```

A/B apply immediately through native selects. C retains its open state while
combining less common facets. D removes one dimension; E clears all metadata
facets. F uses matching counts; Enter on a collection opens its full authorized
detail. Back restores the filtered catalog. The control that changed keeps focus
through refresh. Search accepts `genre:` and `year:` without affecting generic
file modifiers; an active facet with empty search text lists matching music.

```text
Android Music · Albums / Artists / Search
[existing AppBar and Back/search/queue actions]
[A Filters (2)]
[B Genre: Ambient x] [B Year: 2021 x] [Clear filters]
[matching collection counts and existing cards/results]
[existing Music mini player]

A -> Music filters (scrollable native bottom sheet)
     Genre / Year or decade / Role / Release type / Compilation /
     Mood / Grouping / Label (only available or active dimensions)
     [C Clear]                         [D Apply]
```

Sheet Back discards the draft. C clears only the draft; D persists/applies it.
The applied row remains visible during loading, errors and no matches. The
legacy Artists role control stays available when a sheet role is not applied.
Preference identity is `music_facets:<normalized server origin>:<user id>`.
A new account restores its own values; malformed local preferences restore an
empty filter. Storage failures are reported visibly and do not discard the
current applied session values.

Verification handoff: focused Go Music service/API checks passed, including
same-song AND, repeated-value OR, literal punctuation, marked/unmarked and invalid
structured values. Scoped Flutter analysis passed; album/search screen checks
passed, and affected Artists plus draft/persistence/account restore checks
passed after preserving the legacy role API path. Node facet/search/URL/keyboard
checks and native runner pyflakes passed. Exact commands, retained failures and
exit codes are under `/home/paul/tmp/vk3-8k3-evidence/`.

Actual acceptance is pending. Browser runner:
`player-server/test/e2e-web/tests/music-facets.test.ts` with the existing
`playwright.music.config.ts` (installed Chrome/Firefox; genuine touch helper).
Native runner: `player-android/test/e2e-live/music_facets_e2e.py FIXTURE --apk APK
--emulator-wav WAV [--scenario all|core|state|errors]`. Default all covers each
continuation. Core covers combined facets, no cross-song composer, Clear and
sheet draft Back. State covers locked cold restart, actual admin/viewer account
switching and literal release labels. Errors covers owned fixture stop/restart,
visible chips/Retry and permissions revoke/restore. Existing public fixtures and
all previous evidence remain unchanged. Fresh generation adds `Restricted only`
genre to the existing Restricted Hidden Track, so admin/viewer option assertions
prove real ACL exclusion without new tracks. Use `--release-metadata` for labels,
moods and release types; no extra augmentation is required.

Implementation source frozen for root review. Final scoped Music service/API run
(including real SQLite ACL options/results and malformed year) passed. Fifteen
Flutter cases have passing latest results across the retained combined attempt
and affected Artists/sheet retry; the intermediate legacy-role mock failure is
retained in the logs. Three native gesture-region oracle tests pass; Playwright
lists four M08 cases across the two installed-browser projects. These are
focused implementation checks only: no new server/APK build or actual browser /
emulator feature run has been performed by this worker. Root owns bounded
reviews, integrated gates/builds, actual platform verification and closure.

## Implementation-first workflow stage

The user workflow update (2026-10-22 17:53 UTC) defers full suites, final
server/APK builds and actual Chrome/Firefox/native journeys until backlog
implementation is complete. First expert review found deleted search modifiers
remaining applied and repeated artist roles losing matches. The fixes track
typed and explicit facets separately, synchronize chip/Clear edits with typed
modifiers, and include all requested artist roles. Focused Node transition and
real-SQLite API regressions pass, including reversed role order and an invalid
second role. Applied controls also remain usable during result loading without
waiting for options. Logs: `m08-round1-fixes-light.log` and
`m08-round1-fixes-go.log` in the external evidence directory. Narrow final
review confirmed both blockers resolved with no new blocking defect. Task `vk3` remains pending integrated verification; none of
the deferred platform journeys or final gates is claimed as passed.
