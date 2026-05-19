---
id: S18
title: "Single progress update + in-progress listing"
tags: [progress, api, in-progress]
preconditions:
  server_state: running        # server running with admin account and at least two media items
  fixtures: []
assertions:
  - status_code: "GET /api/v1/in-progress 200"
  - db: "SELECT media_id FROM playback_progress WHERE position_seconds > 0"
skip: false
---

# Scenario note

This scenario exercises the single-update progress path (`POST /api/v1/progress`)
and the in-progress listing (`GET /api/v1/in-progress`). The bulk/batch path
and the per-item `progress/status` reset are covered by S10; this scenario
focuses on the one-shot wire format `{"media_id": ..., "position_seconds": ...}`,
its validation rules, and how items move in and out of the in-progress list.

A few server-side details that shape the assertions below (see
`internal/api/handlers_progress.go`, `internal/service/progress.go`, and
`internal/repository/playback_progress.go`):

- The session cookie is mandatory — the handler reads `sessionID` from the
  request context and rejects empty values.
- `media_id == 0` is rejected with HTTP 400 before any DB call.
- The `/in-progress` query joins `playback_progress` against `playback_accumulator`
  and only returns items whose accumulator has crossed the 60-second threshold.
  Each `POST /api/v1/progress` adds at most 12 seconds of accumulated playback
  (the delta is clamped to `[0, 12]`), so reaching the threshold requires at
  least five successive updates against the same media item.
- The `/in-progress` response is a JSON array of media objects (the standard
  `model.Media` shape — `id`, `set_id`, `file_name`, `type`, `duration`, …).
  It does NOT embed the current `position_seconds` for each entry; the
  authoritative position lives in the `playback_progress` table and is
  reachable per-item via `GET /api/v1/media/{id}` (see step 7 below).
- The handler does NOT call `verifyAccess` before upsert. A POST with a
  non-existent `media_id` triggers a SQLite foreign-key violation
  (`media_id REFERENCES media(id)`) which falls through `handleError` as a
  default-case 500 — not a clean 404. The negative step that exercises this
  path accepts either 500 or 4xx so the scenario does not fail on the current
  behaviour, but treat a 500 here as a real defect worth investigating.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Find two media items to operate on: call `GET /api/v1/media?limit=2` with
   the session cookie. Confirm the response is HTTP 200 and the JSON body
   contains at least two media objects. Save the `id` of the first item as
   `media_id_1` and the `id` of the second item as `media_id_2`.

3. Push a small single update for the first item: call
   `POST /api/v1/progress` with the session cookie and body
   `{"media_id": <media_id_1>, "position_seconds": 12.5}`. Confirm the response
   is HTTP 200 and the returned JSON body is `{"status": "ok"}`.

4. Push a single update for the second item: call `POST /api/v1/progress` with
   the session cookie and body
   `{"media_id": <media_id_2>, "position_seconds": 90.0}`. Confirm the response
   is HTTP 200 and the returned JSON body is `{"status": "ok"}`.

5. Push additional successive updates against `media_id_1` to cross the
   60-second accumulator threshold. Each `POST /api/v1/progress` adds at most
   12 seconds of accumulated playback (the server clamps the delta to
   `[0, 12]`), so issue five further calls with increasing positions:
   `{"media_id": <media_id_1>, "position_seconds": 24.0}`,
   `{"media_id": <media_id_1>, "position_seconds": 36.0}`,
   `{"media_id": <media_id_1>, "position_seconds": 48.0}`,
   `{"media_id": <media_id_1>, "position_seconds": 60.0}`, and
   `{"media_id": <media_id_1>, "position_seconds": 72.0}`. Confirm each
   response is HTTP 200 with body `{"status": "ok"}`. After this step the
   server-side accumulator for `media_id_1` should be at or above 60 seconds.

6. Repeat the same pattern for `media_id_2` so it also crosses the
   accumulator threshold: issue five further updates with positions
   `102.0`, `114.0`, `126.0`, `138.0`, and `150.0`. Confirm each response is
   HTTP 200 with body `{"status": "ok"}`.

7. Verify the per-item position was recorded for `media_id_1`: call
   `GET /api/v1/media/{media_id_1}` with the session cookie. Confirm the
   response is HTTP 200 and the returned JSON contains a `progress` object
   whose `position_seconds` matches the last value sent in step 5 (72.0). The
   single-update endpoint does not return the position, so this is the
   authoritative check that the upsert landed.

8. List the in-progress items: call `GET /api/v1/in-progress` with the
   session cookie. Confirm the response is HTTP 200 and the body is a JSON
   array. Confirm the array contains an entry whose `id` equals `media_id_1`
   AND an entry whose `id` equals `media_id_2`. Note: the response entries
   are standard `model.Media` objects (`id`, `set_id`, `file_name`, `type`,
   `duration`, …) and do NOT carry `position_seconds`; per-item position
   verification was already done in step 7. If either item is missing from
   the array, the accumulator threshold was not crossed — fail the scenario
   so the regression is caught.

9. Negative case — empty body: call `POST /api/v1/progress` with the session
   cookie and body `{}`. Confirm the response is HTTP 400 and the body
   contains the text `media_id required` (the handler short-circuits before
   touching the service).

10. Negative case — explicit zero media_id: call `POST /api/v1/progress` with
    the session cookie and body `{"media_id": 0, "position_seconds": 5}`.
    Confirm the response is HTTP 400 and the body contains the text
    `media_id required`.

11. Negative case — non-existent media_id: call `POST /api/v1/progress` with
    the session cookie and body
    `{"media_id": 999999999, "position_seconds": 1}`. Confirm the response
    status code is `>= 400`. The handler does NOT call `verifyAccess`, so the
    SQLite foreign-key constraint
    (`playback_progress.media_id REFERENCES media(id)`) fires inside the
    upsert and falls through `handleError` as the default case (HTTP 500).
    A future fix that adds an explicit existence check and returns HTTP 404
    here is acceptable — both 404 and 500 satisfy this step. Treat a 200
    response as a real defect (a row was written referencing a non-existent
    media item).

12. Negative case — malformed JSON body: call `POST /api/v1/progress` with
    the session cookie and the raw request body `not-json` (Content-Type
    `application/json`). Confirm the response is HTTP 400 and the body
    contains the text `invalid body`.

13. Mark `media_id_1` as finished via the status endpoint: call
    `POST /api/v1/progress/status` with the session cookie and body
    `{"media_id": <media_id_1>, "status": "finished"}`. Confirm the response
    is HTTP 200 and the returned JSON body is `{"status": "ok"}`. Marking
    finished sets the `finished` flag in `playback_progress` and the
    in-progress listing filters those rows out.

14. Confirm `media_id_1` no longer appears in the in-progress list: call
    `GET /api/v1/in-progress` with the session cookie. Confirm the response
    is HTTP 200 and the returned array does NOT contain any entry whose `id`
    matches `media_id_1`. The array MAY still contain `media_id_2` (its
    progress was not changed in step 13), and the YAML `db` assertion
    independently verifies that at least one `playback_progress` row with
    `position_seconds > 0` exists in the database after this scenario
    completes.

15. Cleanup: reset both media items back to `not_started` so subsequent runs
    of this scenario, S10 or S07 start from a clean state. Call
    `POST /api/v1/progress/status` with the session cookie and body
    `{"media_id": <media_id_1>, "status": "not_started"}`, then call it again
    with body `{"media_id": <media_id_2>, "status": "not_started"}`. Confirm
    each response is HTTP 200 with body `{"status": "ok"}`.
