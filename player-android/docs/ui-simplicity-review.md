# Android UI simplicity review (task 0w3)

This is an implementation and verification inventory, dated 2026-10-28.
It covers every screen under `lib/screens`, including route wrappers and dialogs,
plus the shared player, queue, discovery, and settings surfaces. “Keep” means
code inspection found an existing focused layout; it does not claim device
verification. Existing recent simplifications are deliberately retained.

Screen targets count the complete visible surface, including player chrome.
Five primary buttons is a target, with transport and form/navigation exceptions.
Secondary actions use labeled menus or expandable details. No capability relies
on a swipe, long press, hover, or other unlabeled gesture alone. Existing optional
hold-to-seek has named menu equivalents. Touch targets added here are 48dp;
existing form and navigation controls retain their standard Material targets.

## Changes

| Surface | Before | After and retained actions |
| --- | --- | --- |
| My Shares | Copy and action menu on every row | One row hamburger holds Copy link, Edit share, Revoke share. Row tap still edits; revoke still confirms. |
| Trash | Restore and permanent-delete icons on every row | One labeled row hamburger; Restore and Delete permanently remain named items. Permanent deletion still confirms. |
| Users | Destructive delete as a primary row action | Destructive action in a named user menu. Own account has no delete menu; create, role badges and confirmation retained. |
| Permissions | Refresh, user-editor shortcut, and overflow menu competed | One hamburger holds Refresh and the alternate editor/matrix destination. Search text, toggles and matrix retained. |
| Music identity | Full candidate descriptions and a resolution toolbar per conflict | Collapsed path/count rows expand to full paths, reason, Keep separate, and each candidate merge choice. Busy guards retained. |
| Server plugins | Every plugin displayed its description, permissions, and action toolbar | Compact name/status rows expand to full name, version, description, error, permissions, Enable/Disable and self-check. Existing confirmations and request guards retained. |
| Notes | Generic overflow icon | Hamburger consistent with other screen menus, named More options, 48dp. Save, Clear, autosave and confirmation retained. |
| Podcasts | Separate Refresh control and manual URL menu; discovery options before the query | Explore stays direct; Refresh feeds and Add feed URL share one hamburger. Discovery puts query/Search first, chart settings in Popular podcasts, with visible concise disclosure and expandable full details. |
| Music home pins | Separate Unpin button below every pinned playlist | Unpin joins the existing collection hamburger containing playback, queue, sharing and download actions. Empty playlists can still open the menu to unpin; playback stays disabled. |

## Complete screen inventory

