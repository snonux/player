# Music candidate interaction and verification map

Companion to [music-interaction-design.md](music-interaction-design.md)
(task `nj3`). One row per candidate scope from the 2026-10-20 music gap
inventory, with its `ask` task alias, where it lives, the primary action and
what is progressively disclosed, the keyboard/touch path, the states that
task must design, and the end-to-end check that proves it on each platform.

Conventions used in the tables:

- **Lives in** — `W:` web location, `A:` Android location. Section numbers
  (§) refer to the design document.
- **Keys / touch** — web key (see §12) / Android control. "menu" means the
  existing `a` action menu on the web and the row ⋮ bottom sheet on Android.
- **Verify** — the minimum real journey. *C* = installed Google Chrome,
  *F* = installed Mozilla Firefox, *A* = release APK on an Android emulator,
  all against a disposable local server with the `oj3` fixture. Unless a row
  says otherwise, the same journey runs on C, F and A, plus light, dark and
  narrow screenshots and keyboard (web) / TalkBack labels and Back (Android).
- Status words (Absent/Partial/Parity/Native) restate the inventory: what
  Player has at `5040cfb` and therefore what must be *extended*, not rebuilt.

Foundation order (dependency, not priority): `oj3` V01 fixture → M01 → M02–M05
→ M12–M16 (queue) → M07/M08 → playlists → the rest. `jm3` (V02) verifies the
whole backlog at the end.

## Catalog and navigation (M01–M11)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M01 `pj3` | Index embedded tags (title, artist, album artist, album, track/disc, year, genres); show music titles. Absent. | Server scanner/probe + `music` JSON object; W/A: every song row, player title, media info | Titles replace file names everywhere songs appear · raw file name in Info | — | untagged file → name without extension; admin rescan progress; mixed tagged/untagged folder | Rescan fixture; C/F: grid card, player title and media info show "Long Road Home"; A: grid, player app bar, `dumpsys media_session` title. Untagged `track_0042` shows `track_0042`. |
| M02 `yj3` | Album catalog and detail: artwork, ordered songs, totals, linked artist; play album. Absent. | W: Music › Albums, Album detail (§5.2). A: drawer Music › Albums, Album screen (§6.2) | Play (album) · Shuffle, More menu | Enter opens, Play button plays; tap card, Play button | empty catalog (no tags), loading, partial album (no access), album without cover | Open Night Transit, Play: queue = tracks 1–3 in order, auto-advance to 2; image `cover.jpg` never queued (B3). A: same with notification next. |
| M03 `zj3` | Artist catalog/discography; track vs album artists; composer/performer credits. Absent. | W: Music › Artists, Artist detail. A: Artists screen | Play (artist: all albums) · Albums, Appears on, Credits (disclosure) | `B` go to artist; tap artist link | artist with only "appears on" songs; many credits (`+N`) | "Aurora Lane feat. Kei" credited on track 2 links to Aurora Lane and appears under Kei's *Appears on*. |
| M04 `7k3` | Multi-disc and compilations: disc/track order, multiple artists, disc subtitles/art. Absent. | Album detail disc headers | — · disc headers only when >1 disc | — | compilation "Various Artists" with per-track artists | Synth Compilation lists Disc 1 (Neon Rain, Glass City), Disc 2 (Afterglow), each row shows its track artist (B6 fixed on A). |
| M05 `0k3` | Rich release metadata (dates, release type, label, catalog no., media, mood/grouping/comments). Absent. | Album detail › Info disclosure; Album Info sheet (A) | — · *Album info* dialog/sheet | `i` on album; ⋮ › Info | missing fields hidden, not "unknown" | Fixture with label/original date: values shown in Info on C/F/A; absent fields omitted. |
| M06 `8k3` | Music/technical info panel (credits, BPM, gain, sample rate, bit depth + existing codec/bitrate/size). Partial. | Existing media info modal (W `i`), Info sheet (A) | — · grouped *Music* / *File* sections | `i`; ⋮ › Info | long credit lists scroll inside dialog | Info on FLAC shows sample rate/bit depth and the existing path/size fields remain. |
| M07 `9k3` | Metadata-aware search (title/artist/album → music entities). Partial (file-name search). | W: `/` grouped results (§5.5). A: search field with Songs/Albums/Artists chips | Enter: play song / open entity · Files group | `/`, `artist:`/`album:` modifiers; tap result | no results with *Clear filters*; Escape restores previous view | Query `Kei` returns the song and the artist on C/F/A (B2 fixed); Escape returns to the album view, not set root. |
| M08 `vk3` | Music facets: genre/year/decade, roles, release type/compilation, mood/label. Partial. | W: facet selects in Albums/Songs toolbar. A: filter bottom sheet ("Filters (2)") | Apply filter · less common facets under *More filters* | native selects keep their keys; chips | facet with no values hidden; active-filter count | Genre = Ambient lists only Night Transit (Harbour); clearing restores; URL/route restores filter after reload/restart. |
| M09 `wk3` | Album grid/table, music columns, music sorts, remembered layout. Partial. | Albums/Songs toolbar: Sort, View ▦/☰, Columns (table) | Sort select · *Columns* menu | `g` keeps picture layout; sort select | narrow: table collapses to two-line rows | Sort by year, switch to table, reload/restart → remembered on C/F/A. |
| M10 `ak3` | Artist/album enrichment (bio, image, description, links, refresh). Absent; external provider. | Artist/Album detail header *About* disclosure | Read · *Refresh metadata* (admin) | ⋮ › About | provider disabled/unreachable; no data | Deterministic local provider fixture: bio appears; provider off → section hidden, no error spam. |
| M11 `bk3` | Custom artist/playlist artwork upload/remove. Partial (set/folder covers). | Artist/Playlist ⋮ › *Change artwork* (dialog / Android picker) | Upload · Remove with confirm | menu letters; file picker | too-large/invalid image error; permission (not owner) | Upload PNG on C and F (file input), photo picker on A; image shown after reload; non-owner sees no action. |

