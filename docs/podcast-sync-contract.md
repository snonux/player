# Podcast identity and synchronization contract

Status: **specification**, task `uh3` of the
[podcast plan](podcast-management-plan.md). Nothing in this document is
implemented yet except the identity reference functions in
`player-server/internal/podcast/identity.go` and their shared test vectors in
`player-server/internal/podcast/testdata/identity-vectors.json`. The current
API remains documented in [`player-server/docs/api.md`](../player-server/docs/api.md);
update it as each delivery task (`vh3` … `4i3`) ships its part. Platform
verification (Chrome, Firefox, two Android clients) belongs to `5i3`.

Words in **must** / **must not** are requirements on the implementing tasks.

## 1. Current behavior (audit)

| Area | What the code does today | Consequence for this contract |
| --- | --- | --- |
| Subscription ownership | Feeds are global rows in one shared podcast set (`sets.is_podcast`, `podcast_feeds.set_id`). `POST /api/podcasts` is admin-only (`podcastSubscriptionService.SubscribeFeed` → `verifyAdmin`) and grants the admin an `owner` row on the set (`ensurePodcastSet`). `GET /api/podcasts` lists every feed whose set the caller may read (`ListFeeds` → `checkSetPermission`). | There is no per-user subscription. Migration must create one per user who can see a feed today (§9). |
| Unsubscribe | `DELETE /api/podcasts/{id}` (`UnsubscribeFeed`, `service/podcast_unsubscribe.go`) deletes the feed, cascades `podcast_episodes` and `podcast_status`, hard-deletes the episode media and files, and with them every user's notes, bookmarks, progress, favorites and shares. The web Unsubscribe button calls it (`web/js/podcasts.js`, `unsubscribe`). | Must not be used for per-user unsubscribe, sync, or file cleanup (§7.4). |
| Episode status | `podcast_status(user_id, episode_id, is_completed, position_seconds, updated_at)`. The only writer is `ToggleEpisodeComplete` (`service/podcast_episode.go`), a read-then-flip that is not retry-safe and always writes `position_seconds = 0`. | A retried toggle undoes itself. `position_seconds` never holds a real position. |
| Media progress | `playback_progress(user_id, media_id, position_seconds, finished, updated_at, accumulated_seconds)` plus `playback_resets` (edit-time barrier) and `media_bookmarks`. `SyncPlayback` (`repository/playback_sync.go`) merges one progress register and bookmark registers by edit-time milliseconds; equal times keep the server value. A finish writes `position = duration` (`progressService.SetProgressStatusAt`); a position save writes `finished = 0`. | Server-downloaded episodes already have a second, media-scoped history, keyed by a server media id Android must not depend on, and deleted with the media row (`ON DELETE CASCADE`). |
| Android sync | `OfflineDownloadRepository` keeps `sync_items` / `sync_bookmarks` per `(origin, user_id)` with `pending` flags that survive restarts and copy removal; `OfflinePlaybackSync` posts them to `/media/{id}/playback-sync`, guards every step with the account generation (`OfflineSyncAccount.current`) and waits at most 5 s before an offline playback (`syncKey`). `ProgressQueue` batches streamed-media positions with `observed_at`. | The clock, tie, tombstone, account-scoping and durability rules below are taken from this code. |
| Episode identity | `podcast_episodes` is `UNIQUE(feed_id, guid)`; `ParseFeed` copies `item.GUID` verbatim, empty when missing. `CheckFeeds` → `upsertFeedEpisodes` skips known GUIDs, including the empty one. | Only the first GUID-less item of a feed is ever stored: the feed checker skips later ones as already known, and on subscribe their insert fails. A changing GUID creates a new episode. |
| Feed URL | `doFeedRequest` lets Go's client follow redirects; `feed_url` is never updated. `findExistingFeed` matches by exact URL, then by title. | Moved feeds keep fetching through redirects forever; `http`/`https` variants are different feeds. |
| Re-subscribe | `insertPodcastEpisodes` stops at the first `CreateEpisode` error, so re-subscribing an existing feed inserts nothing after the first known episode. | Ingest must be an idempotent upsert per episode key. |
| Download flag | `podcast_episodes.media_id` is `ON DELETE SET NULL`, but `is_downloaded` is not cleared when the media row goes. | Server availability must be derived from a live media row, not from `is_downloaded`. |
| Fetch policy | Feeds are fetched only for admin-created rows; no outbound address restriction exists. `auto_download` is stored but unused. | Once non-admins can create catalog feeds through sync, an outbound policy is needed (§7.3). |

## 2. Model overview

Three kinds of state, deliberately kept apart:

1. **Catalog** (server only, shared): feed and episode metadata fetched by the
   server, and the server's downloaded copies (`media` rows). Governed by the
   existing set permissions of the podcast set.
2. **Per-account state** (synchronized): the subscription register per feed,
   and the position and completion registers per episode. Keyed only by
   stable identities (§3), never by database ids, server media ids or local
   file paths. Owned by exactly one user account.
3. **Per-device availability** (never synchronized): downloaded bytes and
   download flags on each Android device and on the server.

Android keeps its own catalog (feed metadata fetched directly from the
publisher) and its own per-scope state; the server is the hub that merges
per-account state between clients. Sync never transfers audio and never asks
the server to download anything.

## 3. Stable identities

### 3.1 Feed key

`feed_key = "feed:" + canonical_location(url)`; reference implementation
`podcast.FeedKey`. The algorithm works on bytes, not on a URL library, so the
Go and Dart implementations produce identical keys:

