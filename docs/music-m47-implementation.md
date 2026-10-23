# M47 collection pins — implementation design

Albums and playlists use the existing wrapping collection action row. A separate
**Pin offline…** button opens the collection's saved snapshot and status. This
extends U01's original Android storage proposal to all required platforms now
that M45 provides browser storage. Download ZIP remains an exported file action.

State: Not pinned → Loading songs → Saving N of total → Pinned N/total. Failed or
canceled transfers retain complete copies and offer Retry; no incomplete copy is
playable. Empty collections explain that no songs are available. Account changes
cancel writes. Errors explain access, connection or storage and remain retryable.
The dialog preserves the existing theme, native controls, scrolling long content,
keyboard Escape/focus return on web and touch/Back on Android; no new shortcuts.

Pinned snapshots survive disposable cache eviction. **Play downloaded** explicitly
plays saved audio through the existing queue owner. **Unpin** removes the pin and
keeps completed downloads. Browser **Remove download** confirms removal of this
collection snapshot and bytes unreferenced by other saved collections; Android
uses existing Downloads removal of individual files, with explicit local-only
copy. Pin state is derived from real complete files, not just a manifest flag.
Manual native downloads and overlapping collection files are preserved by unpin.

Browser storage is best effort: request persistent storage where supported and
show that browser/site-data cleanup or quota pressure can still remove copies.
Keep snapshots and complete bytes in an account-specific CacheStorage namespace
separate from streaming cache. Online playback revalidates access and rendition;
HTTP denials never authorize offline fallback. Native snapshots retain canonical
origin/account identities and use the existing download repository and offline
playback requests; credentials are never persisted in the snapshot.

Implementation-first stage: targeted coherence checks only. Full suites, final
builds, actual Chrome/Firefox and native emulator journeys (pin album/playlist,
restart, offline playback, cache clear/eviction, overlap, unpin/remove, access and
account changes, keyboard/touch/Back, light/dark/narrow) remain integrated gates.

Saved snapshots are also reachable in browser Settings → Saved music collections
(for the currently confirmed signed-in account) and native Downloads → Saved music
collections (retaining each stored origin/account). Browser cold offline sign-in
is not introduced: an unknown scope shows an honest online-sign-in requirement.
Native saved collections can play without a network session, like existing local
downloads; refresh is enabled only for the matching signed-in account.

Quality follows M51: browser pins use the existing Download quality selector;
native pins use Download quality in Settings. Each operation freezes that choice
before collection resolution. Original saves the authenticated original download;
other profiles save that exact rendition. Complete existing per-song downloads
retain their actual encoding even after preference changes or overlap. The pin
dialog reports recorded quality/MIME; older unrecorded copies are labeled honestly.
Explicit Play downloaded uses saved bytes under stream-only Wi-Fi restrictions.
Missing bytes or an online access/rendition refusal fail visibly rather than
turning this explicit local action into an audio network request.

R1 storage interleaving and refresh-removal blockers are fixed; narrow R2 confirmed both resolved with no new blocking defects (2/2 rounds used). Final native pin widget analysis passed after download-quality integration.
