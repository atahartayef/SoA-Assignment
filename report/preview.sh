#!/usr/bin/env bash
# Renders a .docx to PDF with headless LibreOffice.
# Used to measure the page each heading lands on, and to produce the PDF copy
# of the report.
# Usage: ./preview.sh input.docx output.pdf
set -euo pipefail
DOCX="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
PDF="$(mkdir -p "$(dirname "$2")" && cd "$(dirname "$2")" && pwd)/$(basename "$2")"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# LibreOffice swaps in Liberation fonts unless the real ones are in its profile,
# so give a throwaway profile copies of Times New Roman and Courier New.
FONTS="$WORK/profile/user/fonts"
mkdir -p "$FONTS"
cp "/System/Library/Fonts/Supplemental/Times New Roman"*.ttf \
   "/System/Library/Fonts/Supplemental/Courier New"*.ttf "$FONTS/"

soffice -env:UserInstallation="file://$WORK/profile" --headless \
  --convert-to pdf --outdir "$WORK" "$DOCX" >/dev/null 2>&1

OUT="$WORK/$(basename "${DOCX%.*}").pdf"
[ -s "$OUT" ] || { echo "preview failed: no PDF produced" >&2; exit 1; }
mv "$OUT" "$PDF"
echo "preview: $PDF"
