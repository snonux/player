# Firefox audio investigation (qc3)

Investigated 2026-10-14 against `https://player.f3s.buetow.org`, deployment
image `registry.lan.buetow.org:30001/player:d1bd1e5`, runtime FFmpeg 6.1.2,
and local Linux Firefox 156.0 with an isolated headless profile.

## Finding

The reproduced silent videos are MPEG-TS files named `.mp4`. The server
correctly detects them and remuxes them to fragmented MP4, but writes the
initial MP4 header before the AAC bitstream filter has supplied the audio
initialization data. The resulting audio track has no AAC AudioSpecificConfig
(`ffprobe` reports no `extradata_size`). Firefox plays the video while ignoring
that audio track: `mozHasAudio=false`, with no media-element error.

This is a remux output problem, not missing audio in the source or a muted
player control. True MP4 files with the same H.264/AAC codecs decode audio in
the same Firefox installation.

## Evidence

Read-only `ffprobe` of all 117 `.mp4` files in `/media/Music Videos` found:

| Actual container | Files | Stream codecs |
| --- | ---: | --- |
| MPEG-TS | 63 | H.264 video, AAC audio |
| MP4/MOV family | 54 | H.264 video, AAC audio |

The live set API returned 118 media rows: those 117 videos had stored codec
`h264/aac`; one additional item was FLAC. Every row reported `transcoded=false`.
The web source selector therefore uses `/api/media/{id}/stream`. The server's
MPEG-TS sniff in `internal/service/streamer.go` remuxes the TS sources on this
route; the `/compat` transcode cache is not involved.

Direct playback of those actual stream URLs in an isolated same-origin video
element gave the following results after eight seconds:

| Track | ID | Container | Firefox audio | WebAudio RMS | Video |
| --- | ---: | --- | --- | ---: | --- |
| Britney Spears — Toxic (`00 - …`) | 3433 | MPEG-TS | `mozHasAudio=false` | 0 | 1920px wide, advancing |
| Dua Lipa — Houdini (`00 - …`) | 3444 | MPEG-TS | `mozHasAudio=false` | 0 | 1616px wide, advancing |
| Calvin Harris — Potion (folder item) | 3488 | MP4 | `mozHasAudio=true` | 0.168 | 1920px wide, advancing |
| Ciara — Ecstasy | 3489 | MP4 | `mozHasAudio=true` | 0.501 | 1440px wide, advancing |

All four had `readyState=4`, `paused=false`, `muted=false`, `volume=1`, an
AudioContext in `running` state, and no `MediaError`. RMS was measured from a
WebAudio analyser connected to the video element, so it verifies decoded audio
rather than relying on the host's speakers. Production logs confirmed the TS
tracks used the remux branch. The diagnostic page removed the app's UI before
playing, so it did not write progress or other user metadata.

A 1 MiB prefix read from Toxic's live stream answered `200`,
`Content-Type: video/mp4`. Its video track had 46 bytes of codec initialization
data; its AAC track had none. A twelve-second remux of the same original with
the production flags reproduced that missing audio initialization data.
Adding only `delay_moov` produced two bytes of AAC initialization data, hex
`1190` (AAC-LC, 48 kHz, stereo). No video/audio re-encoding was needed.

## Recommended fix

In `internal/probe/remux.go`, add `delay_moov` to the existing flags:

```text
-movflags frag_keyframe+empty_moov+default_base_moof+delay_moov
```

[FFmpeg documents `delay_moov`](https://www.ffmpeg.org/ffmpeg-formats.html)
as deferring the initial `moov` until the first fragment is cut/flushed. This
allows `aac_adtstoasc` to produce the audio configuration before the MP4 header
is emitted. [FFmpeg documents this filter](https://ffmpeg.org/ffmpeg-bitstream-filters.html#aac_005fadtstoasc)
as converting ADTS headers to MPEG-4 AudioSpecificConfig. Keep the existing
stream selection, input hardening, and bitstream filter. This preserves the source files and avoids a full-library transcode.

The isolated browser A/B below validates the proposed output in Firefox and
Chrome. Before shipping, update the argument assertion and add a real-FFmpeg
regression using generated synthetic MPEG-TS with ADTS AAC input, fragmented MP4 output, requiring nonempty AAC
initialization data. Keep live library media out of the repository.
Run the runtime-container FFmpeg tests as well as local tests. Startup may
wait until the first fragment is available; check that delay on representative
long-GOP input. Check playback beyond the first fragment and end of clip.

## Methods and limits

Authentication used the documented `test/e2e-web/live-foostore.sh` credential
convention, with the secret passed only to the diagnostic process. Browser
sessions and temporary cookie material are local diagnostic artifacts, not
repository content; temporary cookie material was deleted after the live checks.
Cluster inspection read deployment metadata, source files
and narrowly filtered remux logs; it did not change the deployment, accounts,
media files, library database, or initiate `/compat` cache jobs. Short remuxes
wrote to stdout and were captured locally, never into the production mount.

The container survey covers all 117 MP4 video files, but browser reproduction
covers two failing tracks and two controls, not all 63 TS files. The 63 files
are the affected-format population, not a claim that every one was individually
heard silent. The user's exact browser/profile/OS and perceived audio were not
inspected. The controlled Firefox result and missing initialization data explain
the reproduced failure independently of browser volume preferences.

Firefox's AAC support can depend on platform decoders, as described in
[Mozilla's audio codec guide](https://developer.mozilla.org/en-US/docs/Web/Media/Guides/Formats/Audio_codecs#AAC_(Advanced_Audio_Coding)).
That general limitation does not explain this sample: the MP4 controls decode
AAC successfully in the same Firefox instance.

## Candidate browser validation

Twelve-second clips produced with the deployed FFmpeg 6.1.2 were served only
from loopback, with identical original source, stream mapping and copy/filter
arguments. The sole A/B change was adding `delay_moov`. Fresh Firefox and
Chrome profiles played each clip with volume 1, unmuted, through a WebAudio
analyser connected to the output. Results sampled after five seconds:

| Browser | Remux flags | Audio indication | WebAudio RMS | Playback |
| --- | --- | --- | ---: | --- |
| Firefox 156.0 | Existing baseline | `mozHasAudio=false` | 0 | Video advances, no error |
| Firefox 156.0 | Baseline + `delay_moov` | `mozHasAudio=true` | 0.00453 | Audio/video advance, no error |
| Chrome 144.0.7559.132 | Existing baseline | Property unavailable | 0.000525 | Audio/video advance, no error |
| Chrome 144.0.7559.132 | Baseline + `delay_moov` | Property unavailable | 0.00190 | Audio/video advance, no error |

Both browsers reported `readyState=4`, 1920px video width and playback time
above 4.7 seconds for both variants. Absolute RMS differs with sample timing;
its relevant distinction is zero versus nonzero decoded output. Chrome's
baseline audio explains why this malformed remux can appear browser-specific.
Adding `delay_moov` restores both the missing AAC metadata and decoded audio in
Firefox while keeping Chrome playback working.

The browser A/B validates the first five seconds of one twelve-second clip,
not end-of-file, seeking, long-GOP startup latency or every affected source.
Those remain release checks. All owned browser processes and the temporary
loopback server were closed after measurement.

This investigation changes documentation only. The proposed fix has not been
deployed.
