# Independent podcasts and synchronized listening

Status: implementation and integrated verification in progress. The
[synchronization contract](podcast-sync-contract.md) is specified. Task `5i3`
remains open until every required browser and native boundary is verified.

## Goal

Subscribe to podcasts, choose episodes to download, listen with saved progress,
resume across Android clients and the web UI, and remove downloaded files after
listening without losing episode history. Android works independently of the
Player server; the server provides optional synchronization and the web UI.

## Browsing and episode actions

- Provide a folder per subscribed podcast and an **All podcast episodes** view.
  The combined view is virtual: files stay in their per-podcast folders, with
  no duplicate downloads. Show podcast names in the combined view and order
  episodes newest first.
- Both views default to **Uncompleted**, including started episodes. Offer
  **Uncompleted / Completed / All** filters.
- Every episode, downloaded or not, offers **Mark completed** or **Mark
  uncompleted**. Changes apply to both views and synchronize across clients.
- Save positions during playback, show a resume action and progress, and mark
  completion automatically when playback reaches the end. Seeking, pausing,
  stopping, or a playback error alone must not mark completion.
- Manual completion changes retain the saved position. Offer **Play from
  beginning** separately. Returning an episode to uncompleted restores its
  visibility in the default view; define end-position replay behavior in the
  contract task.
- Deleting a downloaded file is an explicit action, initially manual. Retain
  the episode, subscription, completion state, and saved history, and allow
  downloading again. This action is distinct from unsubscribing.

## Independent storage and downloads

| Concern | Android | Web UI / server |
| --- | --- | --- |
| Subscriptions and feed metadata | Stored locally; fetch feeds directly | Stored and fetched on the server |
| Episode audio | Download directly from the publisher onto the device | Explicit web download saves onto the server |
| Listening state | Persist locally, synchronize when possible | Persist on the server and participate in sync |
| Availability | Usable without a reachable server or active login | Requires the server |

Android subscription, download, and playback must not require server media IDs
or call the server's episode-download endpoint. Synchronizing a subscription or
listening history must never trigger a server audio download. Each Android
device and the server has its own download availability; bytes and download
flags are not replicated. Existing playback and download infrastructure should
be reused where compatible with these independent identities.

Deleting an Android copy affects only that device. Deleting a server copy can
affect other server listeners and must follow existing authorization and clearly
state its scope. Do not reuse the current destructive unsubscribe behavior for
ordinary file cleanup or subscription synchronization.

## Synchronization contract

The concrete identity, API, data and migration specification derived from
this section is [podcast-sync-contract.md](podcast-sync-contract.md) (task
`uh3`); it settles the decisions this section leaves open.

- Synchronize subscription additions and removals, manual and automatic
  completion changes, and positions of started, uncompleted podcast episodes.
- State is scoped to the selected server/account. Keep credentials, pending
  edits, and last-success information separate across accounts and servers.
  Standalone use remains available; the contract task must define how local
  subscriptions become associated with a connected account.
- Use stable feed and episode identities across clients, independent of local
  IDs and server media IDs. Define feed URL/redirect handling, episode GUID
  matching, and a fallback for feeds without reliable GUIDs.
- Merge changes in both directions, with durable pending changes and deletion
  records so offline clients cannot silently resurrect removed subscriptions.
  Define deterministic conflict handling, retry-safe writes, and migration of
  existing subscriptions and listening state before implementation.
- Reuse or extend existing playback synchronization instead of creating
  divergent progress histories for the same episode. Preserve existing users,
  permissions, media, history, and shares during migration; document any
  deliberate change to subscription ownership.
- Persist pending edits through app restarts. Retry failures without dropping
  local work or falsely treating a partial operation as a complete sync.
- Before each podcast playback starts, attempt sync with a short bounded wait,
  then choose the latest reconciled position and completion state. If sync is
  unavailable, play from local state. Late responses must not jump active
  playback or overwrite newer local playback edits.
- At episode end, save completion locally first and attempt sync immediately.
  A failed sync must not delay the next episode.
- Provide a global **Sync now** action that sends pending supported changes and
  retrieves updates. Include podcast subscriptions/status and existing
  syncable non-podcast progress and other already-supported sync data. This
  does not require adding synchronization for every currently local-only field.

## Quiet sync feedback

- Background failure produces a small non-blocking hint such as
  **Sync unavailable · changes saved on this device**, with **Retry**.
- No alarm sounds, blocking dialogs, repeated toasts, or notification spam.
  A publisher download error is a separate operation and may offer its own retry.
- After 48 hours without a fully successful sync, gently show the last-success
  age, for example **Last synced 2 days ago**. Explicitly represent never-synced
  state when synchronization is configured; standalone use must not be nagged
  to configure a server.
- Show sync activity, pending changes, and success without interrupting playback.
  Clear unavailable/stale feedback after full success. A partial failure must
  preserve pending edits and must not reset the full-success timestamp.
- Keep wording accurate: do not label authentication or other failures as a
  known server outage. The user preference is a humble hint, not an alarm.

## Delivery tasks

All tasks are managed with the agent-task-management skill through `ask`, from
the monorepo root under project `player`, tagged `podcasts`. Every task links
back to this plan. The dependency column describes delivery order, not platform
completion: the feature is complete only after the final verification task.

