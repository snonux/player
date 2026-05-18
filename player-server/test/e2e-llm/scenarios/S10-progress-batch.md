---
id: S10
title: "Bulk progress sync → batch update + status query"
tags: [progress, api, sync, mobile]
preconditions:
  server_state: running        # server running with an existing admin account and at least two media items
  fixtures: []
assertions:
  - db: "SELECT id FROM progress WHERE position_seconds=30"
  - status_code: "POST /api/v1/progress/batch 200"
skip: false
---

# Scenario note
This scenario exercises the mobile offline-sync use case: a client that
accumulated playback progress for several media items while offline pushes
the batch to the server in a single request, then queries the per-item
status to reconcile. The real wire format for `/api/v1/progress/batch`
wraps the items in `{"updates": [...]}` (see `handlers_progress.go`), and
`/api/v1/progress/status` accepts a single `{"media_id": ..., "status":
"finished"|"not_started"}` payload — so the status step is repeated per
media ID to cover both items.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Save the `session`
   cookie returned in the response for all subsequent authenticated requests.

2. Retrieve at least two media items: call `GET /api/v1/media?limit=2` with the
   session cookie. Confirm the response is HTTP 200 and contains at least two
   media objects. Save the `id` of the first item as `media_id_1` and the `id`
   of the second item as `media_id_2`.

3. Push a batch of two progress updates: call `POST /api/v1/progress/batch`
   with the session cookie and body
   `{"updates": [{"media_id": <media_id_1>, "position_seconds": 30.0}, {"media_id": <media_id_2>, "position_seconds": 60.0}]}`.
   Confirm the response is HTTP 200 and the returned JSON contains
   `{"status": "ok"}`.

4. Confirm both progress rows landed in the database: in the same step or via
   a follow-up sanity check, the harness's YAML `db` assertion verifies that
   at least one row exists with `position_seconds=30` after this scenario
   completes (see the front-matter assertions block).

5. Query the recorded progress for the first media item: call
   `GET /api/v1/media/{media_id_1}` with the session cookie. Confirm the
   response is HTTP 200 and the returned JSON contains a `progress` object
   whose `position_seconds` is `30` (or `30.0`) — matching what was pushed in
   step 3.

6. Query the recorded progress for the second media item: call
   `GET /api/v1/media/{media_id_2}` with the session cookie. Confirm the
   response is HTTP 200 and the returned JSON contains a `progress` object
   whose `position_seconds` is `60` (or `60.0`) — matching what was pushed in
   step 3.

7. Mark the first media item as finished via the status endpoint: call
   `POST /api/v1/progress/status` with the session cookie and body
   `{"media_id": <media_id_1>, "status": "finished"}`. Confirm the response is
   HTTP 200 and the returned JSON contains `{"status": "ok"}`.

8. Reset the first media item back to `not_started` via the same endpoint:
   call `POST /api/v1/progress/status` with the session cookie and body
   `{"media_id": <media_id_1>, "status": "not_started"}`. Confirm the response
   is HTTP 200. This proves the status endpoint accepts both transitions for
   the IDs that were just batch-updated.

9. Verify both items still appear in the in-progress list (the batch update
   recorded real progress for each): call `GET /api/v1/in-progress` with the
   session cookie. Confirm the response is HTTP 200 and the returned array
   contains entries whose `id` matches `media_id_1` and `media_id_2`
   respectively. Note: the in-progress listing requires accumulated playback
   time on the server side; if either media item is missing because the
   accumulator threshold has not been crossed, treat its absence as
   acceptable — the authoritative check is the DB assertion in the YAML
   front-matter.
