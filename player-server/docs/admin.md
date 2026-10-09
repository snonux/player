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
with a hash.) A directory named `.thumbnails` directly in `MEDIA_ROOT` is
never scanned as a set.

Every thumbnail is generated into a temporary file (`.tmp-*.jpg` in the same
`.thumbnails` directory) and renamed into place once complete, by rescan,
upload, podcast download and "regenerate thumbnail" alike. A thumbnail in
place is therefore always complete. Should the server be killed mid-way, the
temporary file stays behind; a rescan deletes such files once they are more
than an hour old.

A thumbnail at its current path is deleted together with its media item
(when the trash is purged, when a podcast is unsubscribed), and its
`.thumbnails` directory once empty. Not deleted is a thumbnail still stored
under the old naming described below, because it may be shared with another
item: purging or unsubscribing such an item before the upgrade rescan has
migrated it leaves its old JPEG behind for good, and that file can keep an
otherwise empty podcast folder in place. Run the upgrade rescan first to
avoid this.

A video thumbnail is a frame from a random position, kept one second away
from the end of the video; if no frame can be had there (for instance under
an audio track that runs longer than the picture) the first frame is used.

A thumbnail that cannot be generated never blocks anything. Whether the file
comes from a rescan, an upload or a podcast download, it is stored and
indexed all the same: a video without a thumbnail, an image with the image
itself standing in, and the reason logged as a warning. That is also what
happens in a folder the server cannot write to.

A file must be what its extension says, broadly: audio and video files must
hold a real media container (AVI, ASF/WMV, FLV, Matroska/WebM, MP4/MOV, MP3,
Ogg, FLAC, WAV, AAC, MPEG-TS — a mislabeled one, such as an MP4 named
`.flv`, is fine), image files a JPEG, PNG, GIF, WebP, BMP, AVIF or SVG
image. Anything else is refused by the server's ffmpeg calls rather than
interpreted. In particular a playlist saved under a media name (an ffconcat
or HLS list called `clip.avi`), which ffmpeg would otherwise follow to
*other* files on the server, is not indexed by a rescan (a warning is
logged, the rest of the set is scanned normally, and the file is looked at
again on the next rescan) and is rejected as an upload. File names are taken
literally: a leading `-`, a `:` or a `%d`-style pattern (`a%03d.png`) is
just part of the name.

Every rescan then tries again, once, for each indexed video and image that
has no generated thumbnail, and regenerates any thumbnail whose file has
gone missing. The cost per rescan: one file check per indexed video and
image, plus one ffmpeg attempt for each file that still has no thumbnail
(none in a folder that is not writable, where it fails before ffmpeg is
started). A file ffmpeg cannot read at all therefore costs one failing
attempt on every rescan; for a video longer than a second that attempt is
two ffmpeg runs, the seeked one and the first-frame fallback.

#### Upgrading from v0.2.2 or older

Those releases named thumbnails after the stem only (`holiday.jpg`), and the
scanner kept all thumbnails of a set flat in the set's `.thumbnails`
directory. Files sharing a stem (`holiday.mp4` and `holiday.png`), or a name
in two folders of one set, overwrote each other's thumbnail.

After upgrading, **run one Rescan**. Until then nothing breaks: stored
thumbnail paths keep working, and affected cards keep showing the shared
thumbnail. The rescan gives every already indexed video and image whose
thumbnail is still at an old path a **newly generated** thumbnail at its new
path, then deletes the old file once no item refers to it any more. Old
thumbnails are not renamed or reused, because nothing records which of the
colliding files an old thumbnail shows.

What that rescan costs:

- one ffmpeg run per indexed video and image, in parallel on all CPU cores,
  so it takes about as long as the very first scan of the library did;
- video thumbnails are random frames, so videos get a different frame than
  before, including ones picked earlier with "regenerate thumbnail".

The rescan can be interrupted and repeated at any point (a second click on
Rescan, a restart, the 30-minute scan timeout): an item is switched only once
its new thumbnail is complete, anything not done yet keeps its old thumbnail,
and the next rescan continues where this one stopped. Each set with work to
do logs a line `scanner regenerating thumbnails` with three counts; the
migration is complete when `old_names` is 0 for every set, or the line is
absent (`missing` and `without` are the repairs described above and can
recur). An item whose thumbnail cannot be generated keeps its old one, is
counted in `old_names` and retried on every rescan, with the reason logged as
a warning. Only files inside the set directory are deleted.

Before that first rescan, uploading or regenerating a file can overwrite the
old thumbnail of another item in one rare case: the new file's thumbnail
name equals the other item's old one (`holiday.mp4` and an indexed
`holiday.mp4.png`, both mapping to `holiday.mp4.jpg`). The other item then
shows the wrong picture until the rescan, which fixes it.

"Regenerate thumbnail" on an item that still has an old path moves just that
item to its new path and deletes the old file unless another item still
uses it.

One race is left open while that rescan runs: if a file is uploaded during
the rescan and its thumbnail path is the old thumbnail of an item migrated
in the same rescan (the `holiday.mp4` / `holiday.mp4.png` pair again), the
upload's thumbnail is deleted as that item's old file. The next rescan
notices the missing thumbnail and regenerates it.

### Managing Trash

- `GET /api/admin/trash` — list soft-deleted media
- `DELETE /api/media/{id}` — soft-delete a media item
- `POST /api/media/{id}/restore` — restore a soft-deleted item

Soft-deleted media remains on disk until garbage collection removes it (see `GC_INTERVAL_MINUTES`).