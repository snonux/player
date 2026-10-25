# Automatic offline music (M48 / sl3)

Implementation-stage contract, based on U01 (§3, §5, §9–11), M47 saved
collections and the reviewed zh3/4i3 global coordinator. Platform acceptance
is deferred under the implementation-first workflow; no platform is accepted
by this document.

## Annotated interaction

Settings gains one existing-style expandable **Automatic offline music** section:

```
Automatic offline music                                  [expand]
  [ ] Keep music current                  A: paused by default
  Keep: [ ] Pinned playlists              B: existing pinned snapshots
        [ ] Starred songs                 C: existing file/song hearts
        [ ] Newest 50 songs (recently added)
  Check every [1 hour / 6 hours / 24 hours] D: inexact interval, not time of day
  [x] Download on Wi-Fi only               E: independent copy policy
  What will be kept: chosen categories + current snapshot/copy counts
  Last attempt / last completed / next due F: account-scoped durable status
  [Sync now] [Cancel current run]          G: reuse global Sync now
  Retired copies stay in Downloads; remove explicitly to free capacity.
```

Web uses native labels/selects/buttons in the existing Settings modal, its
focus trap, Escape and opener restoration. No global shortcut is added. Narrow
screens stack the controls using existing appearance/playlist action classes;
colors/spacing inherit theme tokens. Android uses an ExpansionTile,
SwitchListTile/CheckboxListTile, dropdown and labeled 48dp buttons. Back closes
Settings and leaves a running job alive; Cancel stops only this automatic job.
Pause changes the durable policy and cancels work immediately. Empty selections
do nothing even when global Sync now is pressed. Cancel keeps completed files,
does not erase the prior successful snapshot, and defers the next scheduled
attempt until the selected interval. All policy changes invalidate the old run.

States: locked/signed-out explains explicit sign-in; loading disables policy
edits; paused/empty explains the next action; waiting for Wi-Fi offers Music
network settings (manual Wi-Fi confirmation in Firefox/unknown bearer);
fetching/saving reports quiet progress; failed access/storage/network preserves
the previous snapshot and offers global Retry; complete reports actual saved
counts. Long titles wrap/truncate with readable full labels. No modal/toast on
background failure. Existing global pending/stale/full-success feedback includes
this domain and must not report full success before downloads complete.

## Scheduling and credentials

Default policy is paused, categories empty, interval 6h and Wi-Fi-only. Policy,
attempt, success, due time and retained-copy ownership are scoped by canonical
server origin/account and survive restart. A failed attempt retries after the
interval or via global Retry; an overdue policy catches up at account unlock,
foreground and reconnect. Changes during any await invalidate both byte writes
and metadata publication. Native requests use per-request quiet Unauthorized
handling; background expiry cannot log out or stop live playback.

Web timers check while an open tab is visible, online and authenticated; browser
throttling can delay them. Closed/hidden tabs make no scheduling guarantee.
Unknown bearer is never assumed Wi-Fi. Existing Music network manual selection
supports Firefox and hotspots explicitly; no inference from effectiveType.

Android uses platform JobScheduler periodic jobs, inexact and subject to Doze,
battery/network constraints and OS cancellation. Jobs invoke only the existing
live Flutter engine. An absent engine or locked account defers work; no headless
engine reads saved credentials, no automatic unlock, and no new playback service
is created. Jobs may work while audio keeps that unlocked engine alive in the
background. OS stop cancels the exact invocation, with no late publication.
Foreground/explicit unlock catches up overdue work; process death keeps policy
and the previous committed snapshot, discarding partial files through existing
download recovery. OS scheduling failure is shown, not advertised as enabled.

## Membership, ownership and bounded retention

Pinned-playlist refresh resolves all selected pinned playlist details first.
Starred songs use the existing paginated file-favorites query intersected with
the authorized music catalog. Recently added means newest 50 accessible music
files by created_at then media ID, not recently played. Each chosen category
gets an ordinary saved collection snapshot; existing offline browsing/playback
reads it. Pinned playlist membership replaces its existing M47 snapshot only
after the entire fetch and all required downloads succeed. Empty successful
results replace membership with empty; partial fetch/pagination/access/account
failure does not reconcile anything. Removed/unpinned collection policies stop
automatic upkeep without deleting their manual snapshot or bytes.