## Playback and queue (M12–M25)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M12 `1k3` | Real sequence, true previous/next incl. native transport. Web partial (browse order), Android absent. | Queue owner (§8.1); player ⏮/⏭; Android notification/Bluetooth | Next/Previous · ±15 s moves to overflow for music (§6.3) | `N`/`P`; ⏮ ⏭ buttons; media keys | end of queue (Ended, Play shown); failed item skipped with toast | C/F: album plays 1→2→3 then stops; image not entered (B3); search while playing doesn't stop sequence (B4). A: auto-advance, `KEYCODE_MEDIA_NEXT` changes track (B7), podcast item keeps ±15 s. Also fix `Space/p` doc (B5). |
| M13 `ck3` | Visible editable queue: inspect, jump, reorder, remove, clear. Absent. | W: Queue panel/dialog (§5.3/5.4). A: Queue sheet (§6.3) | Jump (Enter/tap) · Move/Remove via menu | `Q`, `J/K`, `Delete`, Undo; sheet + ⋮, semantics actions | empty queue; Undo after Clear; 1,000+ entries | Reorder with keys only (C/F) and menu only (A); remove + Undo; playback order matches; focus stays on moved row. |
| M14 `xk3` | Play now / Play next / Add to queue for songs and selections. Partial. | `a` menu, row ⋮ sheet, album More | Play · Play next / Add to queue | `e`/`E`; menu | toast "Added 3 songs" with Undo | Play next twice keeps FIFO "Up next (you added)" order on C/F/A; audio actually switches at track end. |
| M15 `kl3` | Play/enqueue albums, artists, playlists; stable collection order; batch selection. Absent. | Album/Artist/Playlist headers and menus | Play · Add to queue | Enter/`e`/`E` on cards | artist with many albums (ordering rule) | Artist Play = albums by year, tracks in order; decide song multi-select (`m` reserved). |
| M16 `yk3` | Persist local queue/current item across reload/restart. Absent. | Queue store (§8.4) | automatic · restore notice | — | removed/forbidden items dropped with notice; other account | Reload (C/F) and force-stop/restart (A): queue, index, position restored; switch account → not shown. |
| M17 `ll3` | Save/restore server play queue for another client. Absent. | Queue › *Save to account* / *Continue from other device?* prompt | Continue · Dismiss | Queue menu | conflict: local queue playing; offline save pending | Save on A, continue on C and F at the same track/position; stale revision rejected. |
| M18 `zk3` | Playback shuffle of queue/remainder/collection without changing browse sort. Web partial, Android absent. | Queue toggle ⤮, collection Shuffle | Shuffle toggle (aria-pressed / selected) | `r` in Queue; ⤮ button | toggle off restores remaining order | Shuffle upcoming: now-playing stays, grid sort unchanged; off restores. |
| M19 `ml3` | Shuffle by album. Absent. | Queue menu, artist/playlist More | menu item | menu | single-album queue: item disabled with reason | Two-album queue: album blocks keep track order. |
| M20 `dk3` | Repeat off/all/one. Web partial (implicit wrap), Android absent. | Player 🔁 and Queue | cycle button with label "Repeat: All" | `Y`; button | repeat one + Next (moves on) | Each mode at end of queue on C/F/A; remove implicit wrap. |
| M21 `0l3` | Gapless multi-track playback. Absent. | Playback engine; Settings › Playback *Gapless* | automatic | — | format not gapless-capable: falls back silently | Measured gap between gapless fixture tracks below threshold on C/F (audio analysis) and A (just_audio sequence); states gap explicitly if output can't be measured. |
| M22 `ek3` | Countdown sleep timer. Absent. | Player ⏲ (web controls), Now Playing overflow (A) | choose duration · Cancel | `Z`; sheet | timer visible countdown; cancel; pauses (no fade) | 1-minute timer pauses playback on C/F/A; state survives screen changes. |
| M23 `fk3` | Web playback speed; keep Android choices. Parity. | Web player control *1×* menu | select speed | `<`/`>`; menu | speed resets per item? (decide: persists per kind) | C/F: `playbackRate` changes and persists; A unchanged 0.5–2×. |
| M24 `2k3` | ReplayGain track/album/auto, preamp, clipping-aware. Absent. | Settings › Playback | select mode · preamp slider | native select/slider | tags missing → no change | Gain fixture: Web Audio gain node / Android volume applied (measured level), both themes. |
| M25 `gk3` | Equalizer and presets; Android system panel option. Absent. | W: Settings › Playback *Equalizer* dialog (Web Audio). A: Now Playing overflow › Equalizer (in-app) + *System equalizer* (Android-only) | preset select · bands | sliders with arrows | system panel absent → hidden | Preset changes measured spectrum on C/F; A in-app EQ effect; system panel launch on A only (§11). |

