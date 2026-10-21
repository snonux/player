Podcast Support
===============

Per-account subscriptions, stable feed and episode identities and their
synchronization are specified in
[podcast-sync-contract.md](../../docs/podcast-sync-contract.md). Implemented
so far: identities, the migration, subscription and episode state sync
(`POST /api/v1/podcasts/sync`, advertised as `podcast_sync` in
`/api/v1/config`) and the catalog/fetch rules below. Feed merges and the web
subscription views follow in later tasks.

Podcasts are **special sets** (`sets.is_podcast = 1`). They reuse set permissions, browsing, and cover images, while adding feed management and episode tracking.

### Subscribing

Admin opens the **Podcasts** button in the admin panel (or calls `POST /api/podcasts`):
- Submit an RSS/Atom feed URL and optional folder name.
- Server creates a set folder, parses the feed, downloads the cover image, and inserts episodes into `podcast_episodes`.

### Subscriptions and the catalog

Each user has their own subscription registers, keyed by stable feed keys
and synchronized between clients. The feeds the server fetches (the
catalog) are shared: a sync subscription to an unknown feed creates a
catalog row without folders, permissions or audio downloads (at most 20 per
user and hour). Catalog rows created by non-admin users are fetched only
from public unicast addresses. The checker refreshes a catalog feed only
while at least one user is subscribed to it. Permanent redirects (301, 308)
and `<itunes:new-feed-url>` move a feed's fetch URL and record the new
URL's key as an alias; temporary redirects change nothing. Exceptions: a
move to a URL whose key belongs to another catalog feed would need a merge
(not implemented yet) and is only logged, the fetch URL stays; a feed with
a `legacy-feed:` key gets the new fetch URL but no alias; at most 16 aliases
are kept per feed, later moves still change the fetch URL.

In the web UI (admin **Podcasts** dialog) **Unsubscribe** only changes the
admin's own subscription; nothing is deleted. **Remove from server**
deletes the feed's downloaded episodes for everyone (after a confirmation),
and the feed itself only when nobody is subscribed any more.

### Listening state

Each user's position and completion of every episode is per-account episode
state, synchronized between clients and kept even when the server never
downloaded the episode. It is the only listening history of an episode:

- the listened/unlistened checkmark and the episode listing read and write
  it (the checkmark is a manual completion edit, the position is kept);
- the media progress of a downloaded episode (resume position, **Mark as
  finished**, **Not started**, offline playback sync of old Android
  clients) edits it, and the downloaded copy's progress shows its
  projection: a completed episode that is being replayed reads as
  unfinished, so it resumes where it was and appears under **In progress**;
- downloading an episode later starts from that state, and deleting the
  server copy keeps it.

Episodes are matched across fetches by stable keys (GUID, else enclosure
URL), so GUID-less episodes, reordered items and changed GUIDs no longer
create duplicates or get lost.

On upgrade, one migration keys every existing feed and episode, gives every
user who could see a feed a subscription to it, and moves existing
completion and playback progress of downloaded episodes into per-account
episode state. It touches no files and grants no permissions.

### Episode Management

Episodes are stored in `podcast_episodes` and rendered in the browse grid for podcast sets:
- **Undownloaded** episodes show a **Download to server** button (calls `POST /api/podcasts/episodes/{id}/download`).
- **Downloaded** episodes become regular `media` rows and appear as normal media cards.
- Users can mark episodes as listened/unlistened via the checkmark button.

### Background Feed Checker

A background goroutine (`CheckFeeds`) refreshes subscribed feeds every hour (configurable via `PODCAST_CHECK_INTERVAL_MINUTES`). It uses conditional GET (`If-None-Match`, `If-Modified-Since`) to avoid re-downloading unchanged feeds.

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/podcasts` | List catalog feeds (with the caller's `subscribed`) |
| `POST` | `/api/podcasts` | Subscribe to a new feed (admin) |
| `POST` | `/api/podcasts/sync` | Synchronize the caller's subscriptions and episode state |
| `DELETE` | `/api/podcasts/{id}` | Remove a feed's server copies (admin); the feed too when nobody subscribes |
| `GET` | `/api/podcasts/{id}/episodes` | List episodes with the caller's episode state |
| `POST` | `/api/podcasts/episodes/{id}/download` | Server-side download |
| `POST` | `/api/podcasts/episodes/{id}/complete` | Toggle completion |