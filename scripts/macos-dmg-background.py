#!/usr/bin/env python3
"""Render the animated Finder-window background used by the macOS DMG."""

import math
import random
import sys

from PIL import Image, ImageDraw, ImageFont


WIDTH, HEIGHT = 760, 480
FRAME_COUNT = 30
SCALE = 2
FONT = "/System/Library/Fonts/Supplemental/Avenir Next.ttc"


def render_base(scale):
    image = Image.new("RGB", (WIDTH, HEIGHT))
    pixels = image.load()
    for y in range(HEIGHT):
        for x in range(WIDTH):
            glow = max(0, 1 - math.sqrt(((x - 380) / 520) ** 2 + ((y - 210) / 430) ** 2))
            pixels[x, y] = (
                int(244 + 4 * glow),
                int(242 + 5 * glow),
                int(237 + 8 * glow),
            )

    if scale == 2:
        image = image.resize((WIDTH * 2, HEIGHT * 2), Image.Resampling.LANCZOS)
    draw = ImageDraw.Draw(image, "RGBA")
    random.seed(17)
    for _ in range(48):
        x, y = random.randrange(WIDTH) * scale, random.randrange(HEIGHT) * scale
        radius = random.choice((1, 1, 2)) * scale
        draw.ellipse((x - radius, y - radius, x + radius, y + radius), fill=(66, 91, 143, 28))
    return image


def render_frame(frame, scale, base):
    image = base.copy()
    draw = ImageDraw.Draw(image, "RGBA")
    heading = ImageFont.truetype(FONT, 24 * scale)
    draw.text(
        (WIDTH * scale // 2, 70 * scale),
        "Drag to Applications to install",
        font=heading,
        fill=(35, 42, 58, 255),
        anchor="mm",
    )

    trail = []
    for index in range(97):
        t = index / 96
        x = (238 + t * 284) * scale
        y = (251 - 16 * math.sin(math.pi * t)) * scale
        trail.append((x, y))
    draw.line(trail, fill=(74, 95, 145, 70), width=2 * scale)

    # Each dot advances from Attune toward the Applications shortcut each frame.
    for index in range(28):
        t = (index / 28 + frame / FRAME_COUNT) % 1
        x = (238 + t * 284) * scale
        y = (251 - 16 * math.sin(math.pi * t)) * scale
        fade = 0.25 + 0.75 * math.sin(math.pi * t)
        alpha = int(75 + 165 * fade)
        radius = (2 if index % 4 else 3) * scale
        draw.ellipse((x - radius, y - radius, x + radius, y + radius), fill=(64, 99, 181, alpha))
        if index % 4 == 0:
            draw.line((x - 6 * scale, y, x + 6 * scale, y), fill=(82, 114, 190, int(alpha * 0.45)), width=scale)
            draw.line((x, y - 6 * scale, x, y + 6 * scale), fill=(82, 114, 190, int(alpha * 0.45)), width=scale)

    return image


destination = sys.argv[1]
base = render_base(SCALE)
frames = [render_frame(frame, SCALE, base) for frame in range(FRAME_COUNT)]
frames[0].save(
    destination,
    format="PNG",
    save_all=True,
    append_images=frames[1:],
    duration=80,
    loop=0,
    dpi=(72 * SCALE, 72 * SCALE),
)
