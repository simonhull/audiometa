#!/usr/bin/env sh
# Regenerates testdata/ fixtures. Requires ffmpeg (libmp3lame, aac, libvorbis,
# libopus) and flac on PATH. Run from repo root:
#   sh internal/testcorpus/generate.sh
set -eu
OUT=testdata
mkdir -p "$OUT"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FF="ffmpeg -hide_banner -loglevel error -y"

# 2 seconds of silence as the audio bed, and a tiny cover image.
SILENCE="-f lavfi -t 2 -i anullsrc=r=44100:cl=stereo"
$FF -f lavfi -i color=c=red:s=16x16 -frames:v 1 "$TMP/cover.jpg"

# Two chapters, in ffmetadata form (milliseconds).
cat >"$TMP/chapters.txt" <<'META'
;FFMETADATA1
[CHAPTER]
TIMEBASE=1/1000
START=0
END=1000
title=Chapter One
[CHAPTER]
TIMEBASE=1/1000
START=1000
END=2000
title=Chapter Two
META

# MP3, ID3v2.3, cover art and chapters.
# shellcheck disable=SC2046,SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -i "$TMP/chapters.txt" -map 0:a -map 1:v -map_metadata 2 \
	-c:a libmp3lame -c:v copy -id3v2_version 3 \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -metadata date="2021" \
	-metadata:s:v title="Cover" -metadata:s:v comment="Cover (front)" \
	"$OUT/sample.mp3"

# FLAC with Vorbis comments and a cover picture.
# shellcheck disable=SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -map 0:a -map 1:v -c:a flac -c:v copy \
	-disposition:v attached_pic \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample.flac"

# FLAC with canonical upper-case Vorbis comment names (native flac encoder).
$FF $SILENCE -c:a pcm_s16le "$TMP/silence.wav"
flac --silent --force \
	--tag="TITLE=Test Title" --tag="ARTIST=Test Artist" \
	--tag="ALBUM=Test Album" --tag="DATE=2021" \
	-o "$OUT/sample_upper.flac" "$TMP/silence.wav"

# M4B (AAC) with iTunes-style tags, cover and chapters.
# shellcheck disable=SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -i "$TMP/chapters.txt" -map 0:a -map 1:v -map_metadata 2 \
	-c:a aac -c:v copy -disposition:v attached_pic \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -f ipod "$OUT/sample.m4b"

# Plain M4A without chapters.
# shellcheck disable=SC2086
$FF $SILENCE -c:a aac \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample.m4a"

# Ogg Vorbis.
# shellcheck disable=SC2086
$FF $SILENCE -c:a libvorbis \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample.ogg"

# Opus.
# shellcheck disable=SC2086
$FF $SILENCE -c:a libopus \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample.opus"

# MP3, ID3v2.4 with an ID3v1 trailer, cover art (write-path coverage).
# shellcheck disable=SC2046,SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -map 0:a -map 1:v \
	-c:a libmp3lame -c:v copy -id3v2_version 4 -write_id3v1 1 \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -metadata date="2021" \
	-metadata:s:v title="Cover" -metadata:s:v comment="Cover (front)" \
	"$OUT/sample_v24.mp3"

# Faststart variants (moov before mdat) for write-path offset shifting.
# shellcheck disable=SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -i "$TMP/chapters.txt" -map 0:a -map 1:v -map_metadata 2 \
	-c:a aac -c:v copy -disposition:v attached_pic -movflags +faststart \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -f ipod "$OUT/sample_faststart.m4b"

# shellcheck disable=SC2086
$FF $SILENCE -c:a aac -movflags +faststart \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample_faststart.m4a"

echo "Generated:"
ls -l "$OUT"
