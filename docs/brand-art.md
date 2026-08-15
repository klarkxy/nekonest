> English | [简体中文](./brand-art.zh-CN.md)

# Brand assets

Shipped brand and agent images live under `pwa/public/brand` and
`pwa/public/agents`. Rebuild them only when approved source art changes.

```powershell
python -m pip install Pillow
python tools/build_brand_assets.py `
  --duo "C:\path\duo-source.png" `
  --claude-code "C:\path\claude-source.png" `
  --codex "C:\path\codex-source.png" `
  --kimi-cli "C:\path\kimi-source.png" `
  --grok-build "C:\path\grok-source.png"
```

The script performs square fitting, resizing, encoding, and maskable-icon
padding. Its `-h` output is authoritative for inputs and output paths.

The README topology illustration keeps its source scene and localized outputs
under `docs/images/`. On Windows, rebuild both labeled versions with:

```powershell
python tools/label_how_it_works.py
```

The overlay script uses the system Segoe UI and Microsoft YaHei fonts. Commit
the source scene and both localized outputs together.

After rebuilding:

1. Inspect the logo, agent portraits, favicon, and maskable icons at small size.
2. Run PWA tests and visual regression.
3. Commit the selected derived assets together; do not hand-edit individual
   sizes into an inconsistent set.

No generation credentials or private source art belong in the repository.
