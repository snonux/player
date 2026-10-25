# Android music clients: DSub2000 and Tempo

## Scope, identity, and evidence

### Takeaway
Research checked 2026-10-20. These are source- and documentation-backed inventories, not hands-on app verification. Capabilities are interpreted from shipped release source, menus, preferences, and implementation; server-dependent APIs are conditional. Generic DSub marketing has not been used to automatically grant features to this fork.

### Cited Findings
- DSub2000 is **paroj/DSub2000**, a fork of daneren2005/Subsonic (original DSub), not an arbitrary secondary fork. Latest observed GitHub release **5.7.5**, published **2026-10-10**, is the comparison snapshot. [Repository](https://github.com/paroj/DSub2000); [release](https://github.com/paroj/DSub2000/releases/tag/5.7.5); [machine-readable timestamp](https://api.github.com/repos/paroj/DSub2000/releases/latest).
- Tempo means Antonio Cappiello’s native Android **CappielloAntonio/tempo**, latest observed release **3.9.0**, published **2024-12-31**. Its GitHub build has full Android Auto and Chromecast; the F-Droid flavor lacks full proprietary integration. The distinct eddyizm fork/successor Tempus is excluded. [Repository](https://github.com/CappielloAntonio/tempo); [release](https://github.com/CappielloAntonio/tempo/releases/tag/3.9.0); [machine-readable timestamp](https://api.github.com/repos/CappielloAntonio/tempo/releases/latest).

### Inferences
- A client action requiring a server endpoint is a capability the client exposes; it does not imply every Subsonic server implements it. Navidrome should not be assumed to implement podcasts, jukebox, or Madsonic extensions just because a client has their UI.

### Gaps
- No physical Android, car head-unit, Chromecast, or DLNA device test was performed. Existing code can carry bugs; “present in source” is not a runtime reliability guarantee. Original Tempo’s release tag contains a stale Gradle versionName (3.8.1); the release record identifies 3.9.0. [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/build.gradle)

## DSub2000 feature inventory

### Takeaway
DSub2000 exposes a broad traditional Subsonic client: folder and tag browsing, album discovery, complete queues/playlists, persistent cache and scheduled offline sync, local DSP, and external playback integration. The following features were checked in the DSub2000 5.7.5 fork itself.

### Cited Findings

#### Browsing and discovery
- Music folders and artist indexes; selectable tag versus folder browsing; album and song listings — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectArtistFragment.java).
- Album home views: newest, random, alphabetical, starred, genre, year, recently played, frequently played; highest-rated when not tag browsing — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/MainFragment.java).
- Random and starred songs; newest/recent/top-played/frequent song views specifically gated to Madsonic versions rather than generic Subsonic — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/MainFragment.java).
- Artist details and similar artists; artist radio/random similar-song buffer; navigate from a song to its album or artist — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectArtistFragment.java).
- Global search; song/album/artist result types — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SearchFragment.java).
- Metadata/detail screens for media, including format/transcoding info — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/adapter/DetailsAdapter.java).

#### Playlists, favorites, sharing
- Browse private/shared server playlists; play or shuffle a playlist; show playlist information — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_playlist_context.xml).
- Create a playlist from the queue; add songs to existing playlists; remove tracks; update playlist info; delete playlists — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_song_context.xml).
- Star/unstar and server rating controls on media; starred discovery views — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_song_context.xml).
- Keep a playlist synchronized offline and stop synchronization — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_playlist_context.xml).
- Create server share links; list shares; edit description/expiry; delete shares — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectShareFragment.java).

#### Playback and queue
- Play now, play next, play last, play shuffled; download, pin, delete cached copy; batch media operations — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_song_context.xml).
- Editable Now Playing queue with reorder and removal; clear queue; save queue as playlist; remove played entries; batch mode — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/nowplaying.xml).
- True track previous/next and automatic transition; repeat off/all/single; local queue shuffle — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/DownloadService.java).
- Global random/shuffle with genre and start/end year filters; configurable random queue length — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/values/strings.xml).
- Shuffle by album (preserve album grouping) versus all tracks; configurable played-track retention; configurable rewind/forward seek intervals — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_playback.xml).
- Gapless local playback; local music playback speed with preset/custom selector; countdown sleep timer — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/NowPlayingFragment.java).
- ReplayGain track, album, smart choice; preamp and untagged-song gain controls — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_playback.xml).
- Built-in equalizer with presets and bass/voice boost controls — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/EqualizerFragment.java).
- Basic text lyrics from server getLyrics(artist,title), with queue-follow option; no synchronized line/word karaoke established — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/LyricsFragment.java).
- Create timestamp bookmark, browse/delete/view bookmark details, and resume from position — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectBookmarkFragment.java).
- Save queue/current track/position to server during lifecycle synchronization — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/DownloadServiceLifecycleSupport.java).

