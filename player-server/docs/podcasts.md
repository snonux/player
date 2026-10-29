Podcast Support
===============

Per-account subscriptions, stable feed and episode identities and their
synchronization are specified in
[podcast-sync-contract.md](../../docs/podcast-sync-contract.md). Implemented:
identities, the migration, subscription and episode state sync
(`POST /api/v1/podcasts/sync`, advertised as `podcast_sync` in
`/api/v1/config`), feed and episode merges, legacy identity retirement,
and the catalog/fetch rules below. The main cross-client journey has been
exercised in Chrome, Firefox and two installed Android clients; task `5i3`
still tracks boundary checks in the
[verification plan](../../docs/podcast-management-plan.md#integrated-verification-snapshot-2026-10-26).

Podcasts are **special sets** (`sets.is_podcast = 1`). They reuse set permissions, browsing, and cover images, while adding feed management and episode tracking.

Global **Sync now / Retry** in the header retries podcast subscriptions and
episode edits, pulls all podcast pages, flushes queued listening progress and
synchronizes the saved music session without changing active playback. Status
shows activity, pending work and the account's last complete success; partial
failure preserves that clock and the last applied pull page. After 48 hours it
shows the age; an account that has never completed a run says **Not synced yet**.
An expired session offers **Sign in** rather than reporting a server outage.
Podcast pending edits are kept only in the open tab; keep it open until synced.
The browser also performs quiet full passes while visible and on reconnection;
it does not promise background execution after the tab closes. Native Android
uses its existing durable coordinator and a labeled global control. Partial
success, stale-age and authentication behavior still require actual platform
checks before `5i3` can close.

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

Web playback attempts scoped reconciliation for at most three seconds before
starting, then uses its in-tab state when synchronization is unavailable.
Late responses update storage without jumping playback. Once loaded, the
player's actual duration decides whether a saved position within five seconds
of the end starts from zero. **Play from beginning** is separate from manual
completion; replaying keeps completion unchanged. Natural HTML end writes the
end position and automatic completion together, without waiting for the server
before advancing. Seek, pause, stop and errors only save positions. Cast keeps
podcast positions but does not infer completion from receiver state changes.

The subscription folders and All podcast episodes view update their progress
and completion filters. Failed web edits stay in memory in the current account's
tab and offer **Retry**; reloading loses unsent edits. Synchronizing never
downloads audio. **Download to server** and **Download and play** explicitly do.

**Delete downloaded file** uses the existing owner/admin media Trash action.
It affects every server listener, preserves the episode, subscriptions and
listening registers, and does not affect Android copies. The source stays in
Trash for seven days and can be restored there; expiry removes the file and
its media-scoped notes, bookmarks, favorites and shares. A cleanup failure
retains the trashed row for the existing GC retry. Unsubscribe and the admin
Remove from server action remain separate operations.

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

The web UI lists subscriptions by `feed_key`: a folder per subscribed podcast
and a virtual **All podcast episodes** view (`GET /api/podcasts/episodes`).
Both default to **Uncompleted** (started included) and offer **Completed** /
**All**, with server-side pagination. Every episode — downloaded or not —
stays in these views with **Mark completed** / **Mark uncompleted** (via
`POST /api/podcasts/sync` completion edits) and clear download / resume
state. `server_copy.media_id` is present only when a readable server copy
exists; download still uses `POST /api/podcasts/episodes/{id}/download`.

The shared podcast set browse grid still shows undownloaded episode cards
and downloaded media as ordinary cards; prefer the subscription views for
completion filters and feed identity.

### Background Feed Checker

A background goroutine (`CheckFeeds`) refreshes subscribed feeds every hour (configurable via `PODCAST_CHECK_INTERVAL_MINUTES`). It uses conditional GET (`If-None-Match`, `If-Modified-Since`) to avoid re-downloading unchanged feeds.

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/podcasts` | List catalog feeds (with the caller's `subscribed`) |
| `POST` | `/api/podcasts` | Subscribe to a new feed (admin) |
| `POST` | `/api/podcasts/sync` | Synchronize the caller's subscriptions and episode state |
| `DELETE` | `/api/podcasts/{id}` | Remove a feed's server copies (admin); the feed too when nobody subscribes |
| `GET` | `/api/podcasts/episodes` | Subscribed / all-episodes view (`feed_key`, completion filter, page) |
| `GET` | `/api/podcasts/{id}/episodes` | List episodes by set id (legacy) |
| `POST` | `/api/podcasts/episodes/{id}/download` | Server-side download |
| `POST` | `/api/podcasts/episodes/{id}/complete` | Toggle completion (legacy; prefer sync) |

## Explore available podcasts

Open **Explore podcasts** in the Podcasts sidebar or **Podcasts on this device**.
Search Apple Podcasts by name, author or topic, or choose a chart country and
press **Load popular podcasts** to browse up to 20 current chart shows. Search
returns up to 30 shows. Each result has one Subscribe action; it uses the same
account subscription or browser device feed workflow as adding an RSS URL.
**Add by feed URL** remains available for feeds outside Apple's directory.
The native Android Podcasts screen has the same Explore action and an
**Add feed URL** menu, and saves subscriptions through its device repository.

Loading is explicit: opening Explore sends no directory request, and search
results load no artwork. Browser searches and chart requests go through the
Player server, which sends the query or chosen country and its own IP address
to Apple. Native Android contacts Apple directly, so Apple also sees the
device IP address. Explicit searches use the current connection, including
mobile data; they do not enable background fetching. Subscribing contacts the
publisher under the existing feed/network policy. Browser device feeds still
need publisher CORS permission; if blocked, use a signed-in account or native
Android. Unavailable directories show a retry message and keep URL entry usable.

The directory needs no API key. Search uses Apple's documented iTunes Search
API; charts use Apple's public `/{country}/rss/toppodcasts/limit=20/json` feed
and a bounded ID lookup for RSS addresses. The server only contacts
`https://itunes.apple.com`, checks public destination addresses and same-host
HTTPS redirects, limits each response to 2 MiB and each request to 15 seconds,
caches up to 32 searches/charts for 15 minutes, and spaces uncached searches by
3 seconds (charts reserve 6 seconds for two requests). Directory outages do not
affect saved subscriptions or playback.

Provider references: [AntennaPod's subscription guide](https://antennapod.org/documentation/getting-started/subscribe),
[AntennaPod discovery privacy](https://antennapod.org/privacy/),
[Apple iTunes Search API](https://performance-partners.apple.com/search-api),
and [Apple Podcast Charts](https://podcasters.apple.com/support/3146-apple-podcasts-charts).


Account discovery saves the subscription before fetching episode metadata, so
browsing stays available during publisher requests. A failed metadata refresh
keeps the subscription and offers **Retry fetching episodes**; each feed's
episode view also has **Refresh feed**. Subscribed feeds appear in the sidebar
from the user's own sync registers even when the user has no permission to the
server's shared podcast set. Subscription and metadata refresh grant no access
to server audio copies or folders.

## Episode metadata

Episode cards show publisher artwork (episode artwork first, feed artwork as a
fallback), a labeled publication date, and expandable **Show notes**. Notes are
rendered as readable text with safe HTTP(S) links; publisher HTML cannot execute
scripts or load embedded resources. Missing dates are explicitly marked
unavailable, and feeds without notes omit the expander.

Refreshing a feed updates publisher metadata without changing episode keys,
progress, completion, or downloaded-file links. Existing server copies expose
optional `media.podcast` metadata, so their display title uses the RSS episode
title while the original filename and embedded `music` tags remain intact.
