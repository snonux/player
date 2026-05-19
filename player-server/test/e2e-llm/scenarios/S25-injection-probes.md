---
id: S25
title: "Injection probes — SQL, XSS, path-traversal against user-input params"
tags: [security, injection, sql, xss, path-traversal, negative-path]
preconditions:
  server_state: running         # server running with at least one admin and some media
  fixtures: []
assertions:
  - status_code: "GET /api/v1/media 200"
  - db: "SELECT count(*) FROM users"          # users table still exists after DROP TABLE probe
  - db: "SELECT count(*) FROM media"          # media table still exists after DROP TABLE probe
skip: false
---

# Purpose

This is a **defensive** security scenario. It exercises every user-controlled
query parameter on `GET /api/v1/media` and the writable note/tag bodies with
classic injection payloads, plus a path-traversal attempt against the public
share-token route. The intent is to lock in the existing safe behaviour and
catch any regression that would let user input reach the SQL engine raw or
break the router.

Source-code reference (the assertions in this scenario only hold while these
properties are true; if they change, this scenario must be updated):

- `internal/repository/media.go::ListMedia` builds the SQL via `?` placeholders
  for every user-controlled value (`Search`, `SetID`, `SetIDs`, `Tags`, `Type`,
  `MinDuration`, `MaxDuration`, `Limit`, `Offset`). The `LIKE` term for
  `search` is escaped with backslash for `\`, `%` and `_`. No `Sprintf` is
  used to inline any of those values.
- The only `fmt.Sprintf` in that query path is `HAVING COUNT(DISTINCT t.name)
  = N` where `N = len(filter.Tags)` (an `int`, not user-controlled bytes).
- `Sort` is fed through a `switch` whitelist (`duration`, `play_count`,
  `date`, `random`, default `file_name`) before being appended — anything
  else falls through to the default. So `sort=name; DROP TABLE media; --`
  is silently swapped for `ORDER BY media.file_name`.
- `set_id` and `set_ids` go through `strconv.ParseInt(...)` in
  `internal/api/handlers_media.go::parseMediaListQuery`; non-numeric pieces
  are dropped on the floor before they reach SQL.
- Share routes are wired as `s.mux.HandleFunc("GET /s/{token}", ...)`. Go's
  `http.ServeMux` path-pattern matcher cleans `..` segments before pattern
  matching, so `/s/../admin/users` does NOT bind `token=..` and forward to
  the share handler.

> **Any HTTP 500 returned by the probes below is almost certainly a real
> injection vulnerability** (a malformed SQL string reached the engine, or
> a panic was raised on a bad input). Treat 500 as a hard failure and file a
> defect via `ask add`.

> **Any response that returns far more rows than a literal search would
> match** (e.g. all media for `search=' OR 1=1 --`) is also a real defect —
> it would indicate that the `LIKE` clause is being short-circuited.

---

## Setup

1. Log in as the admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm HTTP 200
   and save the returned `session` cookie as `ADMIN_COOKIE`. All subsequent
   requests in this scenario use `ADMIN_COOKIE` unless stated otherwise.

2. Snapshot the baseline media count. Call
   `GET /api/v1/media?limit=1000` with `ADMIN_COOKIE`. Confirm HTTP 200,
   parse the JSON array, and save the length as `BASELINE_COUNT`. Save the
   first item's `id` as `MEDIA_ID` for use in Part B.

3. Snapshot the baseline user count. Call `GET /api/v1/admin/users` with
   `ADMIN_COOKIE`. Confirm HTTP 200 and save the array length as
   `BASELINE_USERS`.

---

## Part A — SQL injection probes on `GET /api/v1/media`

Every probe below MUST return HTTP 200 with a JSON array body. A 500 here is
a real vulnerability. A response that returns `BASELINE_COUNT` rows (i.e.
behaves as if no filter was applied) when the literal string clearly does
not match any file is also a vulnerability (`LIKE` injection succeeded).

The payloads include shell metacharacters and must be URL-encoded by the
HTTP client; do NOT shell-escape them yourself — let the client encode the
query string.

4. Classic `OR 1=1` probe against `search`. Call
   `GET /api/v1/media?search=' OR 1=1 --` with `ADMIN_COOKIE`. Confirm:
   - HTTP 200 (NOT 500).
   - Body is a JSON array.
   - Array length is much less than `BASELINE_COUNT` — ideally zero,
     because no real file name on disk literally contains `' OR 1=1 --`.
     If the length equals `BASELINE_COUNT`, this is a CRITICAL defect:
     LIKE injection is succeeding and the `?` placeholder is being bypassed.