1. Trim ASCII whitespace (space, `\t\r\n\f\v`; Unicode spaces such as
   U+00A0 or U+FEFF are not whitespace here). Reject input that is not valid
   UTF-8, is empty or longer than 2048 bytes, or has any remaining byte
   `<= 0x20` or `0x7f`.
2. The scheme (`ALPHA *(ALPHA / DIGIT / + / - / .)` before the first `:`) is
   compared case-insensitively. `feed`, `itpc`, `pcast` and `podcast` mean
   `http`; a wrapper around a complete URL (`feed:https://…`,
   `feed://https://…`) is unwrapped once, and a nested pseudo scheme is
   rejected. Only `http` and `https` remain valid. The rest must start with
   `//`.
3. The authority ends at the first `/`, `?` or `#`. Reject any `@`
   (credentials never become identity) and any `\` (browsers read it as a
   path separator). Split host and port (`[v6]` literals included).
   Lower-case the ASCII letters `A-Z` of the host only, drop one trailing
   `.`; reject an empty host. Non-ASCII (IDN) hosts are kept byte for byte:
   there is no Unicode case mapping and no punycode conversion, so the same
   feed typed once in Unicode and once in punycode gives two keys (known
   limit). A port must be decimal 1–65535; it is dropped when empty or equal
   to the default port of the URL's own scheme (`http:80`, `https:443`),
   else written without leading zeros.
4. Drop the fragment. An empty path becomes `/`. Path and query are kept
   verbatim, except that every non-ASCII byte is written as `%XX` and the
   hex digits of existing `%xx` escapes are upper-cased (so `café` and
   `caf%c3%a9` are one key). An empty query (`?` alone) is dropped. Query
   parameters are not reordered.
5. The scheme is **not** part of the key: `http` and `https` variants of a
   URL are one feed.

All lengths are UTF-8 bytes, never characters or UTF-16 units.

The fetch URL is stored separately and may change; the key is minted once
and never changes for that feed (redirects become aliases, §3.3).

Besides these shapes, the server mints `legacy-feed:<n>` and
`legacy-episode:<n>` (decimal row ids) during migration for rows whose
stored URL cannot be canonicalized or whose episode key collides (§9.1).
Clients never mint them but must store and echo them unchanged; the server
accepts them only when such a key exists in its catalog. A legacy key is
retired by a merge (§3.3): `legacy-feed` when the stored URL is replaced by
a valid one (a corrected URL or a permanent redirect), `legacy-episode` when an
ingest claims the episode by its stored enclosure. It then remains an alias.

### 3.2 Episode key

Computed from one feed document, items in document order (reference
`podcast.EpisodeKeys`):

1. The enclosure is the item's first `<enclosure>` (gofeed
   `Enclosures[0]`). Its URL is not resolved against the feed URL: an item
   whose first enclosure is missing, relative or does not canonicalize
   (same algorithm, prefix `enc:`) is not a podcast episode and is ignored.
2. The trimmed GUID (`<guid>`, Atom `<id>`; compared byte-for-byte and
   case-sensitively, `isPermaLink` ignored; a GUID that is not valid UTF-8
   counts as missing) of the first item carrying it gives `guid:<guid>`.
   GUIDs longer than 1024 bytes become `guid:sha256:<64 lower-case hex>`.
3. An item without a GUID, or repeating a GUID already used earlier in the
   document, uses `enc:<canonical enclosure>`; if an earlier item already has
   that enclosure, the item is a duplicate and ignored.

Title and publication date are deliberately not identity: publishers edit
them. Each catalog episode stores the enclosure key of the latest document
that carried it (`enclosure_key`, overwritten on every ingest, not an alias
and never synchronized), so signed or rotating enclosure URLs do not
accumulate anything.

### 3.3 Aliases and resolution

The server keeps a global alias table for feeds (`alias_key → feed_key`) and
per feed for episodes (`(feed_key, alias_key) → episode_key`). Only the
server's own observations create aliases or merges: what a client sends is
used to look keys up, never to link two keys, so one user can never merge
or redirect another user's feeds.

- **Permanent redirects** (301, 308) seen while fetching, and an
  `<itunes:new-feed-url>` element, add the target's key as a feed alias and
  replace the stored fetch URL. Temporary redirects (302, 303, 307) are
  followed but change nothing. The fetcher must record permanent hops through
  `http.Client.CheckRedirect`. At most 16 aliases per feed are kept.
- **Episode ingest** (reference `podcast.MatchEpisodes`; the Android device
  catalog uses the same function on its own fetches) resolves a whole
  document at once, and every known episode is matched by at most one item:
  1. *Key pass*, in document order: an item whose key is a known key or alias
     of the feed claims that episode, unless its enclosure key singles out a
     different episode whose canonical key is an `enc:` key (that episode
     exists because the GUID was ambiguous). The `guid:` key of a GUID that
     repeats in the document is ambiguous and not used in this pass.
  2. *Enclosure pass*: an unmatched item claims the single episode whose
     `enclosure_key` (or `enc:` key or alias) equals its enclosure key, only
     if that episode is unclaimed and its own key is not the unambiguous key
     of any item in the document, i.e. its GUID changed, vanished or became
     ambiguous. If several episodes share the enclosure, this pass does
     nothing.
  3. *New pass*: every remaining item becomes a new episode under its key,
     or under its enclosure key when its key already belongs to an episode;
     if that is taken too, the item duplicates another and is ignored.

  When an item claims an episode under a different, unambiguous key that
  belongs to no other episode, that key becomes an alias of the episode
  (GUID appeared, changed or vanished), up to 16 aliases per episode. Two
  items of one document that resolve to the same episode: the first claims
  it, the second goes on to the next pass. Ingest is an idempotent upsert;
  re-ingesting the same document changes nothing.
- **Incoming client keys** (§6) are resolved in this order: canonical key;
  alias; evidence (each of a feed's `urls` as a feed key, then an episode's
  `enclosure_url` against `enclosure_key`s of the feed, used only when it
  identifies exactly one episode); otherwise the client's key is kept for
  that user's state (and, for a subscription, a catalog row is created,
  §7.3). The server returns every mapping it applied (`resolved`).
- **Client rewrite**: a client applies `resolved` and pulled `aliases` by
  rewriting local keys in one transaction. When the target key already
  exists locally, the two rows are merged register by register (§4): the
  register with the greater clock wins, together with its pending edit; the
  loser's pending edit is dropped (it can only lose on the server too); equal
  clocks keep the target row. Device catalog rows and downloads are re-keyed
  the same way.
- **Merges**: when a server observation shows that two canonical keys denote
  one feed or episode (an alias that is already another canonical key), they
  are merged. For feeds the redirect target always wins, so a feed that
  redirects into another can only move its own subscribers and never
  rewrites the keys of the target's subscribers. For episodes the older
  catalog entry (smaller row id) wins; for state-only keys the
  lexicographically smaller key. State rows of the loser are merged into the
  winner with the normal register rules (§4) and get new change sequence
  numbers; the loser key becomes an alias. A losing catalog feed row is
  marked `merged_into`, stops refreshing, and keeps its episode rows and
  server copies; availability lookups consult all catalog rows sharing a
  canonical key. Nothing is deleted.

Known limits: an episode whose GUID and enclosure URL both change in one
fetch becomes a new episode; a GUID-less episode whose enclosure URL changes
on every fetch (signed URLs) becomes a new episode on every fetch. In both
cases the old state stays under the old key.

### 3.4 Test vectors

`player-server/internal/podcast/testdata/identity-vectors.json` (version 2)
is the shared fixture: feed keys, invalid URLs, episode documents (including
Unicode whitespace, multibyte and overlong GUIDs, length limits) and ingest
scenarios (repeated GUIDs reordered, shared enclosures, changed, appearing
and vanishing GUIDs, rotating enclosures, the alias cap). The Go tests in
`internal/podcast/identity_test.go` consume it. The Android implementation
(`xh3`) **must** add a unit test that reads the same file through
`../player-server/internal/podcast/testdata/identity-vectors.json`; a client
that disagrees with a vector is wrong. A change to any rule is a contract
change: bump `version` in the file and update both implementations together.

## 4. Registers and merge rules

All per-account state consists of last-writer-wins registers, as in the
existing `SyncPlayback`:

| Register | Key | Value | Clock |
| --- | --- | --- | --- |
| Subscription | `(user, feed_key)` | `subscribed` (bool), plus non-merged display data: `title`, `fetch_url`, `urls` | `subscription_ms` |
| Position | `(user, feed_key, episode_key)` | `position_seconds` | `position_ms` |
| Completion | `(user, feed_key, episode_key)` | `completed` (bool), `source` (`manual` / `auto`) | `completion_ms` |

Rules:

1. A clock is the UTC Unix millisecond at which the user (or player) made the
   edit, captured once on the device and kept through every retry, restart
   and reconnect. It is never the send, receive or migration time, except for
   server-side edits, which use server time.
2. An incoming edit replaces the register only if its clock is strictly
   greater. Equal clocks keep the stored value, so a retried edit is a no-op.
   A client adopts a server register when the server clock is greater than
   or equal to its own, so all replicas converge on the hub's value.
3. Successive local edits of one register are strictly increasing
   (`max(now, previous + 1)`), as `saveProgress` already does with
   `MAX(observed_ms+1, ?)`. A server action in the same millisecond as the
   stored clock advances by 1 ms (`serverProgressTime`).
4. Clock skew: the server treats a clock more than 5 minutes ahead of its own
   time as its own receive time and reports the item in `clamped`. Without
   this, a device with a future clock would block every later edit. The cost
   (a lost acknowledgement plus a retry can re-apply that edit with a later
   clock) is accepted and must be covered by a test.
5. Registers merge independently. Position and completion never overwrite
   each other: a manual completion keeps the saved position, and saving a
   position never changes completion. (This is the deliberate difference from
   the media `progress` register, where a finish moves the position to the
   end and a position save clears `finished`.)
6. Subscription removal is a tombstone (`subscribed = false`), retained
   indefinitely and synchronized like any other value, so a stale device
   cannot resurrect a removed subscription. Unsubscribing never touches
   episode state; re-subscribing later shows the old history again.
7. There is no deletion of episode state. "Reset" is a pair of edits:
   position `0`, completion `false`.

### 4.1 Conflict examples (all deterministic)

| Situation | Result |
| --- | --- |
| Phone A offline marks completed at t1; phone B online saves position at t2 > t1; A syncs at t3. | Completed (A, t1) and position (B, t2): different registers, both kept. |
| A offline unsubscribes at t1; B re-subscribes at t2 > t1; A syncs later. | Subscribed (t2). A adopts it. |
| A offline unsubscribes at t2; B subscribed at t1 < t2 and synced first. | Unsubscribed (t2), on every client after their next pull. |
| A and B both edit completion offline, A at t1, B at t2 > t1, A syncs last. | B's value (t2). A's delayed edit is acknowledged but superseded. |
| Two edits with the same millisecond from different devices. | The one the server stored first; the other device adopts it (rule 2). |
| A's response is lost after the server applied A's edit; A retries. | Equal clock: no-op; acknowledged again. |
| The web marks completed while Android is playing the episode. | Completion true; Android's later position saves change only the position. |

### 4.2 Playback start position, auto-completion and uncompletion

- **Auto-completion** happens only on the player's natural end-of-media event
  during playback (Android `ProcessingState.completed`, HTML `ended`). Seek,
  pause, stop, errors and loading a saved end position never complete. The
  end edit sets completion `(true, auto, t)` and position
  `(duration or last position, t)` with the same clock.
- **Duration** in these rules is the duration the player reports for the
  loaded media; the feed's `<itunes:duration>` is only a fallback before the
  media is loaded, because feed durations are often wrong.
- **Start position** for every play action except "Play from beginning": the
  reconciled position, unless the duration is known and the position is
  within 5 seconds of the end (`position >= duration - 5`), in which case
  playback starts at 0. "Play from beginning" always starts at 0. Starting
  writes nothing; the first periodic save writes a new position edit.
- **Manual uncompletion at an end position** writes only completion
  `(false, manual, t)`. The episode reappears in the Uncompleted view with
  its stored end position; playing it starts at 0 by the rule above, so it is
  not immediately completed again. Its progress indicator shows the stored
  position until the first save.
- Replaying a completed episode does not uncomplete it; only a manual action
  does.

## 5. Data model

### 5.1 Server tables (new)

All clocks are `INTEGER` milliseconds, so none of these columns belongs in
`timeColumns` (`repository/time_utc.go`). Every table referencing `users`
must use `ON DELETE CASCADE` and be listed in `userOwned`
(`repository/orphans.go`); none references `media` or `podcast_episodes`, so
deleting a server copy or a catalog row can never delete account state.

```sql
CREATE TABLE podcast_subscriptions (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_key TEXT NOT NULL,
  subscribed INTEGER NOT NULL,
  subscription_ms INTEGER NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  fetch_url TEXT NOT NULL DEFAULT '',
  urls TEXT NOT NULL DEFAULT '[]',          -- JSON array, at most 10 URLs
  change_seq INTEGER NOT NULL,
  PRIMARY KEY (user_id, feed_key));

