#!/usr/bin/env bash
# Draws every diagram and renders it to a print-resolution PNG in diagrams/out/.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p out
python3 make_diagrams.py
for svg in src/*.svg; do
  rsvg-convert --zoom 3 --background-color white "$svg" -o "out/$(basename "${svg%.svg}").png"
done
for png in out/*.png; do
  printf '  %-22s %s\n' "$(basename "$png")" "$(sips -g pixelWidth -g pixelHeight "$png" | awk '/pixel/ {printf "%s ", $2}')"
done