5. `DROP TABLE` probe against `search`. Call
   `GET /api/v1/media?search='; DROP TABLE users; --` with `ADMIN_COOKIE`.
   Confirm HTTP 200 and a JSON array. Then verify the users table still
   exists by calling `GET /api/v1/admin/users` with `ADMIN_COOKIE`:
   - Confirm HTTP 200.
   - Confirm the array length equals `BASELINE_USERS`. A 500 or any drop
     in the user count would mean the `;` separator was honoured.

6. `LIKE`-wildcard escape probe against `search`. Call
   `GET /api/v1/media?search=%25` with `ADMIN_COOKIE` (URL-encoded `%`).
   Confirm HTTP 200 and a JSON array whose length is much less than
   `BASELINE_COUNT` — the server escapes `%` so the search becomes a
   literal `%` lookup, not a "match everything" wildcard.

7. Numeric-typed `set_id` injection. Call
   `GET /api/v1/media?set_id=1 OR 1=1` with `ADMIN_COOKIE`. Confirm HTTP 200
   and a JSON array. The handler runs `strconv.ParseInt("1 OR 1=1", ...)`
   which fails, so `filter.SetID` stays `nil` and the server returns the
   unfiltered list (length close to `BASELINE_COUNT`). The probe is NOT
   expected to filter to `set_id=1` — the parse fails entirely. A 500
   here is a defect.

8. `UNION SELECT` probe against `set_ids`. Call
   `GET /api/v1/media?set_ids=1,2,3) UNION SELECT * FROM users --` with
   `ADMIN_COOKIE`. Confirm:
   - HTTP 200.
   - Body is a JSON array (NOT a row from `users`, which has a totally
     different schema and would either crash `scanMedia` or surface as
     a 500).
   - Items in the array have shape `{id, set_id, rel_path, ...}` — the
     same shape as a normal media list — NOT `{username, password_hash}`.

9. `sort` whitelist probe. Call
   `GET /api/v1/media?sort=name; DROP TABLE media; --` with `ADMIN_COOKIE`.
   Confirm HTTP 200 and a JSON array. The `switch` statement in
   `ListMedia` falls through to the default branch (`ORDER BY
   media.file_name`) for any unknown sort key, so the payload is silently
   discarded. Then verify the media table still exists by calling
   `GET /api/v1/media?limit=1` with `ADMIN_COOKIE` and confirming HTTP 200
   with a non-empty array (assuming `BASELINE_COUNT > 0`).

10. Empty-string quoting probe against `tags`. Call
    `GET /api/v1/media?tags=' OR ''='` with `ADMIN_COOKIE`. Confirm HTTP
    200 and a JSON array. The `tags` value is split on `,` and each part
    is bound as a `?` placeholder against `t.name`; no real tag is named
    `' OR ''='`, so the result must be empty or close to empty (definitely
    NOT `BASELINE_COUNT`).

---

## Part B — XSS probes via writable fields

The Player server is API-first; XSS prevention for any content the user
stores (notes, tag names) is the UI's responsibility (Vue auto-escapes
text bindings; HTML interpolation is opt-in). The server's job is:

- store the literal string the user submitted, byte-for-byte,
- return it byte-for-byte on read, and
- NEVER reflect attacker-controlled input back into an HTML/JSON error
  body without escaping.

11. Store an XSS payload as a note. Call
    `POST /api/v1/media/{MEDIA_ID}/notes` with `ADMIN_COOKIE`, header
    `Content-Type: application/json`, body
    `{"content": "<script>alert(1)</script>"}`. Confirm HTTP 200 and that
    the returned JSON has `content` equal to the literal string
    `<script>alert(1)</script>`. The server MUST NOT strip or rewrite
    the tags.

12. Read the note back. Call `GET /api/v1/media/{MEDIA_ID}/notes` with
    `ADMIN_COOKIE`. Confirm HTTP 200 and that the `content` field of the
    response equals the literal `<script>alert(1)</script>` from step 11.
    The bytes must round-trip unchanged.