CREATE TABLE podcast_episode_state (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_key TEXT NOT NULL,
  episode_key TEXT NOT NULL,
  position_seconds REAL NOT NULL DEFAULT 0,
  position_ms INTEGER NOT NULL DEFAULT 0,   -- 0 = never set
  completed INTEGER NOT NULL DEFAULT 0,
  completion_source TEXT NOT NULL DEFAULT '',
  completion_ms INTEGER NOT NULL DEFAULT 0, -- 0 = never set
  enclosure_url TEXT NOT NULL DEFAULT '',   -- latest evidence, for alias resolution
  change_seq INTEGER NOT NULL,
  PRIMARY KEY (user_id, feed_key, episode_key));
CREATE INDEX idx_podcast_sub_seq ON podcast_subscriptions(user_id, change_seq);
CREATE INDEX idx_podcast_state_seq ON podcast_episode_state(user_id, change_seq);

CREATE TABLE podcast_feed_aliases (
  alias_key TEXT PRIMARY KEY, feed_key TEXT NOT NULL, change_seq INTEGER NOT NULL);
CREATE TABLE podcast_episode_aliases (
  feed_key TEXT NOT NULL, alias_key TEXT NOT NULL, episode_key TEXT NOT NULL,
  change_seq INTEGER NOT NULL, PRIMARY KEY (feed_key, alias_key));