#### Offline, network, quality
- Playback cache for streamed tracks; permanent pin/download; offline cached music service and cached playlists — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/OfflineMusicService.java).
- User-defined cache size/location and clear-cache; separate Wi-Fi/mobile prefetch counts; network timeout; bitrate limits; Wi-Fi/local-network restrictions — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_cache.xml).
- Scheduled sync enable/interval, Wi-Fi-only and notification controls; automatic starred, recently-added, and library synchronization options — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_sync.xml).
- Multiple server configurations; local network SSID and alternate local address; tag-browsing preferences — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SettingsFragment.java).
- Server transcoding to a supported format/bitrate; original versus transcoded details visible — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/README.md).

#### Android and external playback
- Background service, media controls and lockscreen controls; pause for focus loss/calls; media/headset buttons; headphones-connect autostart and unplug pause settings — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_playback.xml).
- Android Auto media browsing (library, folders, playlists and album collections) through AutoMediaBrowserService — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/AutoMediaBrowserService.java).
- Home-screen widgets at 4x1, 4x2, 4x3, 4x4; optional hiding after app exit — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/AndroidManifest.xml).
- Tasker/Locale configuration activity for start-playing/shuffle/offline automation — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/AndroidManifest.xml).
- Chromecast support in Google flavor only; FLOSS flavor excludes Google Cast dependencies — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/build.gradle).
- DLNA/UPnP renderer playback — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/DLNAController.java).
- Server jukebox remote controller, conditional on server support and user permission — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/JukeboxController.java).
- Cast device proxy; gapless casting option; original stream selection; optional caching while casting; DLNA switch — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_cast.xml).
- Scrobble to server, conditional on server support — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_playback.xml).
- Themes, fullscreen, album-art/track/file-suffix display, custom sort, colors and configurable context actions — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_appearance.xml).
- Show/hide drawer sections and choose initial screen — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_drawer.xml).

#### Music-adjacent appendix (not core music implementation candidates)
- Podcast channels and episodes: add/delete channels, refresh feeds, request server download, download/cache locally, queue and play; podcast sync — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_podcasts_context.xml); [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_podcast_episode_context.xml).
- Internet radio browsing/playback — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectInternetRadioStationFragment.java).
- Video browsing/playback and configurable player/bitrate — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_playback.xml); [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/select_video_context.xml).
- Server chat and administration sections, conditional on server permissions — [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_drawer.xml); [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/menu/admin.xml).
- Bookmarks and speed also apply to spoken audio; a dedicated audiobook library or chapter UI is not established. [DSub2000 source](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/fragments/SelectBookmarkFragment.java)

### Inferences
- Automated favorite/playlist synchronization, prefetch limits and eviction controls are separate from a basic manual download button. An existing manual-download feature does not cover them.
- Repeat/next-track/queue features need to be verified as actual music behavior, including headset actions. A podcast-style ±seconds seek action does not establish track skip.

### Gaps
- Audio crossfade, word-synchronized lyrics, modern OpenSubsonic API-key login, and exclusive USB DAC control were not established in released 5.7.5. A newer default edge branch contains USB-related work; it is excluded from this release inventory.
- DSub2000 has plain lyrics but offline embedded lyrics remains an open request: [issue #150](https://github.com/paroj/DSub2000/issues/150).
- Do not claim perfect Android Auto offline browsing: source exists, but support requests and physical-device behavior require separate verification. [Android Auto offline issue](https://github.com/paroj/DSub2000/issues/119).

## Tempo feature inventory

### Takeaway
Original Tempo 3.9.0 adds a personalized but library-based music home, instant mixes/radio, line-synchronized lyrics, flexible server transcoding and downloads, and a modern editable queue. Its podcast-only speed/silence controls, external device equalizer and GitHub-only cast/car integrations need precise labeling.

### Cited Findings

#### Browsing and discovery
- Album, artist, genre and playlist catalogs; music folders and directory navigation (requires server directory support) — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/values/strings.xml).
- Search artists, albums and songs (three-character minimum) — [search resources](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/values/strings.xml). In-list search/sort including playlist songs and added album sorting — [3.9.0 release](https://github.com/CappielloAntonio/tempo/releases/tag/3.9.0).
- Home: last played, recently added, most played, new releases, starred artists/albums/tracks, discovery, top songs, flashback, favorite-artist best-of and liked-song mixes — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/HomeTabMusicFragment.java).
- Customize home section visibility/order; pin playlists to home — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/dialog/HomeRearrangementDialog.java).
- Album/song sorting by names, artists, year, most/recently played, recently added, recently/least recently starred as available — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/menu/sort_album_popup_menu.xml).
- Artist biography, albums, most-streamed/top songs, artist radio, similar artists; album more-like-this and release/original release dates — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/ArtistPageFragment.java).
- Instant mix from song/album/artist; shuffle artist; filter random/mix tracks below user-rating threshold — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/bottomsheetdialog/SongBottomSheetDialog.java).
- Decade/year and multi-genre filtering — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/README.md).

