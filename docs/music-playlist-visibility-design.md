# Playlist visibility and ownership (hk3 / M29)

Implementation layout follows U01's playlist cards, headers and existing edit dialogs.
Catalog card: artwork → name → song totals → `Private · Owner: alice` or
`Shared with server · Owner: alice`. Detail repeats that label directly under
the name, with `Read only` for other users. Play/search/queue remain available;
Add music/Add songs/Edit/Delete and row mutations require owner or administrator.
Sharing never grants access to a song's set; totals, artwork, entries and playback
resolve using the viewing account's existing media permissions.

Create/Edit dialog: Name → Comment → Visibility (Private / Shared with server)
→ “Shared playlists can be read by signed-in accounts on this server. This does
not create a public link or grant access to music.” → Cancel / Save.
Private is the creation default; existing documents migrate to private.
For administrators editing an existing document, Owner is a labeled account
selector; changing it requires explicit confirmation describing loss of previous
owner edit access. Owner/admin can edit; other signed-in accounts can read shared
lists. Private lists return 404 to other accounts; visible read-only mutations
return 403. Administrators can administer all documents.

States: loading uses current progress/status; no lists uses current create prompt;
a shared list with no caller-visible songs stays visibly labeled shared and empty.
Connection/save/permission errors stay in the dialog or existing error region,
retain typed values and offer retry; no offline writes are implied. Save disables
while pending. A transfer reloads authoritative owner/capabilities. Long labels
wrap in existing card/header/dialog layout; existing theme tokens cover dark/light.
Web uses native select/typing, existing modal focus and Escape, no new shortcuts;
read-only rows omit mutation actions (including keyboard-dispatched buttons).
Android uses dropdowns and labeled touch actions, native confirmation and Back.

Integrated acceptance is deferred under the implementation-first instruction:
Chrome, Firefox and native Android must each create private/shared lists as Alice,
read as Bob, deny Bob writes/private reads, transfer as administrator, verify old
owner loses edits, new owner gains edits, and restrict media membership/artwork/
totals to caller set ACL. Include empty list, long name, offline save, themes,
narrow layout, keyboard focus/typing/Escape and native touch/Back. No platform
acceptance or screenshots are claimed by this implementation note.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Expert
round one found no blocker; the minor empty read-only playlist focus fallback
now selects a visible control. Focused Go playlist/service/API permission tests,
scoped Flutter analysis, five existing widget tests and JS syntax checks pass.
Full gates, builds and actual platform journeys remain deferred until backlog
implementation is complete; task hk3 remains pending.
