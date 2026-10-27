# Standalone podcast subscriptions (xh3)

Open **On this device → Podcasts on this device**. Subscribe with an RSS or
Atom feed URL, open a podcast to read its cached episode list, refresh an
individual feed or all subscriptions, or unsubscribe while keeping episode
metadata and listening history. A failed first fetch keeps the subscription
for retry. Publisher errors retain cached lists across restarts.

Feeds are fetched directly with an independent HTTP client that sends no
Player credentials. Fetches have a 30-second overall timeout, a 15-second
connection timeout, ten redirect hops and an 8 MiB document limit. Permanent
301/308 redirects and `itunes:new-feed-url` become bounded aliases; temporary
redirects retain the original fetch location. Refresh uses ETag and
Last-Modified validators. No operation requests a server audio download.

`podcasts.db` is separate from existing local files and server downloads.
Its local scope starts empty, leaving existing data intact. Subscription
registers retain unsubscribe tombstones and random pending edit IDs in the
same transaction. Account scopes use `account:<origin>#<user_id>`; device
catalog metadata is shared across scopes. Account association preserves
original clocks and local scope, merges position/completion separately, and
retains newer account edits. Signing in
does not associate local state automatically. Once the selected, unlocked
account advertises podcast synchronization, an existing separate device library
offers **Sync this device's podcasts with user@server?** Choose **Sync** to copy
its subscriptions and history into that account, or **Keep separate** (or Back)
to leave both libraries separate. The automatic question is shown once per
account. **Settings → Sync this device's podcasts** repeats a declined or
dismissed question. Already associated accounts are not merged again.
Copies on this device remain shared availability, never synchronized audio.
Changing server/account while consent or the merge is pending prevents the old
account's consent from copying data; the merge rolls back if its credential
generation changes before commit. Offline, locked and older-server accounts
remain usable without this optional prompt.

The provider selects local scope while browsing device files; server scope
remembers the last signed-in account identity for signed-out edits. It stores
no credential in podcast storage. The native Podcasts destination uses this
device library, including saved numeric podcast links. Server APIs and ordinary
server media browsing remain available for existing copies.

Open a cached podcast's episode list to **Download** publisher audio directly
onto this device (yh3). Pending/downloading copies offer **Cancel download**;
failed or interrupted copies offer **Retry download**. Available copies offer
**Play** and **Remove download**. Removing a copy stops its playback and removes
device bytes only; metadata, subscriptions and scoped listening edits remain.
Download again at any time. Copies survive restart without a Player connection
or login. Publisher access, supported audio formats and device storage are still
required. Downloads continue while browsing in the running app; process death
requires an explicit retry rather than falsely presenting a partial file.

The virtual **All podcast episodes** view and each podcast folder offer
**Uncompleted / Completed / All** filters, newest-first pages, saved progress,
and manual completion controls regardless of download availability (1i3).
Completion changes retain position and pending sync edits; **Play from
beginning** starts at zero without clearing history. Playback attempts scoped
synchronization for at most three seconds, then starts from reconciled or local
position. A position within five seconds of the loaded audio's end starts at
zero; replay keeps completion until a manual change. The shared audio handler
saves listening/seek/pause/background/stop progress and records completion only
on natural playback end, with one atomic end-position/completion edit before a
quiet detached sync. Late replies update storage without seeking live audio.
The local source never invokes authenticated music or lyric APIs. See the
[lifecycle notes](../../docs/podcast-playback-lifecycle-implementation.md) for
implementation-stage checks and integrated verification gaps.

Cheap checks: shared Go/Dart feed, episode, ingest and resolution vectors;
SQLite restart/tombstone/cache/account-isolation checks; per-register account
association and redirect-collision merge; direct HTTP permanent/temporary
redirects, conditional 304 and Atom parsing. Run from `player-android`:

```sh
flutter test --no-pub test/services/podcast_identity_test.dart test/services/podcast_repository_test.dart
```

Implementation-stage checks above are separate from the installed-client
acceptance recorded below.

Stage: **IMPLEMENTED/REVIEWED/VERIFIED** (2026-10-27). Both bounded
expert rounds completed. Established feed and episode aliases now survive
merges beyond new-alias acquisition caps; SQLite regressions cover alias
pressure, enclosure rotation, history and old-URL resubscription. Scoped
analysis and affected tests pass. Refresh-all retains independent publisher
failures and reports partial failure.

The retained 5i3 fixture at
`/home/paul/tmp/player-integrated-verify/podcast-5i3` verifies standalone
behavior in an installed Android A client. With the Player server connection
absent, the native Subscribe controls fetched two publisher feeds directly;
the aggregate list named the feed, downloaded audio bytes matched the
publisher's SHA-256, captured output contained the publisher tone, and a
process restart retained the uncompleted position and local copy. The native
account-association, Keep separate, other-account isolation, offline
unsubscribe/restart and explicit resubscribe stages passed. The two installed
browser journeys exercised the synchronized feed identities and subscription
views; installed Android B independently obtained its own publisher copy.
The `standalone`, `associate`, `isolation`, `unsubscribe`, `resubscribe`,
`download-negatives` and browser result files in that fixture retain the
per-stage evidence. Redirect, GUID collision and old-schema migration edges
were checked by focused SQLite/HTTP tests; they were not recreated through
each installed client's UI. No standalone podcast implementation changed
after the integrated run, so its passing journeys were reused for `xh3`.