## Playlists and personal data (M26–M37)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M26 `3k3` | Persistent playlists: create/rename/delete, comment, count/duration, cover. Absent. | Music › Playlists (grid), Playlist detail | Play · New/Rename/Delete in menu | `w` creates while adding; *New playlist* button | delete confirm; empty playlist | Create, rename, delete with confirm on C/F/A; survives restart. |
| M27 `nl3` | Edit playlist songs: add, remove, reorder, search within. Absent. | Playlist detail rows; *Add to playlist…* dialog/sheet | Add (`w`) · Move/Remove | `w`, `J/K`, `Delete`+Undo; ⋮ | duplicate add (→ M30); long list filter | Add 3 songs, reorder with keys/menu, remove + Undo, filter within list. |
| M28 `1l3` | Save queue as playlist. Absent. | Queue footer *Save as playlist* | Save · name dialog | Queue button | name collision | Queue order equals playlist order on C/F/A. |
| M29 `hk3` | Playlist visibility/ownership (private vs shared, admin reassignment). Absent. | Playlist Info › *Visible to other users* toggle; admin panel | toggle · owner change (admin) | switch | viewer without set access sees "2 songs hidden" | nj3-user sees shared list minus inaccessible songs; cannot edit. |
| M30 `2m3` | Duplicate add/skip, containing playlists, add to several lists. Absent. | Add-to-playlist dialog/sheet with checkboxes | Add · *Already in: X* notice | checkboxes, Enter | duplicates prompt: Add anyway / Skip | Adding a duplicate prompts on C/F/A; song Info lists containing playlists. |
| M31 `ik3` | M3U import/sync from filesystem. Absent. | Admin/library scan; Playlists show *Imported* badge | automatic · *Stop syncing* | — | unresolved paths listed | Fixture `.m3u` becomes a playlist; changed file re-syncs; unresolved entry reported. |
| M32 `jk3` | M3U export. Absent. | Playlist More › *Export M3U* | download | menu | — | Exported file lists relative paths in order (C/F download, A share sheet/save). |
| M33 `ol3` | Rule-based smart playlists. Absent. | Playlists › *New smart playlist* (form editor) | Save rules · preview count | form controls, no drag | invalid rule message; zero matches | Rule "genre = Synthwave" lists expected songs; refresh after rescan. |
| M34 `kk3` | Favorite albums/artists/playlists (extends file hearts). Partial. | ♥ on album/artist/playlist headers; Music home *Favorites* | ♥ toggle | `F`; ♥ button | — | Favorite album appears in Favorites on all three; song hearts unchanged. |
| M35 `lk3` | Five-star ratings for songs/albums/artists; rated sorts/filters. Absent. | menu › *Rate…* popover (web), rating sheet (A) | choose 0–5 · clear | `0`–`5` inside popover; stars ≥48 dp | — | Rate on C, see on F and A; sort by rating. |
| M36 `mk3` | Personal play counts and last played (+ album/artist aggregates). Partial (global count). | Song/album info, sorts | — | — | existing global count kept as "All listeners" | Two users' counts differ; existing count unchanged. |
| M37 `2l3` | Durable listening-event history. Partial. | Server events; later history view | — | — | privacy: per user only | Each completed play writes one event (API check) from C, F and A, including offline A plays synced later. |

