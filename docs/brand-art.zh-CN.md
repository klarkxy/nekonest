> [English](./brand-art.md) | 简体中文

# 品牌资源

发布使用的品牌与智能体图片位于 `pwa/public/brand` 和 `pwa/public/agents`。
只有已批准原图变化时才重建。

```powershell
python -m pip install Pillow
python tools/build_brand_assets.py `
  --duo "C:\path\duo-source.png" `
  --claude-code "C:\path\claude-source.png" `
  --codex "C:\path\codex-source.png" `
  --kimi-cli "C:\path\kimi-source.png" `
  --grok-build "C:\path\grok-source.png"
```

脚本负责正方形适配、缩放、编码和 maskable icon 留白。输入与输出路径以它的
`-h` 输出为准。

README 拓扑图的底图与本地化输出统一放在 `docs/images/`。在 Windows 上用以下
命令同时重建中英文标注图：

```powershell
python tools/label_how_it_works.py
```

标注脚本使用系统 Segoe UI 与微软雅黑字体。提交时应同时包含底图和两份本地化
输出。

重建后：

1. 在小尺寸检查 Logo、智能体头像、favicon 与 maskable icon。
2. 运行 PWA 测试和视觉回归。
3. 将选定的派生资源作为一组提交，不要手改单个尺寸造成不一致。

仓库不得包含生成凭据或私有原图。
