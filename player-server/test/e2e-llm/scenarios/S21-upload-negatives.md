---
id: S21
title: "Upload negatives — missing parts, bad extensions, traversal, 404/403/413"
tags: [upload, api, security, negative-path]
preconditions:
  server_state: running        # server running with admin account and at least one set
  fixtures: []
assertions:
  - status_code: "POST /api/v1/sets/upload 400"
  - db: "SELECT file_name FROM media WHERE file_name LIKE 'e2e-s21-dedupe%' OR file_name LIKE '%passwd%'"
skip: false
---

# Purpose

This scenario exercises the negative-path branches of the upload handler
(`handleUpload` in `internal/api/handlers_media.go`, lines ~107-155) and the
write service (`writeService.UploadMedia` in `internal/service/write.go`,
lines ~59-99). It locks in the expected status codes for malformed multipart
bodies, missing file fields, unsupported extensions, path-traversal-style
filenames, missing sets, forbidden access, oversized payloads, and the
filename-deduplication behaviour of `uniqueFilename` (`internal/service/filename.go`).

Server-side details that shape the assertions below:

- `handleUpload` wraps `r.Body` in `http.MaxBytesReader` with
  `cfg.MaxUploadSizeMB` (default `100`, from `internal/config.go`). When the
  body exceeds the limit, `ParseMultipartForm` returns a `*http.MaxBytesError`
  and the handler responds with HTTP **413** (`StatusRequestEntityTooLarge`)
  and JSON body `{"error":"file too large"}`.
- Any other `ParseMultipartForm` error (malformed body, missing
  `Content-Type: multipart/form-data` boundary, etc.) maps to **400** with
  body `"invalid multipart form"`.
- A successfully parsed multipart with no `file` part maps to **400** with
  body `"missing file"`.
- The service rejects unknown extensions (anything not in
  `internal/mediatype.IsSupportedExt`) with `ErrUnsupportedExtension`, which
  the handler maps to **400**. Plain `.txt` is NOT a supported extension.
- For a nonexistent set, the service returns `ErrNotFound` → handler maps to
  **404**.
- For a non-admin user without `set_permissions.role` on the set, the access
  helper returns `ErrForbidden` → handler maps to **403**.
- Filenames are sanitized by `uniqueFilename` which calls `filepath.Base()`
  first, so a traversal-style input like `../../../etc/passwd.mp3` collapses
  to `passwd.mp3` and lands inside the set's `MEDIA_ROOT` directory. If the
  saved row ends up with any path separator in `file_name`, or if a file
  actually lands outside `MEDIA_ROOT`, treat that as a security defect and
  file a task — the YAML `db` assertion above looks for the saved name.
- Repeated uploads with the same filename are deduplicated by appending
  `(1)`, `(2)`, … before the extension (see `uniqueFilename`); both rows
  must exist in `media`.

If `MAX_UPLOAD_SIZE_MB` cannot be lowered for the run, the actual 413 step
(step 11) is skipped — see the inline note there.

---

## A) Setup

1. Authenticate as the admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie as `ADMIN_COOKIE` for all
   subsequent admin requests.

2. Find a set to upload into: call `GET /api/v1/sets` with `ADMIN_COOKIE`.
   Confirm the response is HTTP 200 and the body contains at least one set.
   Save the `id` of the first non-podcast set as `set_id`. (Any writable
   filesystem-backed set will do; `musicvideos` from the seeded fixtures is
   a good default.)

3. Pick a clearly nonexistent set id for the 404 step: use `set_id_missing =
   999999999`. Do NOT pick a value that may collide with a real row.

4. Prepare a tiny valid media payload for the steps that need a real body.
   Use the fixture file at `player-server/testdata/media/podcast/` or create
   a 1-second silent MP3 with
   `ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 1 -q:a 9 -acodec libmp3lame /tmp/s21-tiny.mp3`.
   Confirm the file exists and is non-empty. This file is reused by the
   traversal, empty-filename, and dedupe steps below.

## B) Malformed / missing multipart bodies (HTTP 400)

5. Empty body, no multipart header: call
   `POST /api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE`, no body, and no
   `Content-Type` header. Confirm the response is HTTP **400** and the body
   contains the text `invalid multipart form`. (Go's `mime/multipart` parser
   refuses to start without a boundary.)