## Discovery (M38–M44)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M38 `pl3` | Discovery collections (recent added/played, most played, random, favorite, top rated, genre/decade). Partial. | Music home sections (rows of cards) | Open/Play a card · *See all* | `j/k` between sections, `h/l` within | section hidden when empty | Recently played updates after a play on each platform. |
| M39 `3m3` | Configurable music home (order/hide, pin playlists, mixes). Absent. | Music home › *Customize* dialog/sheet | Move up/down, Show/Hide | buttons, no drag | reset to default | Hide and reorder sections with keys (C/F) and buttons (A); persisted. |
| M40 `3l3` | Artist top songs. Absent. | Artist detail *Top songs* (5, *Show more*) | Play top songs | Enter | no play data → section hidden | Uses M36 counts; play-all order matches. |
| M41 `nk3` | Similar artists/albums/songs (metadata/provider). Absent. | Album/Artist *More like this* row | Open · Play | — | provider off → hidden | Deterministic fixture similarity shows expected items. |
| M42 `ql3` | Artist radio / Instant Mix. Absent. | menu › *Start mix* | Start mix (replaces queue with source label "Mix: X") | menu | not enough material → message | Mix queue contains related songs; source label visible. |
| M43 `4m3` | Continuous related playback after queue end. Absent. | Queue footer toggle *Keep playing similar music* | toggle | button | off by default | At end, related songs appended (labelled "Added automatically"). |
| M44 `rl3` | External scrobbling (Last.fm/ListenBrainz-compatible). Absent. | Web Settings › Listening data; A Settings › Listening data | Connect account · per-device opt-out | form fields | auth failure wording; offline queue | Local ListenBrainz-compatible stub receives now-playing/listen from C, F and A plays; opt-out stops it. |

## Offline and quality (M45–M53)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M45 `ok3` | Automatic playback cache with budget/eviction/prefetch. Absent. | A: Settings › Storage & cache. W: not applicable beyond browser HTTP cache (state explicitly) | set budget · Clear cache | slider/buttons | cache full; storage unavailable | A: replay offline after streaming; eviction at budget. Web: documented as Android-only scope plus unchanged streaming. |
| M46 `pk3` | Bulk downloads of albums/playlists/selections. Partial (per file). | Album/Playlist More › Download (W: zip or sequential; A: Keep offline all) | Download · progress, cancel | menu | partial failure list; storage full | Album download completes with correct file count on C/F (download event) and A (Downloads screen). |
| M47 `4l3` | Pin collections offline, independent of cache eviction. A partial, web absent. | A: *Keep offline* on album/playlist; Downloads › Music | Keep offline toggle | button | collection changed upstream → *Update* | A only for storage; web shows no pin (scope stated). |
| M48 `sl3` | Automatic offline sync of playlists/starred/recent with schedule and Wi-Fi policy. Absent. | A: Settings › Offline sync | toggle per collection | switches | Wi-Fi-only waiting state | A: change playlist on web → synced on A under policy; web unaffected. |
| M49 `tl3` | Offline music browsing (albums/artists/playlists/search/queue without server). A partial. | A: Music lens in offline mode with banner | Play downloaded | — | non-downloaded items disabled with reason | A in airplane mode: browse, search, queue downloaded music; web: "Can't reach the server". |
| M50 `4k3` | Selectable streaming quality (original vs codec/bitrate). Partial (fixed AAC 128k compat). | Settings › Playback *Streaming quality* | select | native select | transcode busy (503) state | Chosen bitrate observed in response/codec on C/F/A; original still default. |
| M51 `5l3` | Selectable download quality. Partial. | Download dialog/sheet option | select | — | — | Downloaded file codec/bitrate matches choice. |
| M52 `6l3` | Network-aware settings (Wi-Fi/mobile, warnings, cover data saver). Absent. | A: Settings per network. W: manual *Data saver* toggle | select | — | browser can't detect network (Firefox) → manual | A emulator network switch changes quality; web toggle honored in C and F. |
| M53 `qj3` | Local/remote endpoint fallback (+ SSID). A partial (profiles). | A: server profile › *Local address* | add URL | form | both unreachable | A falls back when local URL down; web unaffected (scope stated). |

