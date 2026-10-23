# Music A06a — preserve music identity across moves and retags (`tk3`)

## Scope

Stable **song identity** (`media.id`) across filesystem moves, renames,
extension changes, and retags so playlists, favorites, play counts,
bookmarks/resume, notes, shares and offline downloads keep working without
user action. Document identity boundaries. Resolve ambiguous duplicates
deliberately; rare conflicts are visible and recoverable in Admin.

Does **not** implement A06b custom tag mapping, A06c long-term missing-file
retention UI, or A06d scheduled scans. Does **not** break A05
`music_libraries` / `sets.library_id` / `PathRoots`.

## Identity boundaries

| Thing | Stable key | What may change |
| --- | --- | --- |
| Song / file | `media.id` (integer) | Path, set, tags, size, codec |
| Album | `album_key` from release MBID or normalized album artist+title | Retag of album/artist/MBID recomputes the key (songs keep their `media.id`) |
| Artist | `artist_key` from name (MBID later) | Same as album |
| Playlist entry | `entry_id` + `media_id` | Survives when `media_id` is preserved |
| Library | `library_id` | Moves are matched **only within the same library**; cross-library copies are new songs |

Podcast sets (`is_podcast`) and non-audio media are excluded from music
identity matching (path-keyed soft-delete/create as before).

## Matching policy (automatic)

On each admin Rescan, after every library’s sets are scanned (orphans
soft-deleted, new paths inserted):

1. Collect audio rows **soft-deleted in this scan** and audio rows
   **created in this scan**, grouped by `library_id`.
2. Score candidate pairs (missing → new) using the strongest applicable
   signal:

| Rank | Signal | Typical case |
| --- | --- | --- |
| Exact | Non-empty MusicBrainz **recording** id equal | Picard-tagged library |
| Exact | Same file size + duration (±0.5s) + basename stem | Rename / move / same encode |
| Strong | Same tag fingerprint (album artist, album, disc, track, title) + size | Retag-preserving size, path changed |
| Stem | Same relative path without extension + (size or duration) | `.flac` → `.mp3` in place |
| 1∶1 | Exactly one missing and one new share the same non-empty tag fingerprint | Single obvious move |

3. Greedy assign highest unique scores. Each missing and each new is used
   at most once.
4. **Ambiguous** (two or more candidates at the best score for one side):
   do **not** merge; record a conflict for Admin. Both rows remain
   (missing soft-deleted, new as a new `media.id`).
5. On a clear match: keep the **old** `media.id` and user state; update
   path/set/tags/thumbnail from the new file; hard-delete the temporary
   new row (no user state yet). Clear soft-delete.

Same-path retags already refresh tags via `UpdateMediaMusic` and never
change `media.id`.

**Same-scan move + heavy retag:** prefer Recording MBID or unchanged
size/duration/basename. If tags and size both change in one scan with no
MBID, matching may fail — scan after move, then retag (or the reverse).

## Conflict UX (rare)

Normal moves need no UI. When conflicts exist:

| Surface | Behavior |
| --- | --- |
| Rescan status | “N songs moved · M identity conflicts” (polite live region) |
| Admin › Music identity | List: previous path, candidate new paths, reason |
| Keep separate | Dismiss conflict; leave both rows (default safe outcome) |
| Merge into… | Admin picks the surviving `media.id` (usually the older); other hard-deleted after path transfer |
| Empty | “No identity conflicts. Moved songs keep playlists and history automatically.” |
| Keyboard | Existing admin focus; native buttons; no new global shortcuts |
| Android | No admin CRUD; shared result is preserved server-side (playlists/history). Web Admin is the recovery surface. |

Selection in Music lists/queues stays on the same `media.id` after an
automatic move; clients that still hold a stale path refresh on next
catalog fetch.

## Data

```
music_identity_conflicts(
  id, library_id, missing_media_id, candidate_media_ids JSON,
  missing_path, reason, created_at, resolved_at, resolution
)
```

Recording MBID is stored in `media.music` JSON (`recording_mbid`) when tags
provide it (`musicbrainz_trackid` / recording id aliases).

## Verification (implementation slice)

Targeted Go tests: rename/move same set, extension change, retag same path,
1∶1 fingerprint, ambiguous conflict (no auto-merge), cross-library non-match,
playlist/favorite/progress preserved after move.

**Deferred (honest gaps):** Chrome / Firefox / emulator E2E move/retag
fixture journeys until integrated verification.
