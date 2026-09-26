#!/usr/bin/env python3
"""Turn the Facebook logo SVG (a traced raster, 1254×1254 with a white
background) into static/logo.svg and static/favicon.svg.

    python3 scripts/clean-logo.py [SOURCE_SVG]

Steps: drop the background <rect> and every path whose fill is near-white
(all channels ≥ 0xF0); crop the viewBox to the drawing (Inkscape); svgo.
The favicon keeps only the green leaf paths above the wordmark.
Needs inkscape and npx (svgo) on PATH.

CSP note: style-src 'self' applies to /static, so the output must carry no
style="" attribute and no <style> block. fill-rule/clip-rule are set as
presentation attributes on the root <svg> instead of via a style attribute
(they are inheritable presentation attributes, so this has the same effect).
"""
import csv
import io
import subprocess
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = Path(sys.argv[1]) if len(sys.argv) > 1 else Path.home() / "Downloads" / "813244257_1454199756835428_2848094938538138542_n.svg"
NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)
TMP = ROOT / "data" / "logo-work"


def rgb(fill):
    fill = (fill or "").lstrip("#")
    if len(fill) != 6:
        return None
    return tuple(int(fill[i : i + 2], 16) for i in (0, 2, 4))


def near_white(fill):
    c = rgb(fill)
    return c is not None and min(c) >= 0xF0


# y (in the 1254-unit source coordinates) below which the AVALON/FOODS
# wordmark and its stem begin; tuned by eye against data/logo-work/favicon*.png.
LEAF_BOTTOM = 620


def is_leaf_fragment(fill, box):
    """True for a shading fragment of the two-leaf mark, false for the
    wordmark or the trace's soft halo washes.

    Color alone can't tell the leaf apart from "AVALON FOODS": both use the
    same green palette, including near-black greens in the leaf's own
    shadows. The wordmark and the halo washes are geometrically distinct
    instead — the wordmark spans nearly the full canvas width, and the
    halos are tall, pale, canvas-spanning blobs — so this filters on size
    and position, only using color to reject grays and to keep pale tones
    to small sheen highlights (real leaf sheen, not a wide pale wash)."""
    c = rgb(fill)
    if c is None:
        return False
    r, g, b = c
    if not (g >= r and g >= b and g > 40):
        return False
    x, y, w, h = box
    if y + h > LEAF_BOTTOM:
        return False
    if sum(c) > 500:
        return w < 100 and h < 150
    return w < 500 and h < 250


def run(*args):
    subprocess.run(args, check=True)


def finish(tree, out):
    raw = TMP / (out.stem + ".raw.svg")
    tree.write(raw, xml_declaration=False)
    cropped = TMP / (out.stem + ".crop.svg")
    run("inkscape", str(raw), "--export-area-drawing", "--export-plain-svg", f"--export-filename={cropped}")
    # precision 0: precision 1 put logo.svg over the 60 KB budget (108 KB);
    # precision 0 brings it to ~42 KB with no visible quality loss.
    run("npx", "--yes", "svgo", "--precision", "0", "--multipass", "-i", str(cropped), "-o", str(out))
    vb = ET.parse(out).getroot().get("viewBox")
    print(f"{out.relative_to(ROOT)}  viewBox={vb}  {out.stat().st_size // 1024} KB")


def main():
    TMP.mkdir(parents=True, exist_ok=True)
    tree = ET.parse(SRC)
    root = tree.getroot()
    for el in list(root):
        if el.tag == f"{{{NS}}}rect":
            root.remove(el)
    n = 0
    for g in root.iter(f"{{{NS}}}g"):
        for p in list(g):
            if p.tag == f"{{{NS}}}path" and near_white(p.get("fill")):
                g.remove(p)
            elif p.tag == f"{{{NS}}}path":
                p.set("id", f"p{n}")
                n += 1
    # Presentation attributes, not a style="" attribute: style-src 'self' CSP
    # forbids inline style on /static output.
    root.set("fill-rule", "evenodd")
    root.set("clip-rule", "evenodd")
    root.attrib.pop("style", None)
    for a in ("width", "height"):
        root.attrib.pop(a, None)
    root.set("viewBox", "0 0 1254 1254")
    finish(tree, ROOT / "static" / "logo.svg")

    # Favicon: only the two-leaf mark above the wordmark.
    raw = TMP / "ids.svg"
    tree.write(raw)
    boxes = {}
    q = subprocess.run(["inkscape", "--query-all", str(raw)], check=True, capture_output=True, text=True).stdout
    for row in csv.reader(io.StringIO(q)):
        if row and row[0].startswith("p"):
            x, y, w, h = map(float, row[1:5])
            boxes[row[0]] = (x, y, w, h)
    fav = ET.parse(raw)
    for g in fav.getroot().iter(f"{{{NS}}}g"):
        for p in list(g):
            box = boxes.get(p.get("id"))
            if p.tag == f"{{{NS}}}path" and not (box and is_leaf_fragment(p.get("fill"), box)):
                g.remove(p)
    finish(fav, ROOT / "static" / "favicon.svg")


if __name__ == "__main__":
    main()