-- One row; next change sequence number. Every row written (subscription,
-- episode state, feed alias, episode alias) takes its own number, inside the
-- write transaction: a transaction writing n rows advances next_seq by n.
-- Numbers are unique, so paging by "change_seq > since" never splits or
-- skips a number, and with SQLite's single writer commit order = seq order.
CREATE TABLE podcast_sync_seq (id INTEGER PRIMARY KEY CHECK (id = 1), next_seq INTEGER NOT NULL);
```

Catalog changes: `podcast_feeds` gains `feed_key TEXT` (unique among rows
with `merged_into IS NULL`), `merged_into INTEGER`, `created_by INTEGER`
(nullable; the user whose subscription created the row) and keeps
`feed_url` as the fetch URL. `podcast_episodes` is rebuilt (§9.1) with
`guid` nullable, `episode_key TEXT NOT NULL`, `enclosure_key TEXT NOT NULL`
(latest enclosure, §3.2), `UNIQUE(feed_id, episode_key)` instead of
`UNIQUE(feed_id, guid)`, and the same ids. The sync
epoch is a random 128-bit value stored in `server_secrets` under
`podcast_sync_epoch`.

### 5.2 Android tables (new, in their own database)

Scope `local` is standalone use; an account scope is
`account:<origin>#<user_id>`, the same pair that keys `sync_items` today.

| Table | Key | Content |
| --- | --- | --- |
| `podcast_scopes` | `scope` | `epoch`, `cursor`, `last_full_success_ms`, `last_attempt_ms`, `last_error_kind`, `associated_from_local` |
| `podcast_subscriptions` | `(scope, feed_key)` | register as on the server, plus `pending_edit_id` (null = synced) |
| `podcast_episode_state` | `(scope, feed_key, episode_key)` | both registers, each with its own `pending_edit_id`, plus `enclosure_url` |
| `podcast_feeds`, `podcast_episodes` | `feed_key`, `(feed_key, episode_key)` | device catalog fetched from the publisher: fetch URL, ETag/Last-Modified, metadata, aliases |
| `podcast_downloads` | `(feed_key, episode_key)` | device file name (generated, never derived from remote paths), size, state; shared by every scope on the device |
| `podcast_sync_rejections` | auto id | rejected edits kept for diagnosis (§6.2) |

Every local edit writes the register and its `pending_edit_id` in one
transaction. Deleting a download never touches state or subscriptions;
unsubscribing never deletes a download without a separate explicit action.

