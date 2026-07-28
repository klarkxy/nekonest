"""Build final NekoNest web assets from approved generated source images.

This is deterministic post-processing only: it crops, resizes, and encodes the
selected originals. It never calls an image service and never reads API keys.
"""

from __future__ import annotations

import argparse
from pathlib import Path

from PIL import Image, ImageDraw, ImageOps


ROOT = Path(__file__).resolve().parents[1]
PUBLIC_DIR = ROOT / "pwa" / "public"
BRAND_DIR = PUBLIC_DIR / "brand"
AGENT_DIR = PUBLIC_DIR / "agents"
IVORY = "#fff8ef"


def source_image(path: Path) -> Image.Image:
    if not path.is_file():
        raise ValueError(f"missing generated source image: {path}")
    image = ImageOps.exif_transpose(Image.open(path)).convert("RGB")
    if min(image.size) < 512:
        raise ValueError(f"source image is too small: {path} ({image.size})")
    return image


def square(image: Image.Image, pixels: int) -> Image.Image:
    return ImageOps.fit(
        image,
        (pixels, pixels),
        method=Image.Resampling.LANCZOS,
        centering=(0.5, 0.5),
    )


def padded_square(image: Image.Image, pixels: int, padding_ratio: float) -> Image.Image:
    inset = round(pixels * padding_ratio)
    inner = square(image, pixels - inset * 2)
    canvas = Image.new("RGB", (pixels, pixels), IVORY)
    canvas.paste(inner, (inset, inset))
    return canvas


def save_webp(image: Image.Image, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path, "WEBP", quality=94, method=6)


def save_png(image: Image.Image, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path, "PNG", optimize=True)


def save_notification_badge(path: Path, pixels: int = 96) -> None:
    """Write a transparent monochrome cat silhouette for Android badges."""
    canvas = Image.new("RGBA", (pixels, pixels), (0, 0, 0, 0))
    draw = ImageDraw.Draw(canvas)
    ink = (255, 255, 255, 255)

    def point(x: int, y: int) -> tuple[int, int]:
        return round(x * pixels / 96), round(y * pixels / 96)

    draw.polygon(
        [point(18, 44), point(27, 10), point(45, 38)],
        fill=ink,
    )
    draw.polygon(
        [point(51, 38), point(69, 10), point(78, 44)],
        fill=ink,
    )
    draw.ellipse(
        (*point(16, 30), *point(80, 91)),
        fill=ink,
    )
    save_png(canvas, path)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build final NekoNest brand and agent assets."
    )
    parser.add_argument("--duo", type=Path, required=True)
    parser.add_argument("--claude-code", type=Path, required=True)
    parser.add_argument("--codex", type=Path, required=True)
    parser.add_argument("--kilo", type=Path, required=True)
    parser.add_argument("--kimi-cli", type=Path, required=True)
    parser.add_argument("--grok-build", type=Path, required=True)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    sources = {
        "claude-code": source_image(args.claude_code),
        "codex": source_image(args.codex),
        "kilo": source_image(args.kilo),
        "kimi-cli": source_image(args.kimi_cli),
        "grok-build": source_image(args.grok_build),
    }
    duo_source = source_image(args.duo)

    for name, image in sources.items():
        save_webp(square(image, 512), AGENT_DIR / f"{name}.webp")

    duo_1024 = square(duo_source, 1024)
    save_webp(duo_1024, BRAND_DIR / "nekonest-duo.webp")
    save_png(duo_1024, BRAND_DIR / "nekonest-mark-1024.png")
    save_webp(square(duo_source, 512), AGENT_DIR / "unknown.webp")
    save_webp(square(duo_source, 512), PUBLIC_DIR / "neko-avatar.webp")

    for name, pixels in (
        ("pwa-512x512.png", 512),
        ("pwa-192x192.png", 192),
        ("apple-touch-icon.png", 180),
    ):
        save_png(square(duo_source, pixels), BRAND_DIR / name)
    save_png(
        padded_square(duo_source, 512, 0.1),
        BRAND_DIR / "pwa-512x512-maskable.png",
    )
    save_notification_badge(BRAND_DIR / "notification-badge.png")

    print("built generated NekoNest brand assets:")
    for path in sorted((*AGENT_DIR.glob("*.webp"), *BRAND_DIR.iterdir())):
        if path.is_file():
            print(path.relative_to(ROOT))


if __name__ == "__main__":
    main()
