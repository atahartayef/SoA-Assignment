#!/usr/bin/env bash
# Builds the Word report, renders it, and rebuilds until the contents page
# numbers match the rendered pages. The final render is kept as the PDF copy.
set -euo pipefail
cd "$(dirname "$0")"
PY="${PYTHON:-python3}"   # set PYTHON to use a particular interpreter
PREVIEW=../out/preview/MaltaShop_Microservices_Report.pdf
for pass in 1 2 3 4; do
  "$PY" build_report.py
  ./preview.sh ../out/MaltaShop_Microservices_Report.docx "$PREVIEW" >/dev/null
  result=$("$PY" measure_pages.py "$PREVIEW")
  echo "  pass $pass: $(echo "$result" | head -1)"
  if echo "$result" | grep -q STABLE; then
    echo "contents page numbers are stable"
    cp "$PREVIEW" ../out/MaltaShop_Microservices_Report.pdf
    echo "pdf: out/MaltaShop_Microservices_Report.pdf"
    exit 0
  fi
done
echo "contents page numbers did not converge" >&2
exit 1