## Controls, external playback and sharing (M54–M71)

| ID / task | Scope and status | Lives in | Primary action · disclosed | Keys / touch | States to design | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| M54 `qk3` | Rich Media Session: title/artist/album/art and real track transport. A partial. | W: `navigator.mediaSession` metadata + next/previous handlers. A: audio_service MediaItem | — | hardware media keys | missing art → app icon | C and F: `navigator.mediaSession.metadata` and action handlers drive queue (media key dispatch); A: `dumpsys media_session` metadata + next/prev. |
| M55 `7l3` | Android Auto browse/play/search. Native gap. | A only: MediaBrowser tree (§11) | — | car UI | flavor without Auto; offline | Desktop Head Unit or Auto emulator host required; if unavailable, gap stated and feature not accepted. Web verifies `/music/*` results used by the tree. |
| M56 `8l3` | Home-screen widgets. Native gap. | A only | play/pause/next | widget buttons | no session | Emulator launcher widget shows current track and controls queue. |
| M57 `5m3` | Tasker/Locale automation. Native gap. | A only: exported intents/plugin | — | — | permission denied | `adb shell am broadcast` with plugin intent starts shuffle; real Tasker optional, gap stated. |
| M58 `9l3` | Headset connect/unplug behavior settings. Native gap. | A: Settings › Playback | switches | — | defaults documented | Emulated `ACTION_AUDIO_BECOMING_NOISY` pauses; connect autostart via emulated Bluetooth if possible, else gap stated. |
| M59 `al3` | DLNA/UPnP renderer playback. Absent. | *Play on…* menu (§10.3), server control point | choose renderer | menu | renderer lost; format unsupported | Local UPnP renderer (e.g. gmrender/rygel in a container) plays from C, F and A. |
| M60 `6m3` | Cast a queue/collection. Partial (current file; Firefox not offered). | Cast in *Play on…* | Cast · queue continues on receiver | existing Cast button | receiver disconnect hands back | Real Cast receiver required; C and A move through the queue; F uses Player Connect (F02). Gaps stated if no receiver. |
| M61 `bl3` | Internet-radio stations. Absent. | Music › Radio; admin station editor | Play station | Enter/tap | stream down; no art | Local test stream (icecast container) plays on C/F/A; admin CRUD. |
| M62 `ul3` | Share albums/artists/playlists/selections (collection share player). Partial. | menu › Share (existing share dialog with collection target) | Copy link · options | `s` on selected entity | share page queue (§8.1) | Open share on F (anonymous) and in A share viewer: plays collection in order; single-use rules kept. |
| M63 `7m3` | Edit share description/expiry. Partial. | My Shares (`L`) row › Edit | Save | Enter in dialog | expired share | Edit on C, see on F and A. |
| M64 `8m3` | Share download permission. Partial. | Share dialog toggle *Allow download* | toggle | switch | — | Download hidden and `/download` refused when off (all three). |
| M65 `9m3` | Share encoding/bitrate choice. Partial. | Share dialog *Quality* | select | — | — | Shared playback uses chosen rendition. |
| M66 `am3` | Share visits / last visit. Partial (use count). | My Shares row secondary line | — | — | never visited | Visit from F updates last visit seen on C and A. |
| M67 `rj3` | Translated UI with language selection. Absent. | Settings › Language (both) | select | — | missing strings fall back to English | Switch to a second language on C/F/A; layouts don't clip (long German labels). |
| M68 `sj3` | Theme/accent/artwork presentation choices; native always-on option. Partial. | Settings › Appearance | select | — | contrast check per accent | Each accent in light/dark passes 4.5:1; always-on only on A. |
| M69 `rk3` | Landing view and show/hide music sections. Partial. | Settings › Music library | select | — | hiding current view | Start destination persisted; hidden sections gone from sidebar/drawer. |
| M70 `cl3` | Optional desktop song notifications. Parity (A has notification). | W: Settings › Notifications (Notification API permission) | Enable · permission prompt on click | button | permission denied/blocked message | C and F show a notification on track change in a background tab; A unchanged. |
| M71 `dl3` | Live Now Playing across listeners. Absent. | Music home *Now playing* (admin/all per policy); uses Player Connect sessions (§10.3) | — | — | privacy opt-out | Play on A, see live entry with progress on C and F. |

