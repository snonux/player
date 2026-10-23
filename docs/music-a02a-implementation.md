# A02a server jukebox playback (`fl3`)

Optional server-host audio engine with permissioned remote controls. Ordinary
browser and Android streaming stay independent until the user explicitly chooses
**Play on… › This server's speakers**.

## Interaction

- Web **Play on…** lists **This server's speakers** first when
  `GET /api/config` advertises `jukebox.enabled` and `jukebox.can_control`
  (administrators only while `JUKEBOX_ENABLED=true`).
- Choosing that target pauses local playback and loads the current audio track
  on the server host. **Return to this device** stops server audio and hands
  back paused locally — never an automatic output switch.
- Android **Play on** shows the same labeled target with a distinct host-audio
  icon; transport uses the shared remote session path.
- Empty/error: disabled feature omits the row; non-admins never see it; denial
  and host-audio failures surface readable messages without claiming local play.

## Architecture

| Layer | Role |
| --- | --- |
| `internal/jukebox` | Engine + injectable `Adapter` (fake tests, bounded MPV) |
| `service.JukeboxService` | Admin gate, media path resolve via browse ACL |
| `GET/POST /api[/v1]/jukebox*` | Status and control (`load`/`play`/`pause`/`seek`/`stop`/`volume`/`set_device`/`set_queue`/`append`/`clear`/`status`) |
| Config | `JUKEBOX_ENABLED` (default false), `JUKEBOX_MPV_PATH` |

MPV is one idle process with UNIX IPC, confined library paths, and IPC
timeouts. Client queues remain local; the engine holds a separate server queue.

## Verification stage

Targeted Go tests use the fake adapter (permission, confine, queue, conflict).
**Deferred to integrated verification:** real MPV host audio, Chrome/Firefox
Play-on journeys, Android emulator output capture, and volume/output-device UI
polish beyond the labeled choice and status note.