## 6. Synchronization API

### 6.1 Capability

`GET /api/v1/config` adds `"podcast_sync": 1`. A client that does not find it
(older server) runs podcasts standalone for that account, keeps media sync
working, and shows no podcast sync error.

### 6.2 `POST /api/v1/podcasts/sync` (also `/api/podcasts/sync`)

Registered through `handleBoth` with `requireSession` (cookie or Bearer).
Every user may call it; it reads and writes only the caller's rows. Body
limit 2 MiB; at most 500 register edits per request, where a subscription
item is one edit and an episode item one or two (`position`, `completion`).

```json
{
  "epoch": "4f1c…",
  "since": 1200,
  "limit": 500,
  "subscriptions": [
    {"edit_id": "q7G…", "feed_key": "feed:example.com/rss",
     "urls": ["https://example.com/rss"], "fetch_url": "https://example.com/rss",
     "title": "Example", "subscribed": true, "observed_ms": 1792065600000}
  ],
  "episodes": [
    {"feed_key": "feed:example.com/rss", "episode_key": "guid:ep-42",
     "enclosure_url": "https://cdn.example.com/42.mp3",
     "position": {"edit_id": "a1…", "seconds": 812.4, "observed_ms": 1792065601000},
     "completion": {"edit_id": "b2…", "completed": true, "source": "auto",
                    "observed_ms": 1792065601000}}
  ]
}
```

Rules:

- `edit_id`: 16–64 bytes of `[A-Za-z0-9_-]`, random per local edit (≥ 128
  bits), reused for every retry of that edit, replaced by a new id when the
  register is edited again.
- `position` and `completion` are optional per episode item; at least one is
  required. `seconds` must be finite and `0 ≤ seconds ≤ 9e12` (the bound
  `SyncPlayback` uses). `observed_ms` must be in `1 … 253402300799999`.
- Keys must have the shapes of §3 (`feed:`, `guid:`, `enc:`, or a
  server-minted `legacy-feed:` / `legacy-episode:` key that exists) and at
  most 2100 bytes; `urls` at most 10, each canonicalizable; `title` at most
  500 characters.
- Per-user quotas: 5 000 subscription rows (tombstones included), 200 000
  episode state rows. An edit beyond them is rejected with `quota`.
- The server applies all valid edits in one transaction (identity
  resolution, register merge, mirror update §7.1, change sequence numbers),
  then reads the changes and commits.

Response `200`:

```json
{
  "epoch": "4f1c…",
  "acked": ["q7G…", "a1…", "b2…"],
  "rejected": [{"edit_id": "…", "reason": "invalid_position"}],
  "clamped": ["…"],
  "resolved": [{"kind": "episode", "feed_key": "feed:example.com/rss",
                "from": "enc:cdn.example.com/42.mp3", "to": "guid:ep-42"}],
  "subscriptions": [{"feed_key": "…", "subscribed": true, "subscription_ms": 1792065600000,
                     "title": "…", "fetch_url": "…", "urls": ["…"]}],
  "episodes": [{"feed_key": "…", "episode_key": "…",
                "position": {"seconds": 812.4, "observed_ms": 1792065601000},
                "completion": {"completed": true, "source": "auto", "observed_ms": 1792065601000}}],
  "aliases": [{"kind": "feed", "from": "feed:old.example.com/rss", "to": "feed:example.com/rss"}],
  "next_since": 1290,
  "has_more": false
}
```

- `resolved` and pulled `aliases` are applied by the client rewrite rule of
  §3.3 (per-register merge on collision).
- `acked` lists every accepted edit, whether it changed the register or was
  superseded. `subscriptions` and `episodes` always contain the current rows
  of every key edited in the request, besides the pulled rows. A client
  clears a register's `pending_edit_id` only if it still equals an acked id,
  and then replaces the local register with the returned one, clock included
  (this also repairs a clamped clock); an edit made while the request was in
  flight has a new id, stays pending and keeps its local value. Registers
  without a pending edit adopt pulled values whose clock is `>=` their own
  (§4 rule 2).
- `rejected` reasons: `invalid_key`, `invalid_url`, `invalid_position`,
  `invalid_time`, `invalid_edit`, `quota`. Rejected edits can never succeed:
  the client removes them from the queue, records them in
  `podcast_sync_rejections`, and the run counts as partial (§8.3).
