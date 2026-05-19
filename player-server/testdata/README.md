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