| Screen source (`lib/screens/`) | Decision and reachable controls |
| --- | --- |
| `home_screen.dart` | Keep library content first, queue and server switch, navigation drawer for albums/playlists/artists, radio, podcasts, downloads, history, shares, settings. All music views remains an explicit drawer destination when optional shortcuts are hidden. |
| `local_library_screen.dart` | Keep Add files and server switch with one hamburger for podcasts/downloads/connection/settings. Rows already have one item menu for notes/relink/remove. Empty-state Add files and Connect remain direct. |
| `media_grid_screen.dart` | Keep queue + hamburger, visible search, filter sheet, compact media tiles. Favorite is a frequent accessible shortcut; advanced filter/layout choices remain in the menu. |
| `folder_browser_screen.dart` | Keep folder navigation, breadcrumbs, queue and preview-size hamburger; no repeated row toolbar. |
| `media_detail_screen.dart` | Keep direct Play/resume, expandable description/details, one hamburger for queue/notes/share and secondary actions. |
| `continue_watching_screen.dart` | Keep resume cards with progress and direct tap; no extra action toolbar. |
| `music_albums_screen.dart` | Keep search, queue, one hamburger, collection playback header and compact album/song actions. Music home pins simplified above. Ordinary album navigation remains direct. |
| `music_artists_screen.dart` | Keep search, queue and hamburger. Detail metadata/related collections use existing disclosure and compact collection menus. |
| `music_playlists_screen.dart` | Keep catalog search/queue/hamburger; selected playlist Play/Shuffle/Add songs and one combined action menu. Details already collapse; song rows have one menu; management/import/export/sync/advanced operations remain in that menu. |
| `music_search_screen.dart` | Keep query and keyboard submit; search options in hamburger and one result action menu. |
| `music_playlist_rules_screen.dart` | Keep focused rule form and Save/Cancel; rule add/remove are essential editing controls rather than secondary navigation. |
| `radio_screen.dart` | Keep direct search and Auto explore, collapsed Filters and explore options, compact station list. Now-playing keeps Play/Pause, Stop and station actions; active exploration retains next/keep controls. Transport exception to button target is intentional. |
| `audio_player_screen.dart` | Keep prominent previous/play/next for music and ±15s/play for spoken audio, seek, queue/cast/hamburger. Speed, details, notes, bookmarks, lyrics, sleep timer and Sound & playback remain in menus; active sleep timer remains visible. Core transport is intentionally an exception to the button target. |
| `video_player_screen.dart` | Keep direct video transport/seek/fullscreen and cast/hamburger. Secondary playback/personal-media actions already in menus. |
| `image_viewer_screen.dart` | Keep image, zoom/navigation, and hamburger for secondary image actions. |
| `share_viewer_screen.dart` | Keep collection/media list with a single clear Play action and share restrictions/errors. Uses the same public player controls after playback starts. |
| `share_screen.dart` | Keep short instruction/entry page; no toolbar to hide. |
| `my_shares_screen.dart` | Changed as above. List descriptions stay bounded; copy is now in the same row menu as edit/revoke. |
| `notes_editor_screen.dart` | Changed menu affordance as above; editor, save state, draft recovery, Save and Clear preserved. |
| `offline_downloads_screen.dart` | Keep offline browse destination, compact saved file rows with one menu, progress and cancel/retry only while applicable. Offline collections remain reachable without authentication. |
| `standalone_podcasts_screen.dart` | Discovery integration owned by the podcast discovery task: Explore navigation, secondary feed refresh/URL subscription actions, compact feed/episode rows. Refresh, subscribe, unsubscribe, download and playback remain reachable. Explore stays direct; Refresh feeds and Add feed URL share the Podcast options hamburger. |
| `podcast_discovery_screen.dart` | New discovery screen owned by the podcast discovery task: Search and compact title/author results, one Subscribe action per result; Add feed URL is secondary. Query/Search come first; country/load are in Popular podcasts, with concise provider/mobile-data disclosure visible and full disclosure in About podcast search. |
| `podcast_list_screen.dart` | Keep compatibility route wrapper to standalone podcasts. |
| `podcast_episodes_screen.dart` | Keep compatibility route wrapper to standalone podcasts. |
| `settings_screen.dart` | Keep existing category pages: Account, Appearance, Headphones, Automation, Music & offline, Servers, Sharing, Administration. Connection/device/podcast association and shortcuts remain discoverable; normal music navigation stays in the library drawer. Forms retain required fields and direct commit/cancel. |
| `server_plugins_screen.dart` | Changed as above; advanced page stays under Administration with its existing forbidden/error/result states. |
| `admin_users_screen.dart` | Changed as above; Create user remains the primary action, refresh retained. |
| `admin_permissions_screen.dart` | Changed as above; default per-user editor remains concise, advanced matrix behind same hamburger. |
| `admin_libraries_screen.dart` | Keep existing single library action menu (Rename, Manage access, Remove), Add library and Refresh; focused root/access dialogs retain their fields and confirmations. |
| `admin_music_identity_screen.dart` | Changed as above; collapsed conflicts reclaim list room, expanded details retain every resolution choice. |
| `admin_rescan_screen.dart` | Keep direct Start/Cancel scan and status refresh, existing collapsed Scan settings. Progress/error information stays visible during active work. |
| `admin_trash_screen.dart` | Changed as above; concise path/deletion metadata and loading/error/empty states retained. |
| `api_tokens_screen.dart` | Keep Create token/Refresh and one revoke action per token. Secret-copy reveal and expiry controls are part of focused creation flow; revoke confirmation retained. |
| `bootstrap_screen.dart` | Keep account setup form, primary submit, server/device alternatives. Required inputs and password visibility are essential controls. |
| `login_screen.dart` | Keep sign-in form, primary submit, server/device alternatives. Required inputs and password visibility remain reachable. |
| `create_share_dialog.dart` | Keep focused share form, collapsed options where present, Create/Cancel and copy-result step; essential permissions/expiry fields retained. |
| `edit_share_dialog.dart` | Keep focused share edit form and Save/Cancel. |
| `subscribe_dialog.dart` | Keep URL/preview/subscribe and Cancel; validation/error/retry is part of the task flow. |
| `local_playback_route.dart` | Keep request-preparation/repair wrapper; delegates actual playback to shared audio/video screens. |
| `offline_playback_route.dart` | Keep offline preparation wrapper and source/download recovery; delegates playback to shared players. |
| `media_playback_navigation.dart`, `cast_screen_mixin.dart`, `playback_preparation_mixin.dart` | Navigation/preparation helpers rather than extra UI screens; existing errors/retry/cast ownership preserved. |