## Existing Player parity follow-ups (F01–F03)

| ID / task | Scope | Lives in | Primary action | Keys / touch | States | Verify |
| --- | --- | --- | --- | --- | --- | --- |
| F01 `tj3` | Seek by clicking a timed lyric line on the web (Android already taps to seek). | Web lyrics panel lines become buttons | click/Enter seeks | `j/k` scroll stays; Tab to a line, Enter | plain (untimed) lyrics stay non-interactive | C and F: click and keyboard Enter on line 2 sets `currentTime` ≈ 4 s; A regression check of *Tap to seek*. |
| F02 `vl3` | Supported Firefox remote-playback journey. | *Play on…* (§10.3) | choose target | button + menu | no targets message; never a dead Cast button | F controls playback on the A app via Player Connect; C keeps Cast; A target receives commands. Real devices or emulator + browser on one host. |
| F03 `uj3` | Expose existing web library filters on Android (tags, sets, duration, size). | A: filter bottom sheet from the search bar | Apply | chips/sliders, labelled | no tags exist | Same filter on web and A returns the same items. |

## Optional infrastructure (A01–A10)

These rows are not part of the first music wave. UI impact is listed so the
infrastructure does not invent its own screens.

| ID / task | Scope | UI placement | Verify |
| --- | --- | --- | --- |
| A01a `el3` | Subsonic/OpenSubsonic auth and browsing | none in Player UI; API tokens screen gains a *Subsonic clients* note | DSub2000/Tempo (or a scripted Subsonic client) against the local server; C/F/A unaffected regression. |
| A01b `wl3` | Subsonic streams, transcodes, downloads | none | stream/download via Subsonic client; ACL enforced. |
| A01c `bm3` | Subsonic playlists, stars, ratings | none; data appears in M26/M34/M35 UIs | star in Subsonic client → visible on C/F/A. |
| A01d `cm3` | Subsonic queue, bookmarks, lyrics, reports | none; M17 queue continuity | queue saved by client restores on A (M17). |
| A02a `fl3` | Server jukebox control | *Play on… › This server's speakers* (admin-enabled) | local audio sink on the server host; controlled from C, F, A. |
| A02b `xl3` | Multiroom routing | same menu, group targets | Snapcast fixture; gap stated if unavailable. |
| A03a `vj3` | More lyric formats (TTML, Enhanced LRC, SRT, YAML) | existing lyrics panel | each fixture renders on C/F/A. |
| A03b `5k3` | Word timing, translations, voices | lyrics panel *Translation* toggle, word highlight | C/F/A show word highlight on Enhanced LRC fixture. |
| A03c `dm3` | Offline lyrics | A offline player shows lyrics | A airplane mode shows cached lyrics; web unchanged. |
| A04a `gl3` / A04b `em3` | Acoustic similarity index / sonic mixes | *Start mix* menu option *Sounds like* | deterministic analysis fixture. |
| A05 `sk3` | Independent library roots and access | admin panel *Libraries* | ACL on roots respected in Music lens for nj3-user. |
| A06a `tk3` | Identity across moves/retags | none; keys stable (§10.1) | move/retag fixture keeps favorites, playlists, queue entries. |
| A06b `yl3` | Custom tag mapping | admin panel | mapped tag appears as facet (M08). |
| A06c `hl3` | Missing-record retention/repair | admin panel *Missing files* | missing file shown greyed in playlists with reason. |
| A06d `zl3` | Automated scans with visible progress | existing scan indicator | indicator visible on C/F; A Rescan screen. |
| A07 `uk3` | Client and transcoding profile admin | admin panel | profile applied to M50 choices. |
| A08a `wj3` – A08d `im3` | Plugin runtime, isolation, provider hookup, management UI | admin panel *Plugins* | plugin fixture feeding M10/M44; disabled plugin degrades to hidden sections. |
| A09a `il3` / A09b `0m3` | Jellyfin music API / discovery, Quick Connect | API tokens/admin approval dialog | Jellyfin-compatible client against the local server. |
| A10a `xj3` – A10e `fm3` | Reverse-proxy auth, granular permissions, backups/CLI, config display, activity | admin panel sections | each with its own admin journey; music lens obeys new permissions. |