6. Multipart envelope but no `file` field: build a multipart body that
   contains a single text field (e.g. `note=hello`) and POST it to
   `/api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE` and the matching
   `Content-Type: multipart/form-data; boundary=…` header. Confirm the
   response is HTTP **400** and the body contains the text `missing file`.
   The form must parse cleanly — only the `file` part is absent.

## C) Unsupported extension (HTTP 400)

7. Plain-text payload with a `.txt` filename: build a multipart body whose
   `file` part has `filename="e2e-s21.txt"` and content `hello world`. POST
   it to `/api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE`. Confirm the
   response is HTTP **400** and the body contains `unsupported extension`
   (the wrapped `ErrUnsupportedExtension` message includes the rejected
   extension, e.g. `.txt`). Confirm via `GET /api/v1/media?set_id={set_id}&search=e2e-s21`
   that NO row with `file_name = "e2e-s21.txt"` exists — the service must
   reject before the file is written or the DB row inserted.

8. Double-extension probe (defensive — flag any bug): build a multipart body
   whose `file` part has `filename="e2e-s21.mp3.txt"` and short text
   content. POST it. The expected behaviour is HTTP **400** because
   `IsSupportedExt` calls `filepath.Ext(name)` which only returns the LAST
   extension (`.txt`). If the response is HTTP 200, that means the
   extension validator was bypassed by a double-extension trick — file a
   bug task and confirm by checking the DB for a row with `file_name LIKE
   'e2e-s21.mp3.txt%'`.

## D) Path-traversal filename (must be sanitized)