| Task alias | Scope | Depends on |
| --- | --- | --- |
| `uh3` | define independent episode identity and synchronization contract | — |
| `vh3` | implement account subscription sync and migrate existing feeds safely | `uh3` |
| `wh3` | unify episode completion and resume state with server playback sync | `uh3`, `vh3` |
| `xh3` | add standalone Android subscription storage and direct feed refresh | `uh3` |
| `yh3` | download audio directly onto Android and preserve history on cleanup | `xh3` |
| `zh3` | implement durable Android reconciliation and a global sync coordinator | `vh3`, `wh3`, `xh3` |
| `0i3` | add web subscription folders and an all-episodes view with completion filters | `vh3`, `wh3` |
| `1i3` | add standalone Android podcast folders, all episodes and manual completion | `xh3`, `yh3`, `zh3` |
| `2i3` | save Android listening state and sync before playback and at episode end | `yh3`, `zh3`, `1i3` |
| `3i3` | integrate web resume, end completion and safe server-file cleanup | `wh3`, `0i3` |
| `4i3` | expose global Sync now and quiet stale-sync feedback | `zh3`, `2i3`, `3i3` |
| `5i3` | verify independent downloads and seamless sync across Chrome, Firefox and two Android clients | `2i3`, `3i3`, `4i3` |

## Verification and acceptance

Use isolated fixture feeds and test libraries, never destructive tests against
production or personal media. Test with a real Google Chrome browser, Firefox,
and two Android client installations/devices/emulators. Browser and widget unit
tests alone do not verify native Android behavior.

1. Subscribe on Android without a reachable server, refresh its feed, download
   an episode directly, and listen. Confirm no server audio download or media
   import happened, including after subscription and progress synchronization.
2. Verify the per-podcast and combined views, their default completion filter,
   pagination, podcast labels, and manual status changes on all platforms.
3. Synchronize phone A's partial position, then resume on phone B and in each
   browser. Each platform obtains its own audio copy. Verify podcast progress
   when no corresponding server media row exists yet.
4. Finish an episode on phone B, synchronize, and confirm it is hidden by
   default everywhere. Mark it uncompleted manually and confirm it returns.
5. Delete a device/server download through its appropriate control; preserve
   history and other devices' downloads, and verify downloading again.
6. Exercise sync before playback, completion sync, and global retry for podcast
   changes and existing non-podcast progress. Verify bounded startup and no
   playback jump from a delayed response.
7. Exercise server outages, offline restart, partial failures, conflicting
   edits, subscription removal, server/account switching, and reconnection.
   Confirm durable pending edits, account isolation, and quiet feedback.
8. Exercise the 48-hour and never-synced states with a controlled clock; only
   full success clears stale status. Verify actual Android UI behavior as well
   as deterministic time tests.
9. Preserve web keyboard navigation/focus and Android labeled touch controls.
   Record evidence separately for Chrome, Firefox, and both Android clients;
   report every remaining verification gap explicitly.

Follow root and component `AGENTS.md` and component-local README commands.
Update the API and podcast documentation to match implemented behavior.

### Integrated verification snapshot (2026-10-26)

The isolated fixture is under `/home/paul/tmp/player-integrated-verify/podcast-5i3`;
each platform's `evidence/<platform>/<run>/results.json` records the stage
result. It uses two separate installed Android emulator clients, Chrome and
Firefox. Passing journeys so far:

| Platform | Passing stages |
| --- | --- |
| Android A | standalone direct publisher download/playback without server audio, explicit account association, observe completed and uncompleted, local delete/redownload, feed-folder and combined-view page 2 |
| Android B | cross-client audible resume and independent download, natural-end completion, observe completed, manual uncompletion, local delete/redownload |
| Chrome and Firefox | subscription catalog, shared-position audible resume, completion and uncompletion visibility, shared-server copy delete/redownload; non-podcast progress on global Retry, partial failure and exact 48-hour/never-synced feedback; bounded pre-playback fallback and late-response stability; conflicting subscription tombstones; genuine sync `401`, quiet sign-in, and narrow light/dark focus and target checks |

These stages establish the main four-client flow, including separate Android
files and retained history. They do not yet prove every acceptance boundary.
For example, the passing native runs include
`evidence/native-A/20261026T113434.160264Z-standalone/results.json`,
`evidence/native-A/20261026T120538.859369Z-associate/results.json`,
`evidence/native-A/20261026T143144.556471Z-browse/results.json`,
`evidence/native-B/20261026T124041.654457Z-resume/results.json`, and
`evidence/native-B/20261026T134658.547694Z-finish/results.json` under the
fixture directory above. The matching browser results are under
`evidence/{chrome,firefox}/`; passing stage directories end in `feedback`,
`boundaries`, `conflict`, and `auth-visual`. Later attempts do not erase failed
attempts, so use each stage's passing `results.json` and associated captures.

The native account-isolation journey proved a quiet never-synced state and
separate subscriptions on the second account, but its full-sync footer stayed
**Not synced yet** despite a server/database full-success record. A coordinator
status race was repaired in commit `01c527b`; the installed app still needs a
build with that repair and an actual rerun before the journey passes. Native
pre-playback started audible output in 5.48 seconds, but the expected scoped
sync request was absent, so bounded reconciliation and late-response safety
are **not** verified by that attempt. Native non-podcast global progress,
offline tombstone/restart/reconnect, quiet 48-hour and authentication feedback,
and the second client's tombstone observation still need actual checks. Browser
observation of the native unsubscribe remains pending, although browser-only
conflicting tombstones passed. Selected-server switching, native account
switching during a held response, failed end-sync with next-source playback,
and download cancel/seek/error negative journeys still need evidence. Record
their results here before closing `5i3`.

No release is requested by this plan. A later release must use the shared
version/bump workflow, build both components, and update the Android F-Droid
changelog.
