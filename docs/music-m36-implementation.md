Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; integrated acceptance remains pending.

# M36 personal listening details (mk3)

Implementation stage: awaiting bounded review and integrated verification.
Prerequisites nj3, oj3, yj3 and zj3 provide the interaction contract, shared
fixtures, permission-filtered albums and structured artist roles.

The existing Music screens gain secondary text beneath the established song
credit or collection totals: `Your plays: 2 · Last played <local date/time>`.
Album and artist detail totals stay above their existing Play controls. Zero
plays say `Your plays: 0 · Not played yet`. Search rows use the same language.
Music Info identifies personal plays separately from the global file counter.
This is passive text: keyboard selection, Enter, Back, menus and touch targets
stay with the existing controls. Native text wraps and follows the current
body-small theme; web uses existing muted small text tokens. Catalog loading,
empty, permission and Retry states continue to govern these reads; a failed
request never substitutes another account's counters. Returning/reloading the
catalog fetches updated figures. Offline events appear in the catalog after
reconnection and synchronization; saved download metadata is not a live count.

Personal qualification uses the established 60-second threshold and maximum
12 seconds of positive position movement per save. It is independent for each
account/session/media identity, including when callers reuse a session string.
Qualification and the existing global counter/progress update share a database
transaction. Dates reflect qualification time, never a bookmark, resume edit,
manual completion, or reconnect time. Existing global counter behavior remains
unchanged. Personal qualified sessions survive logout/reset to prevent recounting
retries; a fresh session must qualify independently. Preexisting global counts
cannot be attributed to users and are not backfilled.

Android downloaded-file playback records capped position deltas per playback
request. At qualification it retains one random event ID and original time in
the account/media-owned sync store. Removal of downloaded bytes retains pending
events. Playback-sync accepts up to 100 events, inserts each user/media/event
once atomically with resume/bookmarks, and returns exact acknowledged IDs.
A late acknowledgement cannot clear another account/media's event. No play is
inferred from a final resume position. Podcast and M4B files do not contribute
to personal music counters. General media/podcast resume contracts are retained.

Catalog data is enriched after current access checks. Album counts sum visible
songs; artist totals count each credited visible song once across multiple roles.
Metadata filtering recomputes album totals from matching songs. Deleting a user
or media item removes the corresponding listening rows.

Focused checks before offline additions: Go service/repository/API compilation
and selected music catalog/listening tests passed. Exact final focused checks and
fresh review findings are recorded in task annotations. Full regression suites,
server/APK builds, actual Chrome/Firefox/native Android threshold/skip, two-account,
restart/offline, keyboard/touch, light/dark/narrow visual checks are deferred to
the integrated verification phase. This feature is not accepted yet.
