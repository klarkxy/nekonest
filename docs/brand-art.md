> English | [简体中文](./brand-art.zh-CN.md)

# NekoNest brand art

Maintainer note: production brand and agent assets already ship under `pwa/public`. Rebuild only when replacing source art.

The production mascot and agent portraits are original hard-cel anime catgirl assets. The current shipped set was regenerated with Grok CLI Imagine (`image_edit`, 2026-08-06) against the established NekoNest house style. Earlier gpt-image-2/DragTokens sources remain historical only. **No API key is stored in this repository.**

## Art direction

- Shared language: premium Japanese visual-novel catgirl illustration, crisp line art, polished cel shading, luminous eyes, strong small-icon silhouette.
- Originality: chocolate/vanilla contrast and cozy catgirl-game mood without copying any existing franchise character.
- Logo: warm cocoa-haired and pearl-silver-haired catgirl pair, square icon-safe composition, no text or watermark.
- Claude Code: copper-auburn scholarly craftswoman, amber and terracotta.
- Codex: short black/teal engineer, emerald circuit motif.
- Kimi CLI: long midnight-blue moon navigator, periwinkle and silver.
- Grok Build: black/white wolf-cut investigator, graphite and electric cyan.

Every generation prompt required one original character for an avatar (exactly two for the logo), complete cat ears, generous crop padding, no text, no watermark, no copied character design, and exact `1254x1254` output.

## Deterministic asset build

After selecting and visually inspecting generated PNG sources:

```powershell
python -m pip install Pillow
python tools/build_brand_assets.py `
  --duo <duo-source.png> `
  --claude-code <claude-source.png> `
  --codex <codex-source.png> `
  --kimi-cli <kimi-source.png> `
  --grok-build <grok-source.png>
```

The script only performs local square fitting, resizing, WebP/PNG encoding, and maskable-icon padding. It writes stable paths consumed by the PWA under `pwa/public/brand` and `pwa/public/agents`.

## Related

- [README](../README.md)
- `tools/build_brand_assets.py`
