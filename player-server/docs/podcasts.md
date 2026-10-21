Podcast Support
===============

Per-account subscriptions, stable feed and episode identities and their
synchronization are specified in
[podcast-sync-contract.md](../../docs/podcast-sync-contract.md). Implemented
so far: identities, the migration, subscription and episode state sync
(`POST /api/v1/podcasts/sync`, advertised as `podcast_sync` in
`/api/v1/config`), feed and episode merges, legacy identity retirement,
and the catalog/fetch rules below. Platform verification belongs to task
`5i3`; server tests alone do not verify Chrome, Firefox or Android.

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
user per rolling hour). Subscriptions beyond that quota are kept and the
background checker creates their catalog rows when quota becomes available;
no resubscribe is needed. Unsubscribing cancels deferred creation. Removing
a catalog row does not reset the hourly quota. Catalog rows created by non-admin users are fetched only
from public unicast addresses. The checker refreshes a catalog feed only
while at least one user is subscribed to it. Permanent redirects (301, 308)
and `<itunes:new-feed-url>` move a feed's fetch URL and record the new
URL's key as an alias; temporary redirects change nothing. A move into an
existing catalog feed merges into that target. Only the source's
subscriptions move; the target keeps its identity and its listeners. Each
user's subscription, position and completion registers merge independently
by edit time, with ties keeping the target value. Losing catalog rows stop
refreshing and retain their episodes and server copies. Episode lists show
each canonical episode once and find readable server copies across all
those rows.

A corrected or permanently redirected `legacy-feed:` key becomes an alias
of the valid feed key. Ingest retires a `legacy-episode:` key when its stored
enclosure identifies the episode. Retired keys remain accepted from old
clients. State stored under a key before it became an alias moves to its
canonical key. Episode merges keep the older catalog identity and retain
both copies. Ordinary redirect and GUID alias creation is capped at 16 per
identity; merges retain inherited mappings and retired identities so existing
client keys keep working.

In the web UI (admin **Podcasts** dialog) **Unsubscribe** only changes the
admin's own subscription; nothing is deleted. **Remove from server**
deletes the feed's downloaded episodes for everyone (after a confirmation),
and every catalog row sharing its canonical key only when nobody is
subscribed any more. A failed file removal keeps the media and episode link
so the request can be retried; other copies are still attempted.

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