Automatic operations reuse existing complete downloads, preserve recorded
quality and never claim a borrowed manual copy. New copies record automatic
ownership separately. Membership reconciliation never deletes bytes or personal
data: the current stores cannot prove every live playback/history/manual
reference. Retired copies therefore remain in existing Downloads/saved music,
with explicit cleanup guidance. At most 200 currently retained automatically
created song copies per account are allowed (active plus retired). Missing or
explicitly deleted copies free capacity; this is not a lifetime counter. A run
that would exceed capacity fails before downloading with cleanup guidance.
Manual copies and overlapping pins are outside this automatic ownership cap.
No history, bookmarks, notes, pending edits, other account data or playing bytes
are purged. Existing confirmed local removal retains its established semantics.

## Source evidence and checks

Pinned [DSub 5.7.5 settings_sync.xml](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/res/xml/settings_sync.xml)
offers interval, Wi-Fi, starred and most-recent policy; the exact Player recent
choice is explicitly recently added. [DSub offline service](https://github.com/paroj/DSub2000/blob/5.7.5/app/src/main/java/github/paroj/dsub2000/service/OfflineMusicService.java)
and [Tempo 3.9.0 preferences](https://github.com/CappielloAntonio/tempo/blob/3.9.0/app/src/main/res/xml/global_preferences.xml)
provide the offline/starred reference boundary, not code copied into Player.
Android scheduling follows the primary [JobScheduler](https://developer.android.com/reference/android/app/job/JobScheduler)
and [JobService](https://developer.android.com/reference/android/app/job/JobService)
contracts. No new scheduler dependency or saved credential store is introduced.

Focused checks must cover due/cancel/policy/account races, failed fetch retaining
snapshots, empty/removed membership, real saved bytes, manual overlap, capacity
and restart. Integrated gates still require Chrome, Firefox and native Android
actual scheduling/manual sync, Wi-Fi unknown/manual policy, offline playback,
restart/locked unlock, removal reconciliation, focus/touch/Back and light/dark/
narrow layout. No browser/emulator/full-suite/final-build evidence is claimed.

## Implementation handoff

Both clients now include the policy, global Sync now domain, authoritative
selection fetch and saved collection reconciliation. Android registers a
JobScheduler plugin and permission-protected JobService; it never launches a
headless Flutter engine. Automatic ownership is recorded in the same transaction
as a newly downloaded native copy. Browser automatic files remain reachable
through a retained-downloads collection even when a run is interrupted.

Cancellation invalidates transfer/publication leases. Native preference loading
uses a separate generation so a network change cannot strand Settings in its
loading state. A stopped OS invocation is remembered before its automatic domain
starts, preventing a late download after earlier global domains finish.
Unresolved automatic failures remain global failures during the interval wait;
skipping a not-due attempt never advances full-success status. Explicit Retry
can try immediately, and deliberate pause/policy change resets the failure.

Focused production-module checks exercise complete/empty/failed membership,
manual-overlap/retained-copy capacity, explicit removal freeing capacity,
account/cancel/network/unpin/storage boundaries, favorite pagination, recent
ordering, durable restart, native pending OS stop, loading cancellation and
full-success timestamps. These are coherence checks only. Full suites, final
builds and actual Chrome/Firefox/native Android journeys remain deferred to
integrated verification. No runtime acceptance is claimed.

R1 fixes: automatic category ownership now follows current membership, including
deselection. The retained-copy manager is rebuilt from automatic-owned copies
after cleanup so a previously protected active copy can later be reclaimed when
retired. Manual collection ownership is unchanged. Both clients persist an
unfinished-attempt checkpoint before starting work; a fresh process reports an
interrupted run and keeps global full-success unchanged during the interval
wait. Successful retry, handled failure/cancel or deliberate policy reset clears
the checkpoint. Focused regressions exercise real Remove download operations
for replace/deselect/overlap/capacity and fresh-instance restart/global status.
