"""Overlay exact topology labels onto the catgirl architecture scene."""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "docs" / "images" / "how-it-works-scene.jpg"
OUT_DIR = ROOT / "docs" / "images"

FONT_LATIN = Path(r"C:\Windows\Fonts\segoeui.ttf")
FONT_LATIN_BOLD = Path(r"C:\Windows\Fonts\segoeuib.ttf")
FONT_CJK = Path(r"C:\Windows\Fonts\msyh.ttc")

CREAM = (255, 246, 236)
INK = (62, 38, 28)
MUTED = (120, 86, 68)
COCOA = (176, 108, 78)
TEAL = (46, 120, 110)
NAVY = (70, 86, 140)
GRAPHITE = (80, 86, 96)
FOOTER = 92


def load_font(path: Path, size: int) -> ImageFont.FreeTypeFont:
    if path.suffix.lower() == ".ttc":
        return ImageFont.truetype(str(path), size=size, index=0)
    return ImageFont.truetype(str(path), size=size)


def measure(draw: ImageDraw.ImageDraw, text: str, used: ImageFont.ImageFont) -> tuple[int, int]:
    left, top, right, bottom = draw.textbbox((0, 0), text, font=used)
    return right - left, bottom - top


def badge(
    canvas: Image.Image,
    title: str,
    subtitle: str | None,
    cx: int,
    cy: int,
    title_font: ImageFont.ImageFont,
    sub_font: ImageFont.ImageFont,
    accent: tuple[int, int, int],
) -> None:
    overlay = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)
    tw, th = measure(draw, title, title_font)
    sw, sh = measure(draw, subtitle, sub_font) if subtitle else (0, 0)
    pad_x, pad_y, gap = 18, 7, 1 if subtitle else 0
    w = max(tw, sw) + pad_x * 2
    h = th + (sh + gap if subtitle else 0) + pad_y * 2
    x0, y0 = int(cx - w / 2), int(cy - h / 2)
    shadow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    ImageDraw.Draw(shadow).rounded_rectangle(
        (x0 + 2, y0 + 3, x0 + w + 2, y0 + h + 3),
        radius=13,
        fill=(70, 48, 32, 48),
    )
    overlay = Image.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(3)), overlay)
    draw = ImageDraw.Draw(overlay)
    draw.rounded_rectangle(
        (x0, y0, x0 + w, y0 + h),
        radius=13,
        fill=(255, 248, 240, 236),
        outline=(*accent, 230),
        width=2,
    )
    draw.text((x0 + (w - tw) // 2, y0 + pad_y - 2), title, font=title_font, fill=(*INK, 255))
    if subtitle:
        draw.text(
            (x0 + (w - sw) // 2, y0 + pad_y + th + gap - 2),
            subtitle,
            font=sub_font,
            fill=(*MUTED, 255),
        )
    canvas.alpha_composite(overlay)


def render(kind: str, dest: Path) -> None:
    scene = Image.open(SRC).convert("RGBA")
    width, height = scene.size
    canvas = Image.new("RGBA", (width, height + FOOTER), (*CREAM, 255))
    canvas.paste(scene, (0, 0))
    draw = ImageDraw.Draw(canvas)
    draw.line((0, height, width, height), fill=(*COCOA, 80), width=1)

    latin_title = load_font(FONT_LATIN_BOLD if FONT_LATIN_BOLD.exists() else FONT_LATIN, 20)
    latin_sub = load_font(FONT_LATIN, 13)
    cjk_title = load_font(FONT_CJK, 20)
    cjk_sub = load_font(FONT_CJK, 13)
    title_font = cjk_title if kind == "zh" else latin_title
    sub_font = cjk_sub if kind == "zh" else latin_sub

    if kind == "zh":
        phone, nest, outbound, host = "手机 PWA", "VPS Server", "由 PC 出站", "主机 Daemon"
        phone_sub, nest_sub, out_sub, host_sub = "Vue 3", "认证 · 中转 · SQLite", "WSS", "Windows / Linux"
    else:
        phone, nest, outbound, host = "Phone PWA", "VPS Server", "WSS outbound", "Host Daemon"
        phone_sub, nest_sub, out_sub, host_sub = "Vue 3", "auth · relay · SQLite", "from the home PC", "Windows / Linux"

    badge(canvas, phone, phone_sub, 268, 668, title_font, sub_font, COCOA)
    badge(canvas, "HTTPS / WSS", None, 430, 162, latin_title, latin_sub, TEAL)
    badge(canvas, nest, nest_sub, 790, 388, title_font, sub_font, COCOA)
    badge(canvas, outbound, out_sub, 1130, 338, title_font, sub_font, TEAL)
    badge(canvas, host, host_sub, 640, 556, title_font, sub_font, GRAPHITE)

    agents = [
        (628, "Claude Code", COCOA),
        (792, "Codex", TEAL),
        (968, "Kimi CLI", NAVY),
        (1148, "Grok Build", GRAPHITE),
    ]
    footer_y = height + FOOTER // 2
    for cx, name, accent in agents:
        badge(canvas, name, None, cx, footer_y, latin_title, latin_sub, accent)

    canvas.convert("RGB").save(dest, quality=93)
    print(dest, dest.stat().st_size)


if __name__ == "__main__":
    render("en", OUT_DIR / "how-it-works.en.jpg")
    render("zh", OUT_DIR / "how-it-works.zh-CN.jpg")
