# M59 DLNA / UPnP output (al3)

Implementation reviewed in two bounded rounds. Real receiver and Chrome, Firefox and native Android
acceptance remain required; protocol tests are not platform evidence.
Prerequisite nj3/oj3/ck3/qk3 accepted implementations are reused.

## Interaction layout and states

Web: the current playback controls keep Cast where the Google SDK supports it;
**Play on…** beside it opens a native focus-trapped dialog on Chrome and Firefox.
The dialog reads `[Play on] [Selected: Kitchen] [status] [named speakers]
[Return to this device] [Search again] [Close]`. Only the selected row starts
remote playback. Search explains that discovery runs on the **server's** LAN.
Loading disables duplicate search. Empty and failed search keep Search again
and Close. Native dialog Tab/Shift-Tab/Enter/Escape retain their browser behavior;
closing restores the opener and existing library selection/vi keys remain.
Long device names wrap; the modal uses existing light/dark theme surfaces and
narrow viewport sizing. No new global shortcut is allocated.

Android: the existing output button and touch-first bottom sheet list both Cast
and DLNA speakers, each named and labeled by protocol. The current remote player
shows the selected name, play/pause, seek, previous/next for the renderer queue,
and **Return to this device**. The existing mini player also shows the remote device/title with play/pause, next and reopen controls after Back. Back dismisses the sheet/player while the existing
app-scoped remote playback controller retains playback ownership and progress.
The handback is paused at the latest observed remote position; it never starts a
second audible source automatically. Queue selection snapshots occurrences,
including duplicates, independently of subsequent browsing.

Transport states: local → choosing → loading → remote playing/paused → next queue
entry or return paused. A failed load explains network/format reachability.
Three failed status polls declare receiver loss. The web retains selected output
and its latest position so Play on offers reconnect or return; native follows
its existing remote-session handback and shows the connection failure. A renderer
STOPPED response only advances after observed playback near the track's end;
a refused URI or external stop is not treated as successful completion.

## Architecture and boundaries

The Go `internal/renderer` package uses SSDP MediaRenderer discovery and UPnP
AVTransport SOAP (SetAVTransportURI, Play, Pause, Seek, Stop, GetPositionInfo,
GetTransportInfo). Polling is specified by the
[UPnP AVTransport service](https://upnp.org/specs/av/UPnP-av-AVTransport-v2-Service.pdf).
No renderer library, browser network privilege or Android multicast permission
is added. Browsers and native app call the same authenticated server endpoints.

`GET /api[/v1]/renderers` returns only opaque IDs and friendly names. Discovery
reads a bounded number of datagrams/descriptions and has a bounded duration.
Description/control URLs must use numeric private LAN IPs over HTTP, match the
SSDP reply IP, and keep control on the description host. Hostnames, loopback,
link-local/cloud metadata addresses, redirects and HTTP proxies are refused.
This intentionally excludes devices advertising only hostnames, loopback,
link-local-only or HTTPS control addresses. The server needs multicast LAN
connectivity; NAT/container networks may require operator network configuration.

`POST /api[/v1]/renderers/control` accepts `{device_id, action, media_id?,
position?, session_id, revision}`. Each client captures a random output-session ID and advances its revision on load/stop; the server serializes the revision gate with the complete SOAP sequence and retains bounded stopped/retired-session guards. Pending loads own the client output immediately and cannot reclaim a newer local selection or reopen a stopped output. Actions are load/status/play/pause/seek/stop; arbitrary URLs and
SOAP commands are never accepted. A device is exclusively claimed by an account
until stop, with abandoned ownership reclaimable after two minutes without control/status calls. Media permission and audio content type are checked before load.
The speaker receives an expiring existing cast-ticket URL for exactly that media
and user, never cookies or bearer credentials; stream permission is checked again
on every receiver request. Signed media URLs use the server request's scheme and
Host; that address must be reachable from the receiver (localhost/emulator-only
addresses and HTTPS reverse proxies whose internal scheme differs require a
reachable deployment address). Original audio is served with its MIME type and
DIDL resource metadata; receiver codec support still applies. No silent arbitrary
URL proxy or automatic lossy conversion is introduced.

Web and native adapters reuse existing remote-output controls. The web queue's
normal completion/next path drives subsequent renderer loads. Native controller
snapshots requests/indices and advances on completion without releasing its
session lease, and provides previous/next controls. Progress uses captured
per-occurrence listening IDs, including explicit local/remote handoff IDs.

## Integrated verification still required

Use one disposable server/library/account and a real protocol-compatible local
renderer. Verify discovery, actual receiver media/audio, pause/play, seek,
multiple queue tracks including duplicate occurrences, manual next/previous,
receiver loss, reconnect/return paused, server/permission loss and sign-out on
installed Chrome, Firefox and native Android emulator. Check keyboard focus and
pointer alternatives, touch/Back, light/dark/narrow/long-name/empty/error states.
Do not accept mock SOAP assertions, manifest declarations or one browser as
proof of another platform or an actual receiver.

Focused Go protocol/API tests, renderer vet/errcheck, Node Cast/renderer tests,
scoped native analysis and renderer controller/mini/handback/logout checks pass.
Round 1 found reversed Seek SOAP arguments and a delayed completion callback
that could advance or stop a newer occurrence. Action-specific wire ordering
and exact session/occurrence/revision guards fix both; strict receiver and held
callback regressions pass. Round 2 found no remaining blocker in the fix.
The web Stop result also preserves uncertainty and supports an acknowledged
retry before automatic replacement.

An earlier affected audio-screen run had 21 passes and 31 failures. One existing
lyrics failure reproduces on isolated HEAD 8612150 with only three stale fake
method signatures corrected; its log is at
`/home/paul/tmp/al3-head-screen-baseline/baseline-lyrics.log`. The other 30 failures
remain unclassified and are retained for integrated verification. None of these
unit checks establishes actual receiver or platform acceptance.
