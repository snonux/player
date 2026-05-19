---
id: S24
title: "Soft-delete persistence across admin rescan (and disk-deletion behaviour)"
tags: [media, admin, scan, rescan, trash, soft-delete]
preconditions:
  server_state: running        # server running with an existing admin account and media library
  fixtures: []
assertions:
  - db: "SELECT id FROM media WHERE deleted_at IS NOT NULL"
  - status_code: "POST /api/v1/admin/rescan 200"
skip: false
---

This scenario answers the question: when a soft-deleted media row is
re-encountered by an admin rescan (the file still exists on disk under the
media root), does the rescan resurrect it, leave the soft-delete intact, or
fail outright? The expected and desired behaviour is **(b): the soft-delete
sticks across rescans** — re-importing a row the admin explicitly trashed
would be a surprising, silent "undelete". A secondary check probes what
happens when the underlying file is removed from disk while a non-deleted
media row points to it.

Two real defects in the scanner used to be locked in by earlier drafts of
this scenario; both are now fixed and asserted as regressions:

1. **Soft-deleted rows tripped the rescan.** `FSScanner.loadExistingMedia`
   used a `ListMedia` call that filtered `deleted_at IS NULL`, so a
   re-walk of a soft-deleted file tried to `CreateMedia` for the same
   `(set_id, rel_path)` pair and hit the schema's `UNIQUE` constraint.
   `last_error` was set and the scan reported failure. Fixed by adding
   `MediaFilter.IncludeDeleted` and using it in the scanner.

2. **Files deleted from disk left orphan rows.** The scanner only walked
   files that exist and never compared the resulting set against the DB,
   so a removed file kept its row in `GET /api/v1/media` (where any
   stream attempt would 404). Fixed by `reconcileOrphans` which soft-
   deletes any active row whose `rel_path` was not seen during the walk.

The scenario asserts both fixes (clean rescan in step 13; orphan
soft-delete in step 20). A regression in either fires this scenario.

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie as `admin_session` for all
   subsequent admin requests.

2. Create an API token for the upload step (Bearer auth): call
   `POST /api/v1/auth/tokens` with the `admin_session` cookie and body
   `{"name": "e2e-soft-delete-rescan", "expires_in_days": 1}`. Confirm the
   response is HTTP 200, save the `token` plaintext as `BEARER_TOKEN`, and
   save the returned `id` as `token_id`.

3. List the available sets: call `GET /api/v1/sets` with the `admin_session`
   cookie. Confirm the response is HTTP 200. Save the `id` of the first
   non-podcast set whose `root_path` is `audiobooks` (or, failing that, the
   first non-podcast set in the list) as `set_id`, and save that set's
   `root_path` as `set_root_path`.

4. Prepare a small disposable test audio file. Run
   `ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 1 -q:a 9 -acodec libmp3lame /tmp/test-soft-delete-rescan.mp3 -y`
   to create a 1-second silent MP3. Confirm `/tmp/test-soft-delete-rescan.mp3`
   exists and is non-empty.

5. Upload the file to the set: call
   `POST /api/v1/sets/{set_id}/upload` as a `multipart/form-data` request
   with:
   - `Authorization: Bearer {BEARER_TOKEN}` header
   - form field `file` containing the bytes of
     `/tmp/test-soft-delete-rescan.mp3` with filename
     `test-soft-delete-rescan.mp3`.
   Confirm the response is HTTP 200 and the returned JSON contains a non-zero
   `id`. Save `media_id` and save the returned `abs_path` (or, if not
   returned, reconstruct it as
   `<MEDIA_ROOT>/<set_root_path>/test-soft-delete-rescan.mp3`) as
   `abs_path`.

6. Confirm the freshly uploaded item appears in the active media list: call
   `GET /api/v1/media` with the `admin_session` cookie. Confirm the response
   is HTTP 200 and the returned list contains an entry whose `id` matches
   `media_id`.

7. Soft-delete the media item: call `DELETE /api/v1/media/{media_id}` with
   the `admin_session` cookie. Confirm the response is HTTP 200.

8. Confirm the item no longer appears in the active media list: call
   `GET /api/v1/media` with the `admin_session` cookie. Confirm the response
   is HTTP 200 and the returned list does NOT contain any entry whose `id`
   matches `media_id`.

9. Confirm the item is now in the admin trash: call
   `GET /api/v1/admin/trash` with the `admin_session` cookie. Confirm the
   response is HTTP 200 and the returned list contains an entry whose `id`
   matches `media_id`.

10. Confirm the DB row has `deleted_at` set: run
    `db: SELECT deleted_at FROM media WHERE id={media_id}` and confirm the
    result is a single row with a non-NULL `deleted_at` timestamp.

