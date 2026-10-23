# M43 continuous related playback

Implementation prepared; awaiting bounded review and integrated verification.
M42 prerequisite `ql3` is implemented and reviewed in b605999; `ck3`, `nj3`
and `oj3` are accepted. Dependency links remain. Queue freeze from tl3
(`7f62601`) permits shared queue/panel hooks.

## Interaction design recorded before feature code

The existing Now Playing queue contains a labeled **Autoplay related music**
control beside repeat/shuffle on web, and a native switch row in the Android
queue sheet. The supporting text reads **After this queue ends · related songs
from your library**. It uses existing buttons, spacing and theme colors. Narrow
layouts wrap controls; long song titles retain the existing ellipsis/row layout.
The native row has the platform's generous switch tap target and semantics.
No new shortcut or dialog is introduced. Existing keyboard navigation, native
button activation, visible focus and queue close/Back behavior remain.

Autoplay starts Off for replacements, restored queues and account changes.
Enabling affects natural completion at the end of the selected queue only.
Repeat One and All take precedence. Manual Next at the end stays finite. Starting
an Instant Mix/artist radio replaces the queue and therefore starts Off. Explicit
Play next/Add to queue entries finish before automatic additions are considered.
Disabling invalidates pending suggestions without removing appended songs.

At natural completion, an enabled online queue shows **Finding related songs…**
without a modal. It requests M41's existing bounded pool of up to 12 song
suggestions for the finishing song, excludes all media already in this queue and
marks appended occurrences **Autoplay · related music**. No candidate produces
**No additional related suggestions.** This describes exhaustion of the bounded
candidate pool rather than a claim that the library contains no other match.
Offline playback ends normally with **Autoplay needs a connection.** Other
request failures keep the queue and show **Could not load related songs. Try
again when connected.** Normal explicit replay can retry at the next natural
completion; reconnect never starts audio by itself. Access errors never grant
cached permission. No platform permission is required.

Account/source changes, queue edits, disable, repeat changes and newer manual
transport intents discard late results. Pause/focus/sleep policies must win over
an in-flight lookup. Source metadata remains opaque to translation helpers.
Generated markers and enablement describe this running queue session: recovered
songs restore paused as normal occurrences with autoplay Off.

## Implementation notes

- Web: `queue-autoplay.js` policy + `queue.js` natural-end append via
  `API.relatedMusic('song', …)`; toggle and markers in `queue-panel.js`.
- Android: `MusicAutoplay` / `MusicAutoplayControl`; handler appends on natural
  end; persistence wires related fetch from the bound account API client.
- Offline/unavailable surfaces keep the control but mark connection required.

## Verification scope

Targeted checks:

- `node player-server/web/js/tests/queue-autoplay.test.js`
- `flutter test --no-pub test/services/music_autoplay_test.dart`
- Scoped `dart analyze` of autoplay service/control/queue sheet/handler/persistence
- `node --check` on `queue-autoplay.js`, `queue.js`, `queue-panel.js`

These checks do not exercise integrated queue hooks or actual platform playback.
Full suites, builds and Chrome/Firefox/native acceptance are deferred under the
implementation-first workflow. Integrated acceptance must exercise
enabled/disabled natural endings, repeat precedence, finite replacement Mix,
explicit queue edits and transport cancellation, no candidates, offline/error,
account/restart boundaries, keyboard/focus and native touch/Back.