#### Playlists, favorites, sharing
- Create/name/rename/delete playlists; add tracks; remove tracks and reorder playlist songs — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/dialog/PlaylistEditorDialog.java).
- Play or shuffle a playlist; playlist duration/track count; download all; pin to home; search playlist songs — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/menu/playlist_page_menu.xml).
- Star/unstar tracks, albums, artists; display rating/favorite marks; rate songs — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/bottomsheetdialog/SongBottomSheetDialog.java).
- Create share links for individual tracks/albums/playlists; list/copy links; update description/expiration; delete shares; requires server sharing permission — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/values/strings.xml).

#### Playback and queue
- Play album/playlist/track; actual previous/next music tracks, seek bar, shuffle, repeat off/all/one; music resets speed to 1x — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/PlayerControllerFragment.java).
- Add to queue and play next for tracks/albums/downloaded items; drag queue reorder, swipe remove, clean queue, shuffle remainder/selected range — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/PlayerQueueFragment.java).
- Queue persistence in local database — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/database/dao/QueueDao.java).
- Optional server queue save/restore with current track and position; configurable restore countdown — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml).
- Gapless playback — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/README.md).
- Continuous play after queue/playlist ends by adding similar songs — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/service/MediaManager.java).
- ReplayGain disabled/track/album/automatic, reading ReplayGain and R128 metadata tags — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/util/ReplayGainUtil.java).
- Equalizer launches the installed Android audio-effect control panel; hidden if no panel exists — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/SettingsFragment.java).
- OpenSubsonic structured synchronized lyrics; current-line highlighting and automatic scrolling (called karaoke); fallback basic server lyrics — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/PlayerLyricsFragment.java).
- Track info: album/artist/title, track/disc, duration, genre/year, format/bitrate, path/size, original/transcoded playback details — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/dialog/TrackInfoDialog.java).

#### Offline, network, quality
- Download tracks, albums and playlists; remove downloaded items; downloads view grouping/filtering and shuffle; offline mode with documented multi-server limitations — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/README.md).
- Directory download explicitly downloads this folder only, not nested subfolders — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/dialog/DownloadDirectoryDialog.java).
- Sync starred tracks to offline storage — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml).
- Streaming cache budget and image cache/resolution; buffering strategy; internal/external cache/download storage; clear downloads — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml).
- Wi-Fi-only alert before mobile streaming; mobile data saver avoids cover downloads — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/values/strings.xml).
- Separate Wi-Fi/mobile bitrate and format selections; prefer server transcode settings or force direct/raw; separate transcoded-download settings; estimate-length option — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml).
- Multiple server profiles; username/password; primary/local URL with automatic fallback; optional low-security connection mode — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/dialog/ServerSignupDialog.java).
- ALAC decoding through FFmpeg Media3 extension; GitHub version-update checks — [3.9.0 release](https://github.com/CappielloAntonio/tempo/releases/tag/3.9.0).
- Request server library scan — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/SettingsFragment.java).

#### Android and external playback
- Background MediaLibraryService with media session, audio focus handling, noisy-output unplug pause and notification controls — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/tempo/java/com/cappielloantonio/tempo/service/MediaService.kt).
- Android Auto browse/play/search integration in full GitHub flavor, including albums/artists/genres/playlists/starred/recent/random — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/tempo/java/com/cappielloantonio/tempo/service/MediaBrowserTree.kt).
- Chromecast in full GitHub flavor; README calls the support rudimentary — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/README.md).
- Server scrobbling, including now-playing and played submissions; optional server Last.fm integration — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/service/MediaManager.java).
- Theme/language, rounded artwork, optional always-on display, audio-quality/rating indicators and show/hide directory/radio/podcast sections — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml).

#### Music-adjacent appendix (not core music implementation candidates)
- Podcasts: list/add/delete channels, episode feed/filtering, request server download, local download/removal, queue/play — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/repository/PodcastRepository.java).
- **Podcast only:** 0.8x/1x/1.25x/1.5x/2x speed, silence-skip and rewind/fast-forward controls. Music hides them and resets speed to normal — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/ui/fragment/PlayerControllerFragment.java).
- Internet radio: create/edit/delete station name, stream/homepage URLs; playback — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/repository/RadioRepository.java).

### Inferences
- Tempo “karaoke” means line-based synchronized lyrics, not word timing; if Player has synchronized highlighting/autoscroll, this is already covered in substance.
- A bookmark-shaped header icon restores the server play queue; the actual endpoint implementation is getPlayQueue/savePlayQueue. It is not evidence of independent named timestamp bookmarks — [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/java/com/cappielloantonio/tempo/repository/QueueRepository.java).

### Gaps
- No sleep timer, audio crossfade, DLNA/UPnP output, jukebox UI, home-screen playback widgets, dedicated audiobook/chapter views, named timestamp-bookmark CRUD, or music speed control was established in original Tempo 3.9.0. Do not import these claims from Tempus or other forks.
- The source has generated Subsonic model classes for some unused endpoints. A model such as JukeboxPlaylist alone does not establish a usable client feature.
- Synchronized lyrics are conditional on the server OpenSubsonic lyrics extension and available timed data. Download support does not establish complete offline library parity or robust multi-server offline behavior; README explicitly warns about limitations. [Tempo source](https://github.com/CappielloAntonio/tempo/blob/3.9.0/README.md)
