#!/usr/bin/env python3
"""Generate the unified NP4 app icon: emerald shield with onion-layer arcs
on the deep-space dark rounded square — the same visual language as the
clients' UIs. Outputs every size both clients and the dashboard need.

Usage: python3 clients/tools/gen_icon.py   (requires Pillow)
"""
import math
import os
from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SIZE = 1024
BG = (10, 15, 28)          # #0A0F1C deep-space navy
ACCENT = (52, 211, 153)    # #34D399 emerald
ACCENT_DIM = (18, 48, 31)  # #12301F accent container
GLOW = (52, 211, 153)


def shield_pts(cx, cy, w, h, steps=72):
    """Smooth shield silhouette: elliptical top, quadratic sides to a tip."""
    top_y = cy - h * 0.30
    ry = h * 0.30
    pts = []
    for i in range(steps + 1):
        a = math.pi + i / steps * math.pi
        pts.append((cx + w * math.cos(a), top_y + ry * math.sin(a)))

    def quad(p0, p1, p2, n=28):
        return [((1 - t) ** 2 * p0[0] + 2 * (1 - t) * t * p1[0] + t ** 2 * p2[0],
                 (1 - t) ** 2 * p0[1] + 2 * (1 - t) * t * p1[1] + t ** 2 * p2[1])
                for t in (i / n for i in range(n + 1))]

    tip = (cx, cy + h * 0.46)
    pts += quad((cx + w, top_y), (cx + w * 0.80, cy + h * 0.20), tip)[1:]
    pts += quad(tip, (cx - w * 0.80, cy + h * 0.20), (cx - w, top_y))[1:]
    return pts


def rounded_square(size, radius):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=255)
    bg = Image.new("RGBA", (size, size), BG + (255,))
    # Soft emerald glow from the top edge, like the clients' screens.
    glow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse([-size * 0.3, -size * 0.55, size * 1.3, size * 0.45], fill=GLOW + (26,))
    glow = glow.filter(ImageFilter.GaussianBlur(size * 0.08))
    bg.alpha_composite(glow)
    img.paste(bg, (0, 0), mask)
    return img, mask


def draw_master():
    img, square_mask = rounded_square(SIZE, 232)
    cx, cy = SIZE / 2, SIZE / 2
    w, h = SIZE * 0.245, SIZE * 0.66

    # Outer stroke: a slightly larger shield in solid emerald behind the fill.
    stroke = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    ImageDraw.Draw(stroke).polygon(shield_pts(cx, cy, w * 1.055, h * 1.05), fill=ACCENT + (255,))
    img.alpha_composite(stroke)

    # Shield fill (dark accent container).
    fill = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    ImageDraw.Draw(fill).polygon(shield_pts(cx, cy, w, h), fill=ACCENT_DIM + (255,))
    img.alpha_composite(fill)

    # Onion layers: concentric arcs inside the shield, fading outward-in.
    inner_mask = Image.new("L", (SIZE, SIZE), 0)
    ImageDraw.Draw(inner_mask).polygon(shield_pts(cx, cy, w * 0.94, h * 0.94), fill=255)
    layers = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    ld = ImageDraw.Draw(layers)
    layer_cy = cy - h * 0.10
    for r, width, alpha in ((SIZE * 0.245, 30, 255), (SIZE * 0.183, 24, 205), (SIZE * 0.124, 18, 150)):
        ld.arc([cx - r, layer_cy - r, cx + r, layer_cy + r],
               start=-58, end=238, fill=ACCENT + (alpha,), width=width)
    # Center point of the onion.
    r0 = SIZE * 0.030
    ld.ellipse([cx - r0, layer_cy - r0, cx + r0, layer_cy + r0], fill=ACCENT + (255,))
    img.alpha_composite(layers)

    # Everything stays inside the rounded square.
    out = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    out.paste(img, (0, 0), square_mask)
    return out


def main():
    master = draw_master()
    out_dir = os.path.join(ROOT, "assets")
    os.makedirs(out_dir, exist_ok=True)
    # Flutter launcher icon source (square, opaque background handled by the
    # adaptive-icon background color).
    master.save(os.path.join(ROOT, "assets", "icon-1024.png"))
    # Transparent rounded-square master for the dashboard favicon.
    for name, size in (("icon-256.png", 256), ("icon-64.png", 64), ("icon-32.png", 32)):
        master.resize((size, size), Image.LANCZOS).save(os.path.join(out_dir, name))
    # PyQt window icon.
    pyqt_assets = os.path.join(ROOT, "pyqt", "assets")
    os.makedirs(pyqt_assets, exist_ok=True)
    master.resize((256, 256), Image.LANCZOS).save(os.path.join(pyqt_assets, "icon.png"))
    # Dashboard favicon (served from the embedded /static).
    web_dir = os.path.join(ROOT, "..", "go", "cmd", "bootstrap", "web")
    os.makedirs(web_dir, exist_ok=True)
    master.resize((64, 64), Image.LANCZOS).save(os.path.join(web_dir, "icon.png"))
    # Adaptive-icon foreground: same art at 66% on a transparent square.
    fg = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    scaled = master.resize((int(SIZE * 0.72), int(SIZE * 0.72)), Image.LANCZOS)
    off = (SIZE - scaled.width) // 2
    fg.alpha_composite(scaled, (off, off))
    fg.save(os.path.join(ROOT, "assets", "icon-1024-fg.png"))
    print("icons written:",
          os.path.join(ROOT, "assets"), "+", pyqt_assets, "+", web_dir)


if __name__ == "__main__":
    main()
