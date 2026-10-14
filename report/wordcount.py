#!/usr/bin/env python3
"""Counts the assessed words in Sections 1-6, following the brief.

Counted: all answer text (headings, paragraphs and lists), including the board briefing (5.1)
and the Architecture Decision Records (5.3).
Not counted: the front page, contents, diagrams and their captions, tables, code listings,
screenshots, the reference list and the appendices.
"""
import re
import sys
from pathlib import Path

CONTENT = Path(__file__).parent / "content"
WORD = re.compile(r"[A-Za-z0-9€][\w'’\-–./()%€:]*")
LOW, HIGH = 3600, 4400  # 4,000 words ±10%
BRIEFING_LIMIT = 300


def assessed_text(source: str) -> str:
    text = re.sub(r"```.*?```", " ", source, flags=re.S)                    # code listings
    kept = []
    for line in text.splitlines():
        if line.startswith(("|", "Table: ", "Listing: ", "![", "@file ")):  # tables, captions, figures
            continue
        kept.append(line)
    text = "\n".join(kept).replace("`", "")
    return re.sub(r"[#>*|]", " ", text)


def count(text: str) -> int:
    return len(WORD.findall(text))


def section(source: str, heading: str) -> str:
    match = re.search(rf"^## {re.escape(heading)}.*?$(.*?)(?=^## |\Z)", source, flags=re.M | re.S)
    return match.group(1) if match else ""


def main() -> int:
    total = 0
    for path in sorted(CONTENT.glob("0*.md")):
        n = count(assessed_text(path.read_text(encoding="utf-8")))
        print(f"  {path.stem:<36}{n:>6}")
        total += n
    print(f"  {'TOTAL':<36}{total:>6}")
    status = "within" if LOW <= total <= HIGH else "OUTSIDE"
    print(f"  {status} the permitted range {LOW:,}–{HIGH:,}")
    briefing = count(assessed_text(section((CONTENT / "05-technical-leadership.md").read_text(encoding="utf-8"),
                                           "5.1")))
    print(f"  board briefing (5.1): {briefing} words (limit {BRIEFING_LIMIT})")
    print(f"TOTAL_WORDS={total}")
    print(f"BRIEFING_WORDS={briefing}")
    return 0 if briefing <= BRIEFING_LIMIT else 1


if __name__ == "__main__":
    sys.exit(main())