11. Trigger a full media rescan: call `POST /api/v1/admin/rescan` with the
    `admin_session` cookie and an empty body. Confirm the response is HTTP
    200 and the returned JSON body is `{"status": "ok"}`.

12. Poll `GET /api/v1/admin/scan-progress` with the `admin_session` cookie
    every 1 s, for up to 60 polls, until the JSON object has
    `running: false`. On each poll the response must be HTTP 200. Save the
    final JSON object as `final_progress` for the next step.

13. Inspect the scan outcome. Read `final_progress.last_error`: it MUST be
    empty or absent — the scanner's dedup map now includes soft-deleted
    rows (via `MediaFilter.IncludeDeleted`), so it skips re-inserting them
    and never hits the `UNIQUE(set_id, rel_path)` constraint. A non-empty
    `last_error` (especially text matching
    `UNIQUE constraint failed: media.set_id, media.rel_path`) is a
    regression of that fix and must fail the scenario. Combined with
    step 14, only the clean case is acceptable: rescan succeeds AND the
    soft-deleted row stays out of `GET /api/v1/media`.

14. Re-query the active list to verify the soft-delete persisted: call
    `GET /api/v1/media` with the `admin_session` cookie. Confirm the response
    is HTTP 200 and the returned list does NOT contain any entry whose `id`
    matches `media_id`. Also call `GET /api/v1/admin/trash` and confirm the
    list STILL contains `media_id`.

15. Confirm the DB state did not change: run
    `db: SELECT deleted_at FROM media WHERE id={media_id}` and confirm the
    result is a single row with a non-NULL `deleted_at` timestamp (same row,
    not resurrected; not a new row with NULL `deleted_at`). If a NEW row
    with the same `(set_id, rel_path)` and `deleted_at IS NULL` exists, fail
    the scenario — this is case (a). Run
    `db: SELECT count(*) FROM media WHERE set_id={set_id} AND rel_path='test-soft-delete-rescan.mp3'`
    and confirm the count is exactly 1.

16. Reverse check — file deleted from disk while media row exists. First
    upload a second disposable file: call
    `POST /api/v1/sets/{set_id}/upload` (same content as step 5) with
    filename `test-disk-deleted.mp3` and `Authorization: Bearer
    {BEARER_TOKEN}`. Confirm the response is HTTP 200, save `media_id_2` and
    `abs_path_2` (reconstruct as
    `<MEDIA_ROOT>/<set_root_path>/test-disk-deleted.mp3` if needed).

17. Delete the underlying file on disk (NOT via the API) by running
    `rm -f {abs_path_2}`. Confirm the file no longer exists with
    `test ! -e {abs_path_2}`. The media row in the DB is intentionally left
    intact at this stage.

18. Trigger a second rescan: call `POST /api/v1/admin/rescan` with the
    `admin_session` cookie and an empty body. Confirm the response is HTTP
    200.

19. Poll `GET /api/v1/admin/scan-progress` with the `admin_session` cookie
    every 1 s for up to 60 polls until `running: false`.

20. Verify the rescan reconciled the orphaned row. The scanner now soft-
    deletes media whose underlying file disappeared between scans (see
    `reconcileOrphans` in `internal/scanner/scanner.go`). Run
    `db: SELECT count(*) FROM media WHERE id={media_id_2}` and confirm the
    count is exactly 1 — orphans are SOFT-deleted, not hard-deleted. Then
    run `db: SELECT deleted_at FROM media WHERE id={media_id_2}` and
    confirm `deleted_at` is NOT NULL (a recent timestamp). A NULL
    `deleted_at` here means the orphan-reconcile pass failed to fire —
    regression.

21. Confirm the orphan no longer appears in the active media list: call
    `GET /api/v1/media` with the `admin_session` cookie and verify NO
    entry has `id == media_id_2`. It must instead appear in
    `GET /api/v1/admin/trash` (alongside the row from step 7).

22. Cleanup — `media_id_2` is already soft-deleted by the orphan-reconcile
    pass in step 20, so no DELETE call is needed for it. Remove the first
    test file from disk if it still exists: run `rm -f {abs_path}`. Both
    rows now have `deleted_at IS NOT NULL` and live in the trash; subsequent
    rescans will not re-import them because the dedup map sees them.

23. Revoke the API token: call `DELETE /api/v1/auth/tokens/{token_id}` with
    the `admin_session` cookie. Confirm the response is HTTP 200.

24. Final assertions:
    - The active media list (`GET /api/v1/media`) must not contain either
      `media_id` or `media_id_2`.
    - The admin trash list (`GET /api/v1/admin/trash`) must contain both
      `media_id` and `media_id_2`.
    - `db: SELECT id FROM media WHERE deleted_at IS NOT NULL` must return at
      least these two rows.
