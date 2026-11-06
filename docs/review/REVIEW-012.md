# REVIEW-012 · rv-16 复核（Tailwind v4 + Radix 迁移）

- `basis:` `rv-15..082c31c`（步 1–3 + 步 4/5 合并，共 4 提交）
- `verdict:` **放行**，打 `rv-16`
- `method:` 干净命令复跑 + **lint 变异自证** + 真机 CDP 几何/交互实测 + bundle 前后测量

## 1. 范围与结构

| 检查 | 结果 |
| --- | --- |
| 越界 | `rv-15..HEAD` 只含 `web/**`、`internal/ui/dist/**`、`report-011.md` |
| CSS Modules | 6 个 `.module.css` **全部删除**；`git grep "\.module\.css" web/src` = **0 命中** |
| 样式文件 | 只剩 `styles/{tokens.css, tailwind.css, global.css, style-rules.test.ts}` |

## 2. 命令复核

```
$ npx vitest run                    → 7 files / 50 tests passed
$ npx tsc --noEmit / stylelint      → 干净
$ npm run build && git diff --exit-code internal/ui/dist → 0
$ go build ./... && go test ./...（无 DSN）→ 8 包全绿
```

## 3. lint 变异自证（我自己插的，不看对方自述）

在 `Skeleton.tsx` 的 `cn()` 里临时插入 `"bg-slate-100 p-[13px]"`：

```
× 不使用 arbitrary value（Radix 变体选择器白名单除外）
× 不使用 Tailwind 默认调色板
× 间距 arbitrary value 必须落在 4px 网格上
→ 3 条同时报红；还原后 6 passed
```

`data-[state=open]` 白名单规则限定为 `^(data|aria|has|group|peer)-\[…\]$`，当前代码 0 处使用，属预留而非滥用。**接受**。

## 4. 真机 CDP（1440×1200，与迁移前对比）

| 项 | 迁移前 | 迁移后 |
| --- | --- | --- |
| `main` | 200 / 1240 | **200 / 1240** |
| `table` | x=225 / w=1190 | **x=225 / w=1190** |
| 行高 | 48 | **48** |
| terminals 列宽 | `[180,180,180,370,120,160]` | **完全一致** |
| 抽屉 | select ×5、3 个必填 | **Radix `combobox` ×1 + 3 个必填**（布尔字段仍用三态 Select，未用 Checkbox） |
| 只读快照页 | 11 列 / 0 排序 / 0 写入口 | **一致**；零写请求、CSP 0、console error 0 |

几何零漂移，与对方“整页 AE 1–2% 仅文字抗锯齿”的说法互相印证。

## 5. bundle 实测（我的测量，不是转述）

| 口径 | raw | gzip |
| --- | --- | --- |
| 首屏 5 个资产（index + react + tanstack + router + css） | **441,232** | **138,386** |
| 全部 10 个资产 | **629,458** | **199,727** |

- 首屏 **+16% gzip**、总量 **+19% gzip**（对方报 +14.3%/+16.0% 与 +18.3%/+19.0%，量级一致；差异来自重建后哈希文件数 10 vs 11）。
- 主因：`vendor-radix` 27.6 → **78.4 KB**（Select/AlertDialog/Checkbox + floating-ui）、入口 22.4 → **50.6 KB**（工具类字符串）。
- `vendor-zod`（82 KB raw）**不在首屏引用链**里，随抽屉按需加载——对方建议的“zod 移出首屏”实际已经成立。

## 6. 非阻断发现

| # | 项 | 建议 |
| --- | --- | --- |
| 1 | 声明但未使用：`@radix-ui/react-popover`、`@radix-ui/react-label`、`@radix-ui/react-tooltip`（各 0 处引用） | 下一轮删除，或明确保留为“预留”；树摇后不进产物，但属死依赖 |
| 2 | `tailwindcss` / `@tailwindcss/vite` 放在 `dependencies` 而非 `devDependencies` | 构建期依赖，移到 devDependencies 更准确 |
| 3 | 首屏 gzip +16% | 已如实记录；下一步可考虑把抽屉（含 zod + Radix Select）整体 lazy，让实体页首屏只带 `vendor-react` + 表格 |
| 4 | 布尔字段仍用三态 Select（未用 Checkbox） | 与“未设置/启用/停用”语义一致，**采纳实现**，不算偏离 |

## 7. 结论

迁移目标达成：样式载体换成 Tailwind v4 + `cn()`/CVA，令牌仍是唯一颜色来源，lint 从“扫 CSS”正确迁到“扫 TSX 与 CSS”并能自证。**打 `rv-16`**，唯一需要跟进的实质项是 bundle 增长（§6.3）。
