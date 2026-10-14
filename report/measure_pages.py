#!/usr/bin/env python3
"""Finds the page each contents entry lands on in a rendered PDF.

Writes toc_pages.json for build_report.py and reports whether the numbers
changed, so build.sh can rebuild until the contents page is correct.
"""
import json
import re
import sys
from pathlib import Path

import pypdf

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
import build_report  # noqa: E402  (reuses the same heading list)


def squash(text: str) -> str:
    return re.sub(r"\s+", "", text)


def main() -> int:
    pdf = Path(sys.argv[1])
    reader = pypdf.PdfReader(str(pdf))
    pages = [squash(page.extract_text() or "") for page in reader.pages]

    body = []
    for path in sorted(build_report.CONTENT.glob("0*.md")):
        body += build_report.parse_markdown(path.read_text(encoding="utf-8"))
    body += build_report.parse_markdown((build_report.CONTENT / "90-references.md").read_text(encoding="utf-8"))
    body += build_report.appendix_blocks()
    headings = [b.text for b in body if b.kind in ("h1", "h2")]

    contents_page = next(i for i, text in enumerate(pages) if squash("Table of Contents") in text)
    found, cursor = {}, contents_page + 1
    for heading in headings:
        target = squash(re.sub(r"[`*]", "", heading))
        for index in range(cursor, len(pages)):
            if target in pages[index]:
                found[heading] = index + 1
                cursor = index
                break

    missing = [h for h in headings if h not in found]
    previous = json.loads(build_report.TOC_PAGES.read_text()) if build_report.TOC_PAGES.exists() else {}
    build_report.TOC_PAGES.write_text(json.dumps(found, indent=2, ensure_ascii=False) + "\n")
    print(f"pages: {len(reader.pages)}; located {len(found)}/{len(headings)} headings"
          + (f"; missing: {missing}" if missing else ""))
    print("CHANGED" if found != previous else "STABLE")
    return 1 if missing else 0


if __name__ == "__main__":
    raise SystemExit(main())
