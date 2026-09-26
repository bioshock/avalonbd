#!/usr/bin/env python3
"""Crop the storefront images out of the two Gemini asset sheets.

Re-runnable. Needs Pillow. Paths default to ~/Downloads; pass them to override:
    python3 scripts/slice-assets.py [SHEET1] [SHEET2]
Writes static/img/*.webp and scripts/seed/*.png (relative to avalonshop/).
"""
import sys
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parent.parent
DL = Path.home() / "Downloads"
S1 = Path(sys.argv[1]) if len(sys.argv) > 1 else DL / "Gemini_Generated_Image_hik8aphik8aphik8.jpeg"
S2 = Path(sys.argv[2]) if len(sys.argv) > 2 else DL / "Gemini_Generated_Image_8ga8vo8ga8vo8ga8.jpeg"
IMG = ROOT / "static" / "img"
SEED = ROOT / "scripts" / "seed"

# (left, top, right, bottom) in source pixels. Both sheets are 1684×2528.
SHEET1 = {  # product shots on flat white
    "onion": (150, 80, 480, 730),
    "garlic": (715, 80, 1045, 730),
    "ginger": (1265, 80, 1595, 730),
    "trio": (65, 855, 760, 1410),
    "plate": (780, 840, 1550, 1440),
    "leaf": (840, 1925, 1080, 2435),
}
SHEET2 = {  # scenes and textures
    "hero": (0, 624, 1684, 1656),
    "sunset": (0, 256, 1684, 512),
    "paper": (8, 1765, 820, 2520),
    "wood": (876, 1765, 1676, 2520),
}
KEY = (255, 0, 255)


def cutout(im, tol=40, feather=1.2):
    """Near-white background → transparent: flood fill from the crop edges
    (the sheet backgrounds are flat white), erode 1px, feather the edge."""
    rgb = im.convert("RGB")
    w, h = rgb.size
    edge = [(x, y) for x in range(0, w, 6) for y in (0, h - 1)] + [(x, y) for y in range(0, h, 6) for x in (0, w - 1)]
    for xy in edge:
        if min(rgb.getpixel(xy)) >= 255 - tol:
            ImageDraw.floodfill(rgb, xy, KEY, thresh=tol)
    diff = ImageChops.difference(rgb, Image.new("RGB", rgb.size, KEY)).convert("L")
    alpha = diff.point(lambda v: 0 if v == 0 else 255).filter(ImageFilter.MinFilter(3)).filter(ImageFilter.GaussianBlur(feather))
    out = im.convert("RGBA")
    out.putalpha(alpha)
    return out.crop(alpha.getbbox())


def fit(im, width):
    return im if im.width <= width else im.resize((width, round(im.height * width / im.width)), Image.LANCZOS)


def save(im, path, **kw):
    path.parent.mkdir(parents=True, exist_ok=True)
    im.save(path, **kw)
    print(f"{path.relative_to(ROOT)}  {im.width}x{im.height}  {path.stat().st_size // 1024} KB")


def main():
    s1, s2 = Image.open(S1), Image.open(S2)
    for size in (s1.size, s2.size):
        assert size == (1684, 2528), f"unexpected sheet size {size}"

    hero = s2.crop(SHEET2["hero"]).convert("RGB")
    sunset = s2.crop(SHEET2["sunset"]).convert("RGB")
    for w in (960, 1600):
        save(fit(hero, w), IMG / f"hero-{w}.webp", quality=78, method=6)
        save(fit(sunset, w), IMG / f"sunset-{w}.webp", quality=72, method=6)
    save(fit(s2.crop(SHEET2["paper"]).convert("RGB"), 600), IMG / "paper.webp", quality=70, method=6)
    save(fit(s2.crop(SHEET2["wood"]).convert("RGB"), 1000), IMG / "wood.webp", quality=68, method=6)

    save(fit(cutout(s1.crop(SHEET1["plate"])), 600), IMG / "plate.webp", quality=80, method=6)
    save(fit(cutout(s1.crop(SHEET1["leaf"])), 240), IMG / "leaf.webp", quality=80, method=6)
    for name in ("onion", "garlic", "ginger", "trio"):
        save(cutout(s1.crop(SHEET1[name])), SEED / f"{name}.png", optimize=True)


if __name__ == "__main__":
    main()