- Pull: subscription and episode rows of the caller with `change_seq > since`,
  plus alias rows with `change_seq > since` whose target the caller has a
  subscription or state row for (no other user's feeds leak), ordered by
  `change_seq`, at most `limit` (default 500, max 1000) rows in total.
  Missing registers (clock 0) are omitted. Every subscription row returned
  carries all aliases of its feed, and every episode row all aliases of its
  episode, whatever their sequence numbers (bounded by the 16-alias caps), so
  a client that subscribes after the aliases were written still learns them.
  Each row has its own sequence number (§5.1); `next_since` is the last
  returned one and `has_more` says whether more rows exist. The client
  persists `next_since` after applying each page, so an interrupted pull
  resumes where it stopped. A pull request may carry no edits.
- `epoch` mismatch (database restored or replaced) or `since` greater than
  the server's highest sequence: the server ignores `since`, answers from 0
  and returns its epoch. The client keeps its pending edits, sets its cursor
  to 0 and marks every non-pending local register *unconfirmed*. Each pulled
  register confirms its local counterpart (adopted when the server clock is
  `>=` the local one). When the pull reaches `has_more = false`, every
  register still unconfirmed or with a local clock greater than the server's
  gets a new `edit_id` with its original clock and value and is pushed, so
  state the restored server lost is restored from the clients instead of
  diverging. Only then is the new epoch stored.
- `"scope": {"feed_key": "…", "episode_key": "…"}` instead of `since`
  returns only that episode's current registers (after applying the edits)
  and no cursor fields. It is the pre-playback request (§8.1).

Status codes: `200` (also with item rejections); `400` malformed envelope,
more than 500 edits, invalid `limit`; `401`; `413` body too large (the client
halves its page and retries); `500`. Clients retry network errors and `5xx`
with exponential backoff (15 s doubling to 15 min, reset by connectivity
changes, app foreground, a new edit, or Sync now); `401` stops sync for that
account until sign-in.

### 6.3 Web listing endpoints

Subscription and episode views (`0i3`) read the caller's subscriptions joined
with the catalog through `feed_key` and aliases. Each episode in a listing
carries `feed_key`, `episode_key`, `completed`, `completion_source`,
`position_seconds`, and `server_copy` (`{"media_id": n}` only when a
non-deleted media row is linked and the caller may read the podcast set,
else `null`). Filters: `uncompleted` (default; includes started),
`completed`, `all`; order newest first by `published_at`, then
`episode_key` for a stable page order. Exact routes are defined by `0i3` and
documented in `api.md`. `server_copy` exists only in web listings; the sync
API never returns server media ids.

## 7. Relation to existing synchronization and server data

### 7.1 One history for server-downloaded episodes

Per-account episode state is the only history of a podcast episode. For a
media row linked to an episode (`podcast_episodes.media_id`), the existing
media endpoints become views onto it:

| Existing call on episode-linked media | Effect on episode state |
| --- | --- |
| `POST /progress`, `/progress/batch` (position p at t) | position `(p, t)`; play-count accumulation unchanged |
| `POST /progress/status` `finished` (at t or `observed_at`) | completion `(true, manual, t)`; position unchanged (§4 rule 5). The route cannot tell the web's manual **Mark as finished** (`markAsFinished`, `web/js/app.js`) from a cast end (`web/js/cast.js`), so it is treated as manual; the cast's own position saves already hold the end position. |
| `POST /progress/status` `not_started` | position `(0, t)` and completion `(false, manual, t)` |
| `POST /media/{id}/playback-sync` progress `{p, finished:false, t}` | position `(p, t)` only |
| … `{p, finished:true, t}` | completion `(true, auto, t)` and position `(p, t)` |
| `POST /podcasts/episodes/{id}/complete` (legacy toggle) | completion `(!current, manual, server time)` |

LWW decisions for these calls use the episode registers, not
`playback_progress`. After every change of an episode's state, the server
rewrites the `playback_progress` row of each linked media item in the same
transaction, as a projection in the legacy single-register form:

- `position_seconds` = position;
- `finished` = `completed && completion_ms >= position_ms`, i.e. the legacy
  rule that the latest edit decides: a completed episode that is being
  replayed (position saved after the completion) projects as unfinished, so
  old Android clients resume it at the saved position instead of 0
  (`readPosition` in `offline_download_repository.dart` returns 0 for
  finished copies) and the web **In progress** list (`ListInProgressMedia`,
  `finished = 0`) keeps showing it;
- `updated_at` = the later of both clocks;
- `accumulated_seconds` kept: the mirror is written by its own statement,
  not by `upsertProgress`, `FinishProgress` or `ResetProgressAt`, which would
  reset accumulation or play-count state.

`playback-sync` responses project the same way: `{position, finished as
above, observed_ms = max}`. A new server download seeds the mirror from
episode state. Deleting the media row removes only the mirror. Bookmarks,
notes, favorites and shares stay media-scoped (podcast bookmarks are not part
of this contract).

The merge semantics (edit-time clocks, ties keep the server value, durable
pending edits per account, tombstones, account generation guard) are those of
`SyncPlayback` and `OfflineDownloadRepository`; the extensions are identity
keys instead of media ids, separate position/completion registers, explicit
`edit_id` acknowledgements, and a change cursor for many items.

### 7.2 Account association (standalone → account)

- Android starts in scope `local`. Signing in does not merge anything by
  itself. The first time podcast sync is available for an account
  (`podcast_sync` capability) and scope `local` has subscriptions or state,
  the app asks once: **Sync this device's podcasts with <user>@<server>?**
  with **Sync** and **Keep separate**.
- **Sync** merges every `local` register into the account scope in one
  transaction, register by register by clock, and marks
  `associated_from_local`. When the account register is missing or has a
  smaller clock, the local value and its original clock (no re-stamping) are
  written with a new pending `edit_id`, replacing the account register's
  pending edit, which would lose on the server anyway. When the account
  register's clock is greater or equal, it stays as it is, including its
  pending edit. Keys are first resolved through the account scope's known
  aliases. The result then merges with the server by §4. Scope `local` is
  kept unchanged as the standalone library.
- **Keep separate** (or no answer) leaves both scopes apart; the question
  can be repeated from settings.
- The active library is the signed-in account's scope while an account is
  selected (including signed out of that same account: pending edits stay
  and wait), otherwise `local`. Switching accounts switches scope; pending
  edits, cursor, epoch and last-success data never cross scopes.

### 7.3 Catalog creation and fetching on the server

- A subscription edit with `subscribed = true` for a key that resolves to no
  catalog feed creates a catalog row (`created_by` = caller, `auto_download
  = 0`) in the shared podcast set, without creating folders, granting set
  permissions or fetching inside the request. The feed checker fetches it
  asynchronously.
- The checker refreshes a catalog feed only while at least one user has an
  active subscription to it (legacy admin rows count through their migrated
  subscriptions). Refresh may fetch cover art; it never downloads audio.
  Synchronization never triggers `POST /podcasts/episodes/{id}/download`.
- Catalog rows created by non-admins are fetched only through an outbound
  policy: `http`/`https`, resolved addresses must be public unicast (no
  loopback, private, link-local, unique-local, multicast, or unspecified
  ranges), checked for every connection and redirect hop; at most 20 new
  catalog rows per user per hour. Admin-created rows keep today's behavior.
- Catalog metadata of a feed is visible to its subscribers; server copies
  stay governed by the podcast set's permissions (`checkSetPermission`).

### 7.4 Unsubscribe, file cleanup and catalog removal

| Action | Effect |
| --- | --- |
| Unsubscribe (web, Android) | Subscription tombstone. No files, media rows, catalog rows or episode state are touched. |
| Delete Android download | That device's file only. |
| Delete server copy (`3i3`) | The media row through the existing soft-delete/trash flow and its authorization (owner or admin); state survives; the UI must state that other server listeners lose that copy and its media-scoped notes, bookmarks and shares once the trash expires. |
| `DELETE /api/podcasts/{id}` (admin, legacy) | Deletes the feed's server copies and tidies folders as today, but no longer deletes subscriptions or episode state (they are not foreign keys). The catalog row is deleted only when no active subscription remains; otherwise it stays without copies. The web UI must label it **Remove from server** in the admin area and stop using it for Unsubscribe. |

## 8. Client behavior

### 8.1 Before playback

Before an episode starts, the client sends the scoped request (§6.2) with the
episode's pending edits and waits at most **3 seconds** (connection
included). It then starts from the reconciled registers (§4.2), or from local
state if sync is unavailable, unauthenticated or slow. A response that
arrives after playback started is merged into storage only: it never seeks
the active player and never replaces a register whose local clock is newer or
whose edit is still pending. (The existing media `syncKey` waits 5 s; `zh3`
should align both to one constant.)

