---
id: S16
title: "Media list pagination, filtering and fail-open query parsing"
tags: [media, api, pagination, filtering, search, sort]
preconditions:
  server_state: running        # server running with an existing admin account and several media items
  fixtures: []
assertions:
  - status_code: "GET /api/v1/media 200"
  - db: "SELECT count(*) FROM media WHERE deleted_at IS NULL"
skip: false
---

# Purpose

This scenario exercises `GET /api/v1/media` query-parameter handling end-to-end:
pagination (`limit` / `offset`), search and type filters, the favorites flag,
the per-set filter, and the supported `sort` keys (`name` is the default;
`duration`, `play_count`, `date` and `random` are explicit). It also locks in
the fail-open behaviour of `parseMediaListQuery` (`internal/api/handlers_media.go`):

- A `limit` that is `<= 0`, `> 1000`, or not a valid integer is **silently
  ignored** — the server falls back to the default limit of 100.
- An `offset` that is `< 0` or not a valid integer is **silently ignored** —
  the server falls back to offset 0.

The handler does NOT return HTTP 400 for any of these inputs. If a future
change makes the handler reject bad query parameters with 400, several steps
below will need to be updated.

The `MEDIA_ROOT` must point at a directory with at least 5 media items, at
least one of which is type `audio`, so the pagination, search and type
assertions can find real data. (`./testdata/media` satisfies this — see the harness
README.)

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Discover the total active media count: call `GET /api/v1/media` with the
   session cookie and no other query parameters. Confirm the response is HTTP
   200 and the returned JSON is an array. Save the array length as
   `total_count`. If `total_count` is less than 4, abort the scenario with a
   clear failure message — the rest of the pagination assertions require at
   least 4 items.

3. Page 1 of size 2: call `GET /api/v1/media?limit=2` with the session cookie.
   Confirm the response is HTTP 200 and the returned array has exactly 2
   entries. Save the two `id` values as `page1_ids` (in the order returned).

4. Page 2 of size 2: call `GET /api/v1/media?limit=2&offset=2` with the
   session cookie. Confirm the response is HTTP 200 and the returned array
   has exactly 2 entries. Save the two `id` values as `page2_ids`. Confirm
   that the sets `page1_ids` and `page2_ids` are disjoint — no id from page 1
   appears in page 2. (This proves `offset` actually skips the first page
   rather than being silently ignored.)

5. Oversized limit must clamp to the default 100, not 1000000: call
   `GET /api/v1/media?limit=1000000` with the session cookie. Confirm the
   response is HTTP 200. The returned array length must be bounded — it must
   be less than or equal to `total_count`, and it must be less than or equal
   to 100 (the default that `parseMediaListQuery` falls back to when the input
   exceeds the 1000 cap). This locks in the fail-open behaviour: the handler
   does NOT return 400 for a wildly oversized limit.

6. Negative limit must fail open to default 100: call
   `GET /api/v1/media?limit=-5` with the session cookie. Confirm the response
   is HTTP 200 (NOT 400). The returned array length must be less than or
   equal to `min(total_count, 100)`. The server silently ignored the negative
   value and used the default limit.

7. Non-numeric limit must fail open to default 100: call
   `GET /api/v1/media?limit=foo` with the session cookie. Confirm the response
   is HTTP 200 (NOT 400). The returned array length must be less than or
   equal to `min(total_count, 100)`. The server silently ignored the
   un-parseable value and used the default limit.

8. Negative offset must fail open to 0: call `GET /api/v1/media?offset=-1`
   with the session cookie. Confirm the response is HTTP 200 (NOT 400). The
   returned array length must equal `total_count` (because offset 0 with the
   default limit of 100 returns the same view as step 2, assuming
   `total_count` is at most 100).

9. Search filter: pick a known substring from one of the filenames returned
   in step 2 (for example, the first 4 characters of the `file_name` field of
   `page1_ids[0]` — strip any leading dots and use only the alphanumeric
   prefix). Call `GET /api/v1/media?search={substring}` with the session
   cookie. Confirm the response is HTTP 200 and that every returned item's
   `file_name` OR `rel_path` field contains the substring (case-insensitive
   match is acceptable — the server uses a `LIKE %substring%` query). The
   array must contain at least one entry (the media item the substring was
   derived from).

10. Type filter — audio only: call `GET /api/v1/media?type=audio` with the
    session cookie. Confirm the response is HTTP 200. If the array is
    non-empty, confirm every returned item has `type` equal to `audio`. If
    the array is empty (the seeded `testdata/media/` library has no audio files),
    treat it as acceptable — the type filter still returned 200 with a
    well-formed empty array.

11. Favorites filter (round-trip): call `GET /api/v1/media?favorites=true`
    with the session cookie. Confirm the response is HTTP 200 and save the
    array length as `fav_count_before`. Pick a media item to favorite — use
    `page1_ids[0]` as `fav_media_id`. Call
    `POST /api/v1/media/{fav_media_id}/favorite` with the session cookie and
    confirm the response is HTTP 200 with body `{"favorite": true}`. Call
    `GET /api/v1/media?favorites=true` again. Confirm the response is HTTP
    200, the array length is `fav_count_before + 1`, and the array contains
    an entry whose `id` equals `fav_media_id`. Then clean up: call
    `POST /api/v1/media/{fav_media_id}/favorite` once more and confirm the
    response is HTTP 200 with body `{"favorite": false}` (un-favorited).
    Re-query `GET /api/v1/media?favorites=true` and confirm the array length
    is back to `fav_count_before`.

12. Per-set filter: call `GET /api/v1/sets` with the session cookie. Confirm
    the response is HTTP 200 and save the `id` of the first set as `set_id`.
    Call `GET /api/v1/media?set_id={set_id}` with the session cookie. Confirm
    the response is HTTP 200 and that every returned item has `set_id` equal
    to `set_id`. The array must contain at least one entry (the seeded
    library always has at least one media item in the first set).

13. Sort by duration: call `GET /api/v1/media?sort=duration` with the session
    cookie. Confirm the response is HTTP 200. If the array has 2 or more
    entries, confirm the `duration` field is non-decreasing across consecutive
    entries (the repository uses `ORDER BY media.duration` ascending — see
    `internal/repository/media.go`). Entries with a null/zero duration sort
    first; that is acceptable.

14. Sort by date: call `GET /api/v1/media?sort=date` with the session cookie.
    Confirm the response is HTTP 200. If the array has 2 or more entries,
    confirm the `created_at` field is non-increasing across consecutive
    entries (the repository uses `ORDER BY media.created_at DESC`).

15. Sort default (name): call `GET /api/v1/media?sort=name` with the session
    cookie (the handler doesn't special-case `name` — anything other than
    `duration`, `play_count`, `date` or `random` falls through to the
    default `ORDER BY media.file_name`). Confirm the response is HTTP 200.
    If the array has 2 or more entries, confirm the `file_name` field is
    non-decreasing across consecutive entries (lexicographic ASCII order).
