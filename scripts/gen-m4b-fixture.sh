#!/usr/bin/env bash
set -euo pipefail

OUT="internal/scanner/testdata/minimal.m4b"
mkdir -p "$(dirname "$OUT")"

# 1-second silence + ID3 metadata; smallest valid M4B we can craft.
# Requires ffmpeg locally — not a runtime dependency.
ffmpeg -y \
  -f lavfi -i anullsrc=channel_layout=mono:sample_rate=22050 -t 1 \
  -metadata title="Minimal Test Book" \
  -metadata artist="Test Narrator" \
  -metadata album_artist="Test Author" \
  -metadata album="Minimal Test Album" \
  -metadata date="2024" \
  -metadata genre="Science Fiction" \
  -c:a aac -b:a 16k -movflags +faststart \
  "$OUT"

echo "Generated: $OUT ($(du -h "$OUT" | cut -f1))"

# Chaptered fixture for chap-atom tests. ffmpeg embeds chapters from a
# metadata file. Two-step: first encode audio, then stamp chapters.
# (Combining -f lavfi audio + -i metadata without -map 0:a makes ffmpeg
# mux both inputs indefinitely — so we encode audio first, then remux.)
CHAP_OUT="internal/scanner/testdata/chaptered.m4b"
META="$(mktemp)"
TMP_AUDIO="$(mktemp --suffix=.m4b)"
cat > "$META" <<'EOF'
;FFMETADATA1
title=Chaptered Test
artist=Test Narrator
album_artist=Test Author
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
EOF

# Step 1: encode 2s of silence.
ffmpeg -y \
  -f lavfi -i anullsrc=channel_layout=mono:sample_rate=22050 -t 2 \
  -map 0:a -c:a aac -b:a 16k -movflags +faststart \
  "$TMP_AUDIO"

# Step 2: remux with chapter metadata.
ffmpeg -y \
  -i "$TMP_AUDIO" -i "$META" \
  -map 0 -map_metadata 1 -c copy \
  "$CHAP_OUT"

rm -f "$META" "$TMP_AUDIO"
echo "Generated: $CHAP_OUT"

# MP3 fixture for ParseMP3 tests. 1-second MP3 silence + ID3v2 tags.
MP3_OUT="internal/scanner/testdata/minimal.mp3"
ffmpeg -y \
  -f lavfi -i anullsrc=channel_layout=mono:sample_rate=22050 -t 1 \
  -metadata title="Minimal MP3 Test" \
  -metadata artist="Test Narrator" \
  -metadata album_artist="Test Author" \
  -metadata album="Minimal MP3 Album" \
  -metadata date="2024" \
  -metadata genre="Science Fiction" \
  -c:a libmp3lame -b:a 32k -id3v2_version 3 \
  "$MP3_OUT"

echo "Generated: $MP3_OUT"
