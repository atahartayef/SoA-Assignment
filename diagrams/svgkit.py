#!/usr/bin/env python3
"""Small SVG toolkit shared by this submission's figures.

Everything is drawn at a design width of 880 units, which the report scales to
the full text width, so a font size of 14 here prints at roughly 7.5 pt.
"""
from pathlib import Path

OUT = Path(__file__).parent / "src"

FONT = "Helvetica, Arial, sans-serif"
INK = "#23201f"
GREY = "#75706c"
LIGHT = "#f4f2ef"
WHITE = "#ffffff"
ACCENT = "#8a2846"       # checkout-critical, gates, compensation
AMBER = "#1f6f5c"        # message broker and events
AMBER_FILL = "#e7f3f0"
BLUE = "#a2560d"         # observability and prototype marks
BLUE_FILL = "#fbf0e4"
OLIVE = "#55611f"
OLIVE_FILL = "#f0f3e4"
COLOURS = {"ink": INK, "grey": GREY, "accent": ACCENT, "amber": AMBER, "blue": BLUE, "olive": OLIVE}


# ------------------------------------------------------------------ primitives

def svg(width, height, body):
    markers = "".join(
        f'<marker id="arrow-{name}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" '
        f'markerHeight="7" orient="auto-start-reverse"><path d="M0,1 L9,5 L0,9 z" fill="{colour}"/></marker>'
        for name, colour in COLOURS.items())
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" '
            f'viewBox="0 0 {width} {height}" font-family="{FONT}">\n<defs>{markers}</defs>\n'
            f'<rect width="{width}" height="{height}" fill="white"/>\n{body}\n</svg>\n')


def esc(content):
    return str(content).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def text(x, y, content, size=14, anchor="middle", colour=INK, bold=False, italic=False):
    style = (' font-weight="bold"' if bold else "") + (' font-style="italic"' if italic else "")
    return (f'<text x="{x}" y="{y}" font-size="{size}" text-anchor="{anchor}" fill="{colour}"{style}>'
            f'{esc(content)}</text>')


def text_lines(x, y, rows, size=13.5, gap=17, **kw):
    return "\n".join(text(x, y + i * gap, row, size=size, **kw) for i, row in enumerate(rows))


def rect(x, y, w, h, fill=LIGHT, stroke=INK, width=1.3, rx=2, dashed=False):
    dash = ' stroke-dasharray="6 4"' if dashed else ""
    return (f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" stroke="{stroke}" '
            f'stroke-width="{width}"{dash}/>')


def box(x, y, w, h, title, rows=(), fill=LIGHT, stroke=INK, width=1.4, title_size=15, size=13.5,
        dashed=False, title_colour=INK, top=None):
    """A rounded box with a bold title and centred lines under it."""
    parts = [rect(x, y, w, h, fill, stroke, width, dashed=dashed)]
    block = 18 + len(rows) * 17
    first = top if top is not None else y + (h - block) / 2 + 14
    parts.append(text(x + w / 2, first, title, size=title_size, bold=True, colour=title_colour))
    parts.append(text_lines(x + w / 2, first + 19, rows, size=size))
    return "\n".join(parts)


def arrow(x1, y1, x2, y2, colour="ink", dashed=False, both=False, width=1.5):
    dash = ' stroke-dasharray="6 4"' if dashed else ""
    start = f' marker-start="url(#arrow-{colour})"' if both else ""
    return (f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="{COLOURS[colour]}" stroke-width="{width}"'
            f'{dash}{start} marker-end="url(#arrow-{colour})"/>')


def polyline(points, colour="ink", dashed=False, end_arrow=True, width=1.5):
    dash = ' stroke-dasharray="6 4"' if dashed else ""
    marker = f' marker-end="url(#arrow-{colour})"' if end_arrow else ""
    pts = " ".join(f"{x},{y}" for x, y in points)
    return (f'<polyline points="{pts}" fill="none" stroke="{COLOURS[colour]}" stroke-width="{width}"'
            f'{dash}{marker}/>')


def line(x1, y1, x2, y2, colour=INK, width=1.2, dashed=False):
    dash = ' stroke-dasharray="6 4"' if dashed else ""
    return f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="{colour}" stroke-width="{width}"{dash}/>'


def cylinder(x, y, w=18, h=22, colour=INK):
    """A small database symbol with its top-left corner at (x, y)."""
    ry = 4
    return (f'<path d="M{x},{y + ry} v{h - 2 * ry} a{w / 2},{ry} 0 0 0 {w},0 v{-(h - 2 * ry)}" '
            f'fill="white" stroke="{colour}" stroke-width="1.2"/>'
            f'<ellipse cx="{x + w / 2}" cy="{y + ry}" rx="{w / 2}" ry="{ry}" fill="white" stroke="{colour}" '
            f'stroke-width="1.2"/>')


def service(x, y, w, h, name, stores, critical=False, extra=None):
    """A service box: name on top, its own data store(s) in a lower compartment."""
    stroke = ACCENT if critical else INK
    parts = [rect(x, y, w, h, WHITE, stroke, 2.0 if critical else 1.4)]
    parts.append(text(x + w / 2, y + 22, name, size=15, bold=True, colour=stroke))
    if extra:
        parts.append(text(x + w / 2, y + 38, extra, size=12.5, colour=GREY, italic=True))
    divider = y + (46 if extra else 32)
    parts.append(line(x, divider, x + w, divider, colour=stroke, width=1.0))
    parts.append(cylinder(x + 10, divider + 8))
    for i, store in enumerate(stores):
        parts.append(text(x + 36, divider + 22 + i * 16, store, size=13, anchor="start"))
    return "\n".join(parts)