## Shared surfaces reviewed

- `music_queue_sheet.dart`: existing compact entries, one row menu, accessible
  reorder/remove controls; queue More groups save/load/clear/undo. Shuffle,
  Repeat, autoplay and restored-queue Resume remain understandable and reachable.
  Renderer queue uses direct item selection and Close.
- `music_collection_actions.dart`: existing Play/Shuffle/More on detail headers,
  menu-only compact embeds. Compact management extras now remain available on
  empty collections, just as on noncompact headers.
- `music_discovery_panel.dart`: View all remains direct ordinary browsing;
  Customize home stays explicit; pinned Unpin consolidated as above. Home
  customization form retains visibility, ordering and playlist pin controls.
- `music_enrichment_section.dart`, `music_metadata_details.dart`,
  `music_presence_panel.dart`, artwork/related/top-song/listening/lyrics panels:
  existing disclosure and compact rows retained; optional metadata is not required
  before browsing or playback.
- Shared filter/rating/search/layout sheets, equalizer/loudness/quality/sleep
  sheets, saved queue/import/shuffle/download dialogs and personal media actions:
  already focused secondary surfaces; retain their selections, validation,
  cancel/retry, names and confirmation rather than adding another menu layer.
- Settings widgets for language/theme/presentation/network/navigation/headsets,
  cache/history/automation/server profiles/podcast association: remain grouped on
  their existing focused category pages. No routine library destination moved
  into Settings.

## Verification

Focused widget tests exercise actual new menu paths and retained mutations,
clipboard copy, confirmation/cancel/failure handling, editor/matrix round-trip,
and expanded identity/plugin controls. Plugin tests include narrow light/dark
layouts with 1.5× text. Analyzer checks changed production sources.

The full Flutter suite passed (1,850 tests). Native API 34 emulator checks
against `player.f3s.buetow.org` exercised user actions and delete cancellation,
permissions editor/matrix round-trip, empty plugin and identity states,
My Shares empty state, player Notes > Save/Clear menu, podcast search/popular
charts/subscription/episode loading, curated playlist browsing and playback.
Notes also retained its menu at 1.5× system text. No production account or media
was deleted during verification.

Chrome and Firefox exercised discovery in device and ordinary-account modes,
including manual RSS entry, saved subscriptions, real episode fetching, errors
and keyboard focus. Production library, artists and curated playlists were also
checked in both browsers.

Verification limits: production has no share rows, installed plugins or identity
conflicts, so their populated row/expanded actions were exercised by widget
tests rather than native production data. The complete screen inventory above
is a code review, not a claim that every existing feature was exercised on all
three platforms. Non-admin and injected API-error states are covered by focused
tests; they were not all repeated manually on the production emulator.