### 8.2 At episode end

Write the end edits locally first (one transaction), advance to the next
episode immediately, and start a push without awaiting it.

### 8.3 Sync now, success and feedback state

**Sync now** and the background coordinator run, per active account: the
podcast push/pull until `has_more = false`; all existing `sync_items`
(media `playback-sync`); and a `ProgressQueue` flush. Pushes are paged by 500
register edits.

A media `playback-sync` item answered with `403` or `404` (its server copy
was deleted, or set access was revoked) is a permanent rejection, like a
podcast `rejected` edit: the item is marked `gone` with the time, recorded,
excluded from the success criterion and from automatic retries, and retried
only by an explicit **Sync now** (a restored copy then syncs again). Its
local position stays on the device; edits it already delivered are part of
the episode state and stay synchronized for every other client.

- **Full success**: every step completed, every edit pending at the start of
  the run acked (edits made during the run may stay pending), the pull
  reached `has_more = false`, and nothing new was rejected. Only then does
  `last_full_success_ms` advance and stale/unavailable feedback clear.
- **Partial**: anything less (a failed step, a rejected edit, an interrupted
  pull). Acked edits are cleared, others stay pending, the cursor keeps the
  last applied page, and `last_full_success_ms` stays unchanged.
- `last_error_kind` is one of `offline`, `auth`, `server`, `rejected`,
  `unsupported`; feedback wording must follow the kind (an auth failure is
  not a server outage).
- Stale: no full success for 48 hours shows **Last synced <age>**; a
  configured account that never succeeded shows a never-synced state. Scope
  `local` never shows sync feedback.

The web UI writes through the same endpoint, keeps failed edits (with their
`edit_id`s) in memory, retries them and shows the pending hint; it does not
persist them across reloads (the web UI requires the server).

## 9. Migration

### 9.1 Server

One idempotent migration, run by `initializeSchema` before
`removeOrphanRows`. Steps 1 and 2 each run in their own transaction and are
gated on the absence of the column they add. Steps 3–7 run in one single
transaction that creates `podcast_subscriptions` (gated on its absence, like
`migratePlaybackProgress`), so a crash leaves either none or all of the
backfills and a restart cannot repeat them.

1. Add `feed_key`, `merged_into`, `created_by` to `podcast_feeds`. Backfill
   `feed_key = FeedKey(feed_url)`; an invalid URL gets `legacy-feed:<id>`.
   Rows sharing a key: the smallest id keeps it, the others get `merged_into`.
2. Rebuild `podcast_episodes` with the SQLite twelve-step procedure on one
   dedicated connection with `PRAGMA foreign_keys = OFF` (the DSN normally
   forces it on, see `repository.connectionDSN`), keeping ids so
   `media_id` links and `podcast_status` rows stay valid; finish with
   `PRAGMA foreign_key_check` and abort on any violation. Each feed's stored
   rows, in id order, are treated as one document (`EpisodeKeys` on `guid`
   and `episode_url`); a row left without a key (unusable URL, duplicate)
   gets `legacy-episode:<id>`. `enclosure_key` is the row's enclosure key, or
   empty. Empty GUIDs become `NULL`. Legacy keys are retired later by §3.1.
3. Create the tables of §5.1, the sync epoch and the sequence row.
4. Subscriptions: for every catalog feed not `merged_into`, one row per user
   who can list it today (admins, and users with any `set_permissions` row on
   its set): `subscribed = 1`, `subscription_ms` = the feed's `created_at`.
5. Episode state from `podcast_status`: completion `(is_completed, manual,
   updated_at)`; position only when `position_seconds > 0`.
6. Episode state from `playback_progress` of linked media: position
   `(position_seconds, updated_at)` and, if `finished`, completion
   `(true, auto, updated_at)`, each merged by §4; then `playback_resets` of
   linked media as position `(0, reset)` and completion `(false, manual,
   reset)` where newer.
