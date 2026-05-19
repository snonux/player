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

Reading `internal/scanner/scanner.go` shows that
`FSScanner.loadExistingMedia` uses `repository.ScannerStore.ListMedia` to
build its `existing` map, and `repository/media.go` always appends
`media.deleted_at IS NULL` to the `ListMedia` predicate. That means
soft-deleted rows are invisible to the scanner's dedup map, so a re-walk will
try to `CreateMedia` for the same `(set_id, rel_path)` pair — which the
schema constrains with `UNIQUE(set_id, rel_path)` (see
`internal/repository/schema.go`). The likely observable outcomes are
therefore: rescan fails with a UNIQUE constraint error and the soft-delete
remains (b with a noisy side-effect on `last_error`), or — if the harness
ever changes to upsert — the row is silently resurrected (a, a defect).
The scenario asserts (b) and treats (a) as a failure.

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

13. Inspect the scan outcome. Read `final_progress.last_error` (which may be
    absent or empty when the scan succeeded). Record one of three observed
    cases:
    - **Case (a)** — `last_error` is empty/absent AND step 14 shows the
      soft-deleted row resurfaced in `GET /api/v1/media`. This is a defect:
      rescan silently undeleted media. Annotate task 89 with the observation
      and fail the scenario at step 15.
    - **Case (b-clean)** — `last_error` is empty/absent AND the soft-deleted
      row stays out of `GET /api/v1/media`. This is the desired behaviour.
    - **Case (b-noisy)** — `last_error` contains a UNIQUE constraint error
      (text matching `UNIQUE constraint failed: media.set_id, media.rel_path`
      or similar) AND the soft-deleted row stays out of
      `GET /api/v1/media`. The soft-delete is preserved, but rescan reports a
      failure caused by trashed entries. Annotate task 89 noting this as a
      defect candidate (rescans should not fail because of soft-deleted
      rows).

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

20. Verify what the rescan did to the orphaned row. Run
    `db: SELECT count(*) FROM media WHERE id={media_id_2}` and record the
    result. Reading `internal/scanner/scanner.go` shows the scanner only
    walks files that exist and never reconciles disappeared files against
    the DB, so the expected count is 1 (row unchanged). Also run
    `db: SELECT deleted_at FROM media WHERE id={media_id_2}` and confirm
    `deleted_at` is NULL — the scanner does NOT auto-soft-delete missing
    files. If the count is 0 or `deleted_at` is non-NULL, that means the
    scanner does prune orphans and the scenario should annotate task 89 with
    the observed pruning behaviour, since it contradicts the current
    implementation.

21. Confirm the orphaned row still appears in `GET /api/v1/media`: call
    `GET /api/v1/media` with the `admin_session` cookie and verify an entry
    with `id == media_id_2` is present. (Streaming it would 404 because the
    file is gone, but listing should not.)

22. Cleanup — soft-delete then attempt hard cleanup of both rows. Call
    `DELETE /api/v1/media/{media_id_2}` with the `admin_session` cookie and
    confirm HTTP 200. The first test file at `{abs_path}` is still on disk;
    remove it directly: run `rm -f {abs_path}`. Both DB rows now have
    `deleted_at IS NOT NULL` and live in the trash.

23. Revoke the API token: call `DELETE /api/v1/auth/tokens/{token_id}` with
    the `admin_session` cookie. Confirm the response is HTTP 200.

24. Final assertions:
    - The active media list (`GET /api/v1/media`) must not contain either
      `media_id` or `media_id_2`.
    - The admin trash list (`GET /api/v1/admin/trash`) must contain both
      `media_id` and `media_id_2`.
    - `db: SELECT id FROM media WHERE deleted_at IS NOT NULL` must return at
      least these two rows.
