---
id: S19
title: "Permissions matrix — viewer vs owner across two sets"
tags: [admin, permissions, api, security, authorization]
preconditions:
  server_state: running        # existing DB with admin account and at least two sets of distinct types
  fixtures: []
assertions:
  - status_code: "GET /api/v1/sets 200"
  - db: "SELECT count(*) FROM set_permissions"
skip: false
---

# Purpose

This scenario exercises the per-set permissions matrix end-to-end so the
behaviour of `checkSetPermission`, `verifyAccess`, `verifyModifyAccess` and
`browseService.ListSets` (see `internal/service/access.go` and
`internal/service/browse.go`) cannot regress silently.

Two non-admin users are created, each with a different role on a different
set, and the scenario then asserts visibility, read access and modify access
from each user's perspective. Mark anything that deviates from the assertions
below as a real authorization defect.

Note on viewer write surface: the codebase routes tag, favorite and note
mutations through `verifyAccess` (NOT `verifyModifyAccess`). That means a
viewer is currently permitted to add tags, favorites and notes on media they
can see, even though `model.RoleViewer` is documented as "browsing and
playback" only. The steps below capture the **actual** behaviour (200 for
tag-add by a viewer). If this scenario later starts returning 403 there, the
service has been tightened on purpose — update the step. If it ever flips
back to 200 after a deliberate tightening, that is a regression.

---

## A) Admin setup

1. Authenticate as the admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the returned `session` cookie as `ADMIN_COOKIE`. All
   admin operations below use `ADMIN_COOKIE`.