13. Store an XSS payload as a tag name. Call
    `POST /api/v1/media/{MEDIA_ID}/tags` with `ADMIN_COOKIE`, header
    `Content-Type: application/json`, body
    `{"tag": "<img src=x onerror=alert(1)>"}`. Confirm the response is
    HTTP 200 (the current `tagService.AssignTag` does not validate the
    characters in a tag name — it just calls `GetTagByName` and
    `CreateTag` if missing, both of which use `?` placeholders). If the
    server is later hardened to reject non-printable or HTML-shaped tag
    names, accept HTTP 400 here instead and record the new behaviour by
    updating this step rather than flagging it as a defect.

14. Confirm the tag round-trips. Call `GET /api/v1/tags` with
    `ADMIN_COOKIE`. Confirm HTTP 200, the body is a JSON array, and at
    least one element has `name` equal to the literal
    `<img src=x onerror=alert(1)>` from step 13.

15. Error-body reflection probe. Call
    `GET /api/v1/media?search=<script>alert(2)</script>&limit=foo` with
    `ADMIN_COOKIE` (the `limit=foo` is irrelevant — the handler just
    discards an unparseable limit). Confirm:
    - HTTP 200 (the request is well-formed; the server is tolerant of
      unparseable limit values).
    - The body MUST NOT contain the literal substring
      `<script>alert(2)</script>` — the request payload should never be
      echoed back into a JSON response body, even on the happy path. If
      it is reflected, file a defect.

16. Cleanup the note and tag created above so subsequent scenario runs
    are not polluted:
    - Call `DELETE /api/v1/media/{MEDIA_ID}/notes` with `ADMIN_COOKIE`.
      Confirm HTTP 200.
    - Call
      `DELETE /api/v1/media/{MEDIA_ID}/tags/%3Cimg%20src%3Dx%20onerror%3Dalert%281%29%3E`
      with `ADMIN_COOKIE` (URL-encoded form of
      `<img src=x onerror=alert(1)>`). Confirm HTTP 200. If the URL
      encoding is rejected by the router, this is acceptable — record
      it as a TODO and continue; the leftover tag does no harm.

---

## Part C — Path traversal in the share-token route

The public share routes are wired as `GET /s/{token}` (and
`/s/{token}/stream`, `/s/{token}/thumbnail`, `/s/{token}/download`). Go's
`http.ServeMux` cleans `..` path segments before pattern matching, so a
crafted URL must NOT punch through to an admin route.

17. Send `GET /s/../admin/users` with NO cookie and NO Authorization
    header. Confirm:
    - HTTP status is NOT 200.
    - HTTP status is 401, 403, or 404 — anything that proves the
      request did NOT execute `handleListUsers` as an unauthenticated
      caller. 401 or 403 (with HTML body) is the expected result on a
      modern Go runtime, because `ServeMux` rewrites the URL path to
      `/admin/users` and then `RequireSession`/`RequireAdmin` rejects
      the unauthenticated request. 404 is also acceptable if the
      router declines to match the rewritten path. A 200 here would
      mean the share handler treated `..` as a literal token and the
      admin handler was bypassed entirely — that is a critical
      vulnerability.

18. Send `GET /s/..%2Fadmin%2Fusers` (URL-encoded `/`) with NO cookie
    and NO Authorization header. Confirm HTTP status is 404. The token
    `..%2Fadmin%2Fusers` decodes to `../admin/users` AFTER pattern
    matching, so the share handler does receive that literal string as
    the token, looks it up in the shares table, finds nothing, and
    returns 404. A 200 here would mean the share lookup succeeded for
    an attacker-supplied path, which would be a vulnerability.

19. Send `GET /s/%2E%2E%2Fadmin%2Fusers/stream` with NO cookie. Confirm
    HTTP status is 404 — same reasoning as step 18 applied to the
    stream sub-route.

---

## D) Final state check

20. Re-snapshot the user table to confirm none of the DROP TABLE probes
    succeeded. Call `GET /api/v1/admin/users` with `ADMIN_COOKIE` and
    confirm HTTP 200 with array length equal to `BASELINE_USERS`.

21. Re-snapshot the media count to confirm none of the DROP TABLE probes
    against `media` succeeded. Call `GET /api/v1/media?limit=1000` with
    `ADMIN_COOKIE` and confirm HTTP 200 with array length equal to
    `BASELINE_COUNT` (give or take 0 — this scenario does not create or
    delete media, only notes/tags on an existing media row).

22. **Flag any 500 responses observed above as defects.** If any step
    returned HTTP 500, open a task with
    `ask add "+security S25 returned 500 on <step>: probable injection"`
    and attach the exact failing request. Do NOT silently retry — a 500
    on these probes is the entire point of this scenario.
