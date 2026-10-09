#!/usr/bin/env bash
#
# Generates the "all formats" test library used by the live-deployment e2e
# suites (test/e2e-web/tests/live-deployment.live.ts and
# player-android/test/e2e-live/). It writes one synthetic sample per extension
# the server accepts (see internal/mediatype/mediatype.go) into three sets:
#
#   <target>/test-videos/sample-<ext>.<ext>   8s test pattern with a timecode
#   <target>/test-audio/sample-<ext>.<ext>    12s sine tone
#   <target>/test-images/sample-<ext>.<ext>   800x600 still
#
# The extension is part of the stem on purpose: every file in a directory
# then has a unique name, so a client can address it by label and no two
# files can ever map to the same thumbnail name.
#
# The output is not committed: it is about 10 MB and fully reproducible.
#
# Usage: gen-all-formats.sh <target-directory>

set -euo pipefail

declare -r VIDEO_SECONDS=8
declare -r AUDIO_SECONDS=12

ffmpeg_quiet() {
    ffmpeg -hide_banner -loglevel error -y "$@"
}

# Encodes the moving test pattern plus a 440 Hz tone; the remaining arguments
# select codecs and the output file.
encode_video() {
    ffmpeg_quiet \
        -f lavfi -i 'testsrc2=size=640x360:rate=25' \
        -f lavfi -i 'sine=frequency=440:sample_rate=44100' \
        -t "$VIDEO_SECONDS" "$@"
}

encode_audio() {
    ffmpeg_quiet -f lavfi -i 'sine=frequency=523:sample_rate=44100' \
        -t "$AUDIO_SECONDS" -ac 2 "$@"
}

encode_still() {
    ffmpeg_quiet -f lavfi -i 'testsrc2=size=800x600' -frames:v 1 "$@"
}

generate_videos() {
    local -r dir=$1
    encode_video -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart "$dir/sample-mp4.mp4"
    encode_video -c:v libx264 -pix_fmt yuv420p -c:a aac "$dir/sample-mkv.mkv"
    encode_video -c:v libx264 -pix_fmt yuv420p -c:a aac "$dir/sample-mov.mov"
    encode_video -c:v mpeg4 -c:a libmp3lame "$dir/sample-avi.avi"
    encode_video -c:v wmv2 -c:a wmav2 "$dir/sample-wmv.wmv"
    encode_video -c:v flv -c:a libmp3lame -ar 44100 "$dir/sample-flv.flv"
    encode_video -c:v libvpx-vp9 -b:v 300k -c:a libopus -ar 48000 "$dir/sample-webm.webm"
}

generate_audio() {
    local -r dir=$1
    encode_audio -c:a libmp3lame "$dir/sample-mp3.mp3"
    encode_audio "$dir/sample-wav.wav"
    encode_audio -c:a flac "$dir/sample-flac.flac"
    encode_audio -c:a aac "$dir/sample-aac.aac"
    encode_audio -c:a libvorbis "$dir/sample-ogg.ogg"
    encode_audio -c:a aac "$dir/sample-m4a.m4a"
    # ffmpeg has no muxer registered for the .m4b/.wma names; name it.
    encode_audio -c:a aac -f mp4 "$dir/sample-m4b.m4b"
    encode_audio -c:a wmav2 -f asf "$dir/sample-wma.wma"
    encode_audio -ar 48000 -c:a libopus "$dir/sample-opus.opus"
}

write_svg() {
    cat > "$1" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" width="800" height="600" viewBox="0 0 800 600">
  <rect width="800" height="600" fill="#1b2a41"/>
  <circle cx="400" cy="300" r="180" fill="#5e9eff"/>
  <text x="400" y="320" font-size="64" text-anchor="middle" fill="#fff" font-family="sans-serif">SVG</text>
</svg>
SVG
}

generate_images() {
    local -r dir=$1
    local ext
    for ext in jpg png bmp webp; do
        encode_still "$dir/sample-$ext.$ext"
    done
    cp "$dir/sample-jpg.jpg" "$dir/sample-jpeg.jpeg"
    ffmpeg_quiet -f lavfi -i 'testsrc2=size=320x240:rate=10' -t 2 "$dir/sample-gif.gif"
    encode_still -c:v libaom-av1 -still-picture 1 "$dir/sample-avif.avif"
    write_svg "$dir/sample-svg.svg"
}

main() {
    if (( $# != 1 )); then
        echo "usage: ${0##*/} <target-directory>" >&2
        return 2
    fi
    if ! command -v ffmpeg > /dev/null; then
        echo "${0##*/}: ffmpeg is required" >&2
        return 1
    fi
    local -r target=$1
    mkdir -p "$target"/test-videos "$target"/test-audio "$target"/test-images
    generate_videos "$target/test-videos"
    generate_audio "$target/test-audio"
    generate_images "$target/test-images"
    find "$target" -type f -name 'sample-*' | sort
}

main "$@"