2. Create the first non-admin user `U1`: call `POST /api/v1/admin/users` with
   `ADMIN_COOKIE` and body
   `{"username": "e2e-perm-u1", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON has a non-zero `id`,
   `username` equal to `e2e-perm-u1`, and `is_admin` equal to `false`. Save
   the `id` as `U1_ID`.

3. Create the second non-admin user `U2`: call `POST /api/v1/admin/users` with
   `ADMIN_COOKIE` and body
   `{"username": "e2e-perm-u2", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON has a non-zero `id`,
   `username` equal to `e2e-perm-u2`, and `is_admin` equal to `false`. Save
   the `id` as `U2_ID`.

4. As admin, list sets: call `GET /api/v1/sets` with `ADMIN_COOKIE`. Confirm
   the response is HTTP 200 and the returned array contains at least two
   distinct sets. Pick two sets with different `type` values if possible (for
   example one of type `audiobook` and one of type `image`). Save the chosen
   sets as `SET_A` (id `SET_A_ID`) and `SET_B` (id `SET_B_ID`). They must
   satisfy `SET_A_ID != SET_B_ID`. If fewer than two sets exist, abort the
   scenario with a clear message — the test environment is under-seeded.

5. Pick one active media item from each set for later assertions. As admin
   call `GET /api/v1/sets/{SET_A_ID}/browse` with `ADMIN_COOKIE`. Confirm the
   response is HTTP 200 and pick any media entry; save its id as
   `MEDIA_A_ID`. Do the same for `SET_B`: call
   `GET /api/v1/sets/{SET_B_ID}/browse`, confirm HTTP 200, and save a media
   id as `MEDIA_B_ID`. If either set has zero media items, pick a different
   pair of sets in step 4.

## B) Grant permissions

6. Grant `U1` the `viewer` role on `SET_A`: call
   `POST /api/v1/admin/permissions` with `ADMIN_COOKIE` and body
   `{"user_id": <U1_ID>, "set_id": <SET_A_ID>, "role": "viewer"}`. Confirm the
   response is HTTP 200 and the returned JSON body is `{"status": "ok"}`. `U1`
   now has read-only access to `SET_A` and no access to `SET_B`.

7. Grant `U2` the `owner` role on `SET_B`: call
   `POST /api/v1/admin/permissions` with `ADMIN_COOKIE` and body
   `{"user_id": <U2_ID>, "set_id": <SET_B_ID>, "role": "owner"}`. Confirm the
   response is HTTP 200 and the returned JSON body is `{"status": "ok"}`.
   `U2` now has full modify access to `SET_B` and no access to `SET_A`.

8. Verify the matrix: call `GET /api/v1/admin/permissions` with
   `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the `permissions`
   array contains both grants — one entry with
   `{user_id: U1_ID, set_id: SET_A_ID, role: "viewer"}` and one with
   `{user_id: U2_ID, set_id: SET_B_ID, role: "owner"}`.

## C) U1 (viewer on SET_A) — visibility and read access

9. Log in as `U1`: call `POST /api/v1/auth/login` with body
   `{"username": "e2e-perm-u1", "password": "TestPassw0rd!"}` and no prior
   cookie. Confirm the response is HTTP 200 and save the returned `session`
   cookie as `U1_COOKIE`. Do NOT mix `U1_COOKIE` with `ADMIN_COOKIE`.

10. As `U1`, list sets: call `GET /api/v1/sets` with `U1_COOKIE`. Confirm the
    response is HTTP 200. Expected behaviour (per `browseService.ListSets`):
    a non-admin user sees ONLY sets where they have an explicit permission
    entry. The returned array MUST contain an entry with `id == SET_A_ID` and
    MUST NOT contain any entry with `id == SET_B_ID`. If `SET_B` appears in
    the list, flag this as an authorization leak — the codebase filters by
    `ListPermissionsByUser` and never marks sets as "forbidden but visible".

11. As `U1`, browse `SET_A`: call `GET /api/v1/sets/{SET_A_ID}/browse` with
    `U1_COOKIE`. Confirm the response is HTTP 200 and the body contains a
    media list including `MEDIA_A_ID`.

12. As `U1`, browse `SET_B`: call `GET /api/v1/sets/{SET_B_ID}/browse` with
    `U1_COOKIE`. Confirm the response is HTTP 403 — `checkSetPermission`
    returns `ErrForbidden` for a non-admin without an entry, and `handleError`
    maps that to 403.

13. As `U1`, fetch a media item in the forbidden set: call
    `GET /api/v1/media/{MEDIA_B_ID}` with `U1_COOKIE`. Confirm the response
    is HTTP 403. Reasoning: `verifyAccess` finds the media, then calls
    `checkSetPermission` which returns `ErrForbidden`; the handler maps that
    to 403. If the response is 404 instead, that means the media was either
    soft-deleted between steps or the access helper hid existence on
    purpose — note which it is and flag the divergence from `verifyAccess`.

14. As `U1`, fetch a media item in the permitted set: call
    `GET /api/v1/media/{MEDIA_A_ID}` with `U1_COOKIE`. Confirm the response
    is HTTP 200 and the JSON has an `id` equal to `MEDIA_A_ID`.

## D) U1 — viewer write surface (verify actual behaviour)

15. As `U1`, add a tag to a media item in `SET_A`: call
    `POST /api/v1/media/{MEDIA_A_ID}/tags` with `U1_COOKIE`,
    `Content-Type: application/json` and body `{"tag": "e2e-perm-test"}`.
    Expected behaviour (per `tagService.AssignTag` which calls
    `verifyAccess`, not `verifyModifyAccess`): HTTP 200. If the response is
    403, the service has been tightened to require owner role for tag
    mutations — note this in the run output. If it is 200, leave the tag in
    place for now; step 21 cleans it up.

16. As `U1`, mark the media item as a favorite: call
    `POST /api/v1/media/{MEDIA_A_ID}/favorite` with `U1_COOKIE` and an
    appropriate JSON body if the handler requires one. Expected behaviour
    (per `favService` which uses `verifyAccess`): HTTP 200. Flag any 403 as
    a tightening of viewer privileges.

17. As `U1`, upsert a note on the media item: call
    `POST /api/v1/media/{MEDIA_A_ID}/notes` with `U1_COOKIE`,
    `Content-Type: application/json` and body `{"text": "viewer note"}`.
    Expected behaviour (per `noteService.UpsertNote` which uses
    `verifyAccess`): HTTP 200. Flag any 403 as a tightening of viewer
    privileges.

18. As `U1`, attempt to soft-delete the media item in `SET_A`: call
    `DELETE /api/v1/media/{MEDIA_A_ID}` with `U1_COOKIE`. Confirm the
    response is HTTP 403. Reasoning: `writeService.SoftDeleteMedia` calls
    `verifyModifyAccess`, which requires `RoleOwner` (or admin), and `U1`
    is a viewer.

## E) U2 (owner on SET_B) — modify access

19. Log in as `U2`: call `POST /api/v1/auth/login` with body
    `{"username": "e2e-perm-u2", "password": "TestPassw0rd!"}` and no prior
    cookie. Confirm the response is HTTP 200 and save the returned `session`
    cookie as `U2_COOKIE`.

20. As `U2`, soft-delete the media item in `SET_B`: call
    `DELETE /api/v1/media/{MEDIA_B_ID}` with `U2_COOKIE`. Confirm the
    response is HTTP 200. Reasoning: `verifyModifyAccess` accepts an owner
    role grant.

21. As `U2`, attempt to soft-delete the media item in `SET_A` (no
    permission): call `DELETE /api/v1/media/{MEDIA_A_ID}` with `U2_COOKIE`.
    Confirm the response is HTTP 403. Reasoning: `verifyAccess` is checked
    first and `U2` has no permission on `SET_A`, so `checkSetPermission`
    returns `ErrForbidden`.

22. As `U2`, attempt to browse `SET_A`: call
    `GET /api/v1/sets/{SET_A_ID}/browse` with `U2_COOKIE`. Confirm the
    response is HTTP 403.

## F) Cleanup

23. As admin, restore the media item deleted in step 20 so the test database
    is unchanged after this scenario: call
    `POST /api/v1/media/{MEDIA_B_ID}/restore` with `ADMIN_COOKIE`. Confirm
    the response is HTTP 200.

24. As admin, remove the tag added by `U1` in step 15 (only if step 15
    returned HTTP 200): call
    `DELETE /api/v1/media/{MEDIA_A_ID}/tags/e2e-perm-test` with
    `ADMIN_COOKIE`. Confirm the response is HTTP 200. If step 15 returned
    403 (viewer not allowed to tag), skip this step.

25. As admin, revoke `U1`'s grant on `SET_A`: call
    `DELETE /api/v1/admin/permissions` with `ADMIN_COOKIE` and body
    `{"user_id": <U1_ID>, "set_id": <SET_A_ID>}`. Confirm the response is
    HTTP 200 and the body is `{"status": "ok"}`.

26. As admin, revoke `U2`'s grant on `SET_B`: call
    `DELETE /api/v1/admin/permissions` with `ADMIN_COOKIE` and body
    `{"user_id": <U2_ID>, "set_id": <SET_B_ID>}`. Confirm the response is
    HTTP 200 and the body is `{"status": "ok"}`.

27. As admin, delete `U1`: call `DELETE /api/v1/admin/users/{U1_ID}` with
    `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the body is
    `{"status": "ok"}`.

28. As admin, delete `U2`: call `DELETE /api/v1/admin/users/{U2_ID}` with
    `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the body is
    `{"status": "ok"}`. Do NOT skip cleanup — leftover users and grants
    will pollute subsequent runs of S12, S13, S15 and re-runs of S19.