9. Traversal-style filename, valid extension: build a multipart body whose
   `file` part has `filename="../../../etc/passwd.mp3"` and contains the
   bytes of `/tmp/s21-tiny.mp3` from step 4. POST it to
   `/api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE`. Confirm the response
   is HTTP **200** and the returned JSON `file_name` field is exactly
   `passwd.mp3` (no slashes, no `..`). Save the returned `id` as
   `traversal_media_id`. Then:
   - Run a DB check: `SELECT file_name FROM media WHERE id =
     <traversal_media_id>`. The value MUST equal `passwd.mp3` and MUST NOT
     contain any path separator. The `rel_path` value MUST also be
     `passwd.mp3` (no traversal).
   - Run `GET /api/v1/media/{traversal_media_id}` and confirm `abs_path`
     starts with the configured `MEDIA_ROOT` (or the test's media root) —
     i.e. the file did NOT actually land in `/etc/`. If `abs_path` resolves
     outside `MEDIA_ROOT`, treat this as a critical security defect and
     file a task.

## E) Missing set (HTTP 404)

10. Upload to a nonexistent set: build a normal multipart body with a valid
    `.mp3` `file` part (filename `e2e-s21-missing.mp3`, the bytes from
    `/tmp/s21-tiny.mp3`). POST it to
    `/api/v1/sets/{set_id_missing}/upload` (the `999999999` from step 3)
    with `ADMIN_COOKIE`. Confirm the response is HTTP **404**. The service
    returns `ErrNotFound` from the `GetSetByID` branch before any file is
    written; verify no `media` row was inserted by running
    `GET /api/v1/media?search=e2e-s21-missing` and confirming the result is
    empty.

## F) Oversized payload (HTTP 413)

11. Trigger the `MaxBytesReader` 413 path. The default `MaxUploadSizeMB` is
    100 (see `DefaultMaxUploadSizeMB` in `internal/config.go`), so honestly
    streaming 100 MiB+1 bytes from the LLM harness is wasteful. Two
    acceptable strategies — pick ONE:

    **Preferred:** if the test server was started with a lowered
    `MAX_UPLOAD_SIZE_MB` (e.g. 1), build a multipart body whose `file` part
    is just over the limit (1 MiB + 64 KiB of zero bytes, filename
    `e2e-s21-big.mp3`) and POST it. Confirm the response is HTTP **413**
    and the body contains `file too large`.

    **Fallback:** send a request with `Content-Length` set just above
    `MaxUploadSizeMB << 20` but a body of the same length filled with zero
    bytes. `MaxBytesReader` enforces the limit while the body is being
    read, so a body that simply matches the declared length still trips
    the limit if it exceeds `MaxUploadSizeMB << 20`. Use a 101 MiB body
    only if the harness can stream it without buffering in RAM.

    **Skip note:** if neither option is feasible in the harness (e.g.
    `MAX_UPLOAD_SIZE_MB` is fixed at 100 and the harness cannot stream
    >100 MiB), record `skip-step-11: harness cannot trigger 413 in this
    environment` in the run annotation and continue. Do NOT fail the
    scenario on a skipped 413 — the 400 paths above already satisfy the
    YAML `status_code` assertion.

## G) Forbidden access (HTTP 403)

12. Create a temporary non-admin user without permission on `set_id`: call
    `POST /api/v1/admin/users` with `ADMIN_COOKIE` and body
    `{"username": "e2e-s21-noperm", "password": "TestPassw0rd!", "is_admin": false}`.
    Confirm the response is HTTP 200 and save the returned `id` as
    `noperm_user_id`. Do NOT grant any permission on `set_id` to this user.

13. Log in as that user: call `POST /api/v1/auth/login` with body
    `{"username": "e2e-s21-noperm", "password": "TestPassw0rd!"}` and no
    prior cookie. Confirm the response is HTTP 200 and save the session
    cookie as `USER_COOKIE`. Do NOT mix `ADMIN_COOKIE` and `USER_COOKIE`.

14. Attempt an upload as the non-admin: build a valid multipart body (mp3
    bytes from step 4, filename `e2e-s21-noperm.mp3`) and POST it to
    `/api/v1/sets/{set_id}/upload` with `USER_COOKIE` (NOT `ADMIN_COOKIE`).
    Confirm the response is HTTP **403** (`verifySetModifyAccess` returns
    `ErrForbidden` because the user has no owner role on the set). Confirm
    via `GET /api/v1/media?search=e2e-s21-noperm` (with `ADMIN_COOKIE`)
    that NO row was written.

## H) Empty filename

15. Build a multipart body whose `file` part has `filename=""` (empty
    string) and a short payload. POST it to
    `/api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE`. The expected
    behaviour is HTTP **400** because `IsSupportedExt("")` is false (the
    extension is empty), so the service returns `ErrUnsupportedExtension`.
    Confirm the body contains `unsupported extension`. A 500 response or a
    200 response that writes an empty-named row is a defect — flag it.

## I) Filename deduplication

16. Upload twice with the same filename. First call: build a multipart body
    with the `.mp3` bytes from step 4 and filename `e2e-s21-dedupe.mp3`.
    POST it to `/api/v1/sets/{set_id}/upload` with `ADMIN_COOKIE`. Confirm
    the response is HTTP **200** and save the returned `id` as
    `dedupe_id_1` and the `file_name` as `dedupe_name_1` (should be
    `e2e-s21-dedupe.mp3`).

17. Second call: POST another upload to the same set with the same
    `filename="e2e-s21-dedupe.mp3"` and the same bytes. Confirm the response
    is HTTP **200** and save the returned `id` as `dedupe_id_2` and
    `file_name` as `dedupe_name_2`. The new `file_name` MUST be different
    from `dedupe_name_1` (the helper appends `(1)`, so expect
    `e2e-s21-dedupe(1).mp3`). If `dedupe_name_2 == dedupe_name_1` or if
    the second POST overwrote the first row, that is a regression — file
    a bug.

18. Confirm both rows exist: call
    `GET /api/v1/media?set_id={set_id}&search=e2e-s21-dedupe` with
    `ADMIN_COOKIE`. Confirm the result contains both `dedupe_id_1` and
    `dedupe_id_2` and that their `file_name` values differ.

## J) Cleanup

19. Soft-delete every media row this scenario created. For each id in
    `[traversal_media_id, dedupe_id_1, dedupe_id_2]` that is set, call
    `DELETE /api/v1/media/{id}` with `ADMIN_COOKIE` and confirm the
    response is HTTP 200. Skip ids that were never assigned (e.g. if step 9
    is being investigated for a security defect, leave the row in place
    and annotate the run instead).

20. Delete the temporary non-admin user: call
    `DELETE /api/v1/admin/users/{noperm_user_id}` with `ADMIN_COOKIE`.
    Confirm the response is HTTP 200 and the body is `{"status": "ok"}`.
    Leftover users will pollute future runs of S12, S13, S15, and S21.

21. Confirm cleanup: call `GET /api/v1/admin/users` with `ADMIN_COOKIE` and
    confirm the array does NOT contain any entry whose `username` is
    `e2e-s21-noperm`. Call
    `GET /api/v1/media?set_id={set_id}&search=e2e-s21-dedupe` and confirm
    the result is empty (or only contains rows whose `deleted_at` is set,
    depending on whether the API filters soft-deleted by default).
