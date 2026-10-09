Admin Guide
===========

Admin endpoints are gated by `RequireAdmin` middleware (checks `users.is_admin`). The admin panel is opened via the "Admin" button in the SPA header (shown only when the current user is an admin).

### Bootstrap

On first visit (no users exist), you are redirected to `/bootstrap.html` to create the initial admin account.

### Creating Users

1. Open the admin panel.
2. Enter username, password, and check "Is admin" if desired.
3. Submit — the frontend calls `POST /api/admin/users`.
4. Admins cannot delete themselves via `DELETE /api/admin/users/:id`.

### Managing Set Permissions

- `GET /api/admin/permissions` — list permissions matrix
- `POST /api/admin/permissions` — grant access to a set (`body: { set_id, user_id, role: "owner" | "viewer" }`)
- `DELETE /api/admin/permissions` — revoke access (`body: { set_id, user_id }`)

Roles:
- `owner` — can upload to the set, soft-delete / restore media, regenerate thumbnails
- `viewer` — can browse and play media in the set

Admins implicitly see all sets without explicit permission rows.

### Rescanning the Library

Click **Rescan** in the admin panel, or call:

```bash
curl -X POST -b session=<cookie> http://<host>/api/admin/rescan
```

This triggers `FSScanner.Scan()`, which:
1. Scans immediate subdirectories of `MEDIA_ROOT` as **sets**
2. Recursively walks each set for supported media files
3. Probes new files with `ffprobe`
4. Generates thumbnails for video files
5. Inserts new records into the `media` table

### Thumbnail Naming and Upgrades

A generated thumbnail lives in a hidden `.thumbnails` directory next to its
source file and is named after the source's full name plus `.jpg`. Rescan,
upload and "regenerate thumbnail" all use this one layout, so every media
file has exactly one thumbnail of its own:

| Source | Thumbnail |
|--------|-----------|
| `set/holiday.mp4` | `set/.thumbnails/holiday.mp4.jpg` |
| `set/holiday.png` | `set/.thumbnails/holiday.png.jpg` |
| `set/a/clip.mp4` | `set/a/.thumbnails/clip.mp4.jpg` |

(A source name too long to take the extra `.jpg` is shortened and suffixed
with a hash.)

Releases up to v0.2.2 named thumbnails after the stem only (`holiday.jpg`),
and the scanner kept all thumbnails of a set flat in the set's `.thumbnails`
directory. Files sharing a stem (`holiday.mp4` and `holiday.png`), or a name
in two folders of one set, overwrote each other's thumbnail.

After upgrading, **run one Rescan**. Until then nothing breaks: stored
thumbnail paths keep working, and affected cards keep showing the shared
thumbnail. The rescan then handles every already indexed video and image
whose thumbnail is still at an old path:

- a thumbnail only one media item can have written is renamed to its new
  path (no re-encoding, a manually regenerated frame is kept);
- where several items could have written the same file, each of them gets a
  fresh thumbnail generated from its own source;
- an old thumbnail file is deleted once no item refers to it any more.

The migration shows up in the scan progress as additional files. It can be
interrupted and repeated at any point (a second click on Rescan, a restart,
the scan timeout): an item is only switched to a thumbnail that exists, and
an item that could not be migrated keeps its old thumbnail and is retried on
the next rescan, with the reason logged as a warning. Only files inside the
set directory are touched. Once everything is migrated, later rescans find
nothing to do.

"Regenerate thumbnail" on an item that still has an old path moves just that
item to its new path and deletes the old file unless another item still
uses it.

### Managing Trash

- `GET /api/admin/trash` — list soft-deleted media
- `DELETE /api/media/{id}` — soft-delete a media item
- `POST /api/media/{id}/restore` — restore a soft-deleted item

Soft-deleted media remains on disk until garbage collection removes it (see `GC_INTERVAL_MINUTES`).