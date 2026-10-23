# Music M63: edit existing share links (7m3)

Implemented; awaiting bounded review and integrated verification. Prerequisite
ul3 supplies the reviewed collection share implementation (1e4a587); nj3/oj3
are accepted. Dependency links remain intact.

My Shares keeps its existing rows, copy and revoke actions and adds Edit. Web
editing stays inside the current modal: filename, prefilled Description and
Link expires (local time), then Cancel/Save. Native Android uses an AlertDialog
with a multiline field and date/time pickers. Saving disables controls; failure
keeps the draft with a connection/permission message. Cancel/Escape/Back returns
to management; web restores focus to that row's Edit button. Existing list
keyboard shortcuts remain available outside the editor, and typing/native
controls keep their keys. Canonical locale bindings preserve web drafts while
labels change; metadata remains opaque. Existing theme controls and spacing
carry light/dark styling; guest descriptions preserve line breaks and wrap.

PATCH edits only supplied description/expiry fields. Description is plain text,
at most 1000 Unicode characters; empty clears it. Description-only edits retain
the exact expiry, including already expired links. Changed expiry must be in
the future. Owner/admin authorization mirrors revoke. Token/URL, source snapshot,
creation time, maximum viewings, spent uses, download behavior and existing
viewing credentials remain intact. Shortened expiry applies to active viewings
immediately; extending a link never renews old credentials or resets spent uses.
Guest web/native views display the saved description. Migration retains legacy
links and descriptions through the collection-table rebuild.

Focused checks passed:

- `cd player-server && go test ./internal/service ./internal/repository ./internal/api -run 'ShareEdit|ShareCollectionMigration' -count=1`.
- `cd player-android && flutter test test/screens/edit_share_dialog_test.dart test/screens/my_shares_screen_test.dart`: 21 tests.
- Scoped `dart analyze` of the two API clients, Share model, edit/My Shares/guest screens, guest collection widget and new dialog test: no issues.
- Node `share-edit.test.js` and `i18n.test.js`, new web module syntax checks and `git diff --check`.

These checks cover token/viewing/limit preservation, owner/admin denial,
shortened expiry, expired description edits, strict PATCH fields, immutable
collection sources, failed-save drafts and cancel. The request bound was then
raised to 16 KiB to admit escaped Unicode descriptions; no protocol behavior
changed. The final web template/CSS locale adjustment reused focused Node checks.

Full regression suites, final server/APK builds and installed Chrome, Firefox
and native Android acceptance are deferred under the implementation-first
workflow. Required integrated journeys include edit then reopen the unchanged
URL as a guest; authoritative saved description/expiry and denied foreign edits;
active viewing expiry and spent limits; restart, offline/error recovery;
keyboard/focus and native touch/Back; light, dark and narrow layouts. No actual
platform acceptance, screenshots, audio proof, release or deployment is claimed.
