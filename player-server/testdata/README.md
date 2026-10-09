# Test data

This directory contains the small, license-cleared media library used by
the automated test suites (Playwright `e2e-web/`, LLM `e2e-llm/`, and the
Mage `E2E` target). It is committed to the repository so anyone can clone
and run the suites without an external download step.

See [`LICENSES.md`](./LICENSES.md) for the source URL and license of
every file.

## Layout

```
testdata/
├── LICENSES.md
├── README.md
└── media/
    ├── audiobooks/aesops-fables/   five LibriVox public-domain mp3 chapters
    ├── images/                     four NASA public-domain jpgs
    └── videos/                     one NASA public-domain mp4 short
```

Each top-level directory under `media/` becomes a distinct *set* when the
server scans the library, so the suites exercise multi-set browsing,
per-set permissions, and cover regeneration without needing fixtures
elsewhere.

## Pointing the server at this directory

The server resolves `MEDIA_ROOT` relative to the working directory it
was started from. Start it from `player-server/` so the relative path
works:

```sh
cd player-server
MEDIA_ROOT=./testdata/media \
  SECURE_COOKIES=false \
  DB_PATH=/tmp/player-e2e-llm.db \
  ./player
```

Override with an absolute path if you want to run against a different
library (your own media collection lives outside the repository).

## All-formats library for live-deployment tests

The committed fixtures cover only mp3, jpg and mp4. The live-deployment
suites (`test/e2e-web/tests/live-deployment.live.ts` and
`player-android/test/e2e-live/`) need one file per extension the server
accepts. That library is generated, not committed:

```sh
./gen-all-formats.sh /tmp/all-formats
```

It writes `test-videos/`, `test-audio/` and `test-images/`, each holding
`sample-<ext>.<ext>` files made with ffmpeg's synthetic sources (about 10 MB
in total). Copy the three directories into the `MEDIA_ROOT` of the instance
under test and trigger a rescan. When `internal/mediatype/mediatype.go` gains
an extension, add it to the script and to the format lists in both suites.

## Adding more fixture files

1. Pick a file whose license permits redistribution (public domain,
   CC0, or a permissive Creative Commons variant). When in doubt,
   prefer NASA, LibriVox, or Wikimedia Commons CC0.
2. Keep individual files small (a few MB) so the repository stays
   lightweight. The whole `testdata/` tree should remain on the order
   of ~10–20 MB.
3. Drop the file into the appropriate `media/<set>/` directory and add
   an entry to `LICENSES.md` with the source URL and license.
4. If a test was written against the old fixture set, update its
   assertions to match the new file count or names.
