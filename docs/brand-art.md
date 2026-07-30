# NekoNest generated brand art

Maintainer note: production brand and agent assets already ship under
`pwa/public`. Rebuild only when replacing source art.

The production mascot and agent portraits are original `gpt-image-2` outputs
generated through the user-approved DragTokens image endpoint on 2026-07-28.
No API key is stored in this repository.

## Art direction

- Shared language: premium Japanese visual-novel catgirl illustration, crisp
  line art, polished cel shading, luminous eyes, strong small-icon silhouette.
- Originality: the characters use the broad chocolate/vanilla contrast and
  cozy catgirl-game mood requested by the product owner, but do not copy any
  existing franchise character.
- Logo: a warm cocoa-haired and a pearl-silver-haired catgirl pair, centered in
  a square icon-safe composition with no text or watermark.
- Claude Code: copper-auburn scholarly craftswoman, amber and terracotta.
- Codex: short black/teal engineer, emerald circuit motif.
- Kilo: strawberry twin-bun builder, raspberry speed and block motifs.
- Kimi CLI: long midnight-blue moon navigator, periwinkle and silver.
- Grok Build: black/white wolf cut investigator, graphite and electric cyan.

Every generation prompt explicitly required one original character for an
avatar (exactly two for the logo), complete cat ears, generous crop padding,
no text, no watermark, no copied character design, and exact `1254x1254`
output.

## Deterministic asset build

After selecting and visually inspecting the generated PNG sources, build the
web assets with:

```powershell
python -m pip install Pillow
python tools/build_brand_assets.py `
  --duo <duo-source.png> `
  --claude-code <claude-source.png> `
  --codex <codex-source.png> `
  --kilo <kilo-source.png> `
  --kimi-cli <kimi-source.png> `
  --grok-build <grok-source.png>
```

The script performs only local square fitting, resizing, WebP/PNG encoding, and
maskable-icon padding. It writes the stable paths consumed by the PWA under
`pwa/public/brand` and `pwa/public/agents`.
