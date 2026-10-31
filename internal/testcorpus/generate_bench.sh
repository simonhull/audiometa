#!/usr/bin/env bash
# Regenerates testdata/bench/ fixtures for bench_parse_test.go. Requires ffmpeg (aac, libopus).
set -euo pipefail
OUT=testdata/bench
mkdir -p "$OUT"
META="$(mktemp)"
trap 'rm -f "$META"' EXIT
{
  echo ";FFMETADATA1"
  for kv in title=Book artist=Author album=Book album_artist=Author genre=Audiobook \
            composer=Narrator date=2020 comment=Comment description=Description \
            copyright=Copyright publisher=Publisher grouping="Series #1" show=Series \
            episode_id=1 track=1/1 disc=1/1 language=eng; do
    echo "$kv"
  done
  for i in $(seq 0 49); do
    printf '[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=Chapter %d\n' \
      $((i*200)) $((i*200+200)) "$i"
  done
} > "$META"
gen() { out=$1; shift
  ffmpeg -loglevel error -y -f lavfi -t 10 -i anullsrc=r=44100:cl=mono -i "$META" \
    -map 0:a -map_metadata 1 "$@" "$OUT/$out"; }
gen book.m4b  -c:a aac -b:a 16k -map_chapters 1
gen book.mp3  -b:a 32k -id3v2_version 3 -map_chapters 1
gen book.flac
gen book.opus -c:a libopus -map_chapters 1
ls -l "$OUT"