7. Rewrite the `playback_progress` mirror (§7.1) of every linked media item
   from the migrated episode state, so legacy readers see the projection from
   the first request on.
8. Keep `podcast_status` read-only for one release (rollback aid); new code
   neither reads nor writes it. List it for removal in the following release.

Guarantees: no file is created, moved or deleted; no media, share, note,
favorite, bookmark, permission or user row changes (only the
`playback_progress` mirrors of linked media are rewritten, step 7); no audio is downloaded;
set permissions are not granted to anyone. A rollback to the previous release
keeps working but does not see state written after the upgrade.

### 9.2 Android

The Android client stores no podcast state of its own today: it lists and
toggles through the server (`listPodcasts`, `listEpisodes`,
`toggleEpisodeComplete`, `downloadEpisode` in `dio_player_api_client.dart`).
The new database starts empty in scope `local`; account scopes are filled by
the first pull. Existing offline copies of episode media stay ordinary media
downloads and keep syncing through `playback-sync`, which §7.1 routes into
episode state; they are not converted into `podcast_downloads`.

## 10. Compatibility

| Case | Behavior |
| --- | --- |
| Old Android or cached web → new server | Legacy podcast routes keep their shapes (rows below). Media progress calls on episode media go through §7.1 and old clients read the projection. |
| `POST /api/podcasts` (admin, legacy subscribe) | Creates or reuses the catalog row as today (including the existing owner grant of `ensurePodcastSet` to that admin) and also writes the caller's subscription register `subscribed = true` at server time, so the feed has an active subscriber and keeps refreshing (§7.3). |
| `GET /api/podcasts` | Unchanged meaning: catalog feeds (not `merged_into`) whose set the caller may read, as today, including feeds the caller has unsubscribed from. New fields: `feed_key` and `subscribed` (the caller's register; `false` for a tombstone or no row). It is the catalog/admin view, not the subscription view. |
| `GET /api/podcasts/{setId}/episodes` | Unchanged meaning: episodes of every catalog feed in the set, as today. `is_completed` and `position_seconds` come from the caller's episode state; new fields `feed_key` and `episode_key`. |
| `POST /api/podcasts/episodes/{id}/complete` (toggle) | A manual completion edit at server time (§7.1). It stays non-idempotent, so new clients must not use it. |
| `DELETE /api/podcasts/{id}` | As in §7.4. |
| New Android → old server | No `podcast_sync` capability: podcasts stay standalone for that account, media sync continues, no podcast error. |
| New Android ↔ new server | Full contract. Android never calls the episode download route and never needs a server media id. |
| Two Android clients on one account | Converge through the server by §4; downloads stay per device. |
| One device, two accounts | Separate scopes, cursors, pending edits and last-success values; only downloads are shared on the device. |

## 11. Verification required from the delivery tasks

Use fixture feeds served from a local test server and throwaway libraries.
Automated tests are necessary but do not replace the platform checks of `5i3`.

- Identity (`vh3`, `xh3`): both implementations pass the shared vectors,
  including the ingest scenarios; redirect (301/308 vs 302/307),
  `new-feed-url`, a redirect into another subscribed feed (target wins),
  GUID appearing, changing and disappearing, and negative cases. Required
  ingest cases: (a) a repeated GUID whose items are reordered between
  fetches keeps each item's state on its own episode; (b) two distinct GUIDs
  sharing one enclosure stay two episodes and never produce a `resolved`
  mapping between them; rotating signed enclosures add no aliases; client
  evidence never creates an alias or merge.
- Registers (`vh3`, `wh3`): ties, retries, delayed offline edits, clamped
  future clocks, tombstones resisting resurrection, independent position and
  completion, uncompletion at an end position, account isolation (another
  user's keys, aliases and quotas are invisible).
- API (`vh3`): validation and quota rejections per item, `legacy-*` keys
  accepted only when they exist, `413`, `401`, paging and resume with pages
  cut inside a multi-row transaction (no row skipped), a late subscriber
  receiving aliases written before its cursor, epoch reset (client registers
  newer than the restored server are re-pushed), scoped request, routing
  precedence of `/podcasts/sync` next to `/podcasts/{id}`; legacy subscribe
  writes a subscription and the feed refreshes.
- Migration (`vh3`, `wh3`): a database built with the old schema (shared
  feeds, admin and viewer permissions, GUID-less episodes, toggled status,
  media progress, resets, shares, notes, bookmarks) migrates with every
  non-podcast row unchanged, ids kept, no files touched, and runs twice
  without change.
- Bridge (`wh3`): every row of the §7.1 table; the projection of a replayed
  completed episode (`finished = 0`, listed in **In progress**, resumed by an
  old Android client at its position); `accumulated_seconds` preserved by
  mirror writes; server copy deletion keeping state.
- Android (`zh3`, `2i3`): pending edits survive process death; account switch
  mid-request (generation guard); standalone association and key rewrites
  that collide with newer pending account edits (the newer clock wins);
  epoch change; 3 s pre-play bound; late response without a seek; end sync
  not delaying the next episode; partial vs full success, a `playback-sync`
  `404` not blocking full success, and the 48-hour clock.

## 12. Decisions on former open questions

Confirmed by the project owner on 2026-10-20:

1. `POST /podcasts/episodes/{id}/download` stays available to set viewers
   (`checkSetPermission` with any role, as today), even though it writes
   shared server files.
2. Non-admin subscriptions create server catalog rows, with the outbound
   policy of §7.3 (public addresses only, rate limit per user per hour).
3. Tombstones are kept forever, bounded only by the per-user quota; there is
   no retention period, so long-offline clients never need a forced full
   resync because of tombstone expiry.
