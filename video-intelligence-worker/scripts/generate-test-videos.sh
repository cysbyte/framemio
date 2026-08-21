#!/usr/bin/env bash
set -euo pipefail

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg is required to generate video fixtures" >&2
  exit 1
fi

root_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_directory="${1:-${root_directory}/testdata/videos}"
video_codec="${VIDEO_FIXTURE_CODEC:-libx264}"
mkdir -p "${output_directory}"

ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "testsrc2=size=360x640:rate=30:duration=6" \
  -f lavfi -i "sine=frequency=440:sample_rate=48000:duration=6" \
  -c:v "${video_codec}" -preset veryfast -pix_fmt yuv420p \
  -c:a aac -shortest -movflags +faststart \
  "${output_directory}/short-portrait.mov"

ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "testsrc2=size=640x360:rate=60:duration=8" \
  -f lavfi -i "sine=frequency=660:sample_rate=48000:duration=8" \
  -vf "tmix=frames=2:weights='1 1'" \
  -c:v "${video_codec}" -preset veryfast -pix_fmt yuv420p \
  -c:a aac -shortest -movflags +faststart \
  "${output_directory}/high-fps-action.mov"

ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "color=c=0xD5B89A:size=640x360:rate=30:duration=5" \
  -f lavfi -i "testsrc2=size=640x360:rate=30:duration=7" \
  -f lavfi -i "color=c=0x355C7D:size=640x360:rate=30:duration=6" \
  -f lavfi -i "sine=frequency=330:sample_rate=48000:duration=18" \
  -filter_complex "[0:v][1:v][2:v]concat=n=3:v=1:a=0[v]" \
  -map "[v]" -map 3:a -c:v "${video_codec}" -preset veryfast -pix_fmt yuv420p \
  -c:a aac -shortest -movflags +faststart \
  "${output_directory}/long-trip-scenes.mov"

ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "color=c=0x7C746D:size=480x270:rate=24:duration=7" \
  -vf "noise=alls=3:allf=t" \
  -c:v "${video_codec}" -preset veryfast -pix_fmt yuv420p -an -movflags +faststart \
  "${output_directory}/low-quality-static.mov"

echo "Generated representative fixtures in ${output_directory}"
