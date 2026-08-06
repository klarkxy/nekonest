> [English](./brand-art.md) | 简体中文

# NekoNest 品牌美术

维护者说明：生产环境品牌与智能体资源已随 `pwa/public` 发布。仅在更换源图时重建。

生产用吉祥物与智能体立绘为硬边赛璐璐日系猫娘原创资产。当前随包版本由 Grok CLI Imagine（`image_edit`，2026-08-06）在既有 NekoNest 画风上重生；更早的 gpt-image-2/DragTokens 源图仅作历史参考。**仓库中不存放任何 API 密钥。**

## 美术方向

- 共同语言：偏视觉小说的高级日系猫娘插画、清晰线稿、精致赛璐璐、明亮眼睛、小图标下仍成立的剪影。
- 原创性：巧克力/香草对比与舒适猫娘游戏气质，不抄袭任何既有 IP 角色。
- Logo：暖可可发色与珍珠白发猫娘双人，方形图标安全构图，无文字无水印。
- Claude Code：赤铜褐发学者工匠，琥珀与赤陶。
- Codex：短黑/青绿工程师，翠绿电路母题。
- Kilo：草莓双丸子建造者，覆盆子速度与方块母题。
- Kimi CLI：长夜蓝月导航者，长春花蓝与银。
- Grok Build：黑白狼剪调查者，石墨与电青。

每次生成提示均要求头像为一名原创角色（Logo 恰好两名）、完整猫耳、充足裁切边距、无文字、无水印、不抄角色设计，以及精确 `1254x1254` 输出。

## 确定性资源构建

选定并目视检查生成的 PNG 源图后：

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

脚本仅做本地方形适配、缩放、WebP/PNG 编码与 maskable 图标留白，写入 PWA 使用的稳定路径：`pwa/public/brand` 与 `pwa/public/agents`。

## 相关

- [README.zh-CN.md](../README.zh-CN.md)
- `tools/build_brand_assets.py`
