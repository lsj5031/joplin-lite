# UI 问题解决方案

本文针对 `docs/ui-feedback-review.md` 提出的问题，给出可直接落地的修改方案。

## 已落地（2026-11-27）

- 删除浏览器预览后遗留的 QA：`qa/test_ui.mjs`、`qa/test_browser_ux.mjs`、
  `qa/make_demo.mjs`、`qa/seed_ui_data.py`、`qa/merge_results.py`，以及
  `qa/features.csv` 里 62 条只描述浏览器界面的 UI-* 条目（其余 Go/API/CLI 条目保留）。
- 产品片改由 MyGO render 插件在 Go 里合成：新增 `shell/promo_film_test.go`，删除
  `qa/promo/compose.html`、`render.mjs`、`render.test.mjs`。`docs/promo/*.mp4` 与
  poster 已重新生成，`docs/screenshots/*.png` 已重新生成。
- N2 搜索高亮：按词高亮（`"report draft"` 两个词都亮），重叠区间合并，纯空白查询
  不改变标题。
- 测试夹具里的中文表格行已移出 README 截图用的 fixture。
- 文档与注释的小修：`.gitignore` 空标题、`README.md` 断行与 `--web-dir` 移除说明、
  `DESIGN.md` 断行、`service/service.go` 注释、`qa/README.md` 重写。

## 0. 先回答一个前提：web 是否属于本产品

`web/` 不是桌面应用的一部分。证据：

- `internal/webui/webui.go` 开头写明："The desktop app no longer embeds the SPA —
  the native window is the product shell — so this handler is only mounted by
  `gojoplin serve --web-dir`"。
- `shell/build.sh`、`shell/bundle.sh`、`.github/workflows/release.yml` 都不构建
  `web/`；发布包里的界面完全由 MyGO 原生绘制。
- 它只有通过 `gojoplin serve --web-dir web/dist/client` 手动启动，才会在浏览器里
  出现，走的是同一套 HTTP Data API。

所以：**桌面用户看到的永远是原生 UI**。外部反馈里"表格被裁切"一类问题，如果出自
浏览器预览，就不影响实际使用；如果出自原生窗口，才需要按下面的 N 类修复。

由此产生一个取舍。**本计划采用"删除 web"作为推荐路径**，执行清单见 §2：

- **删除（推荐）**：可以减掉 21 个源文件、一套 React/TanStack/Vite 依赖，以及第二套
  设计系统。它现在有独立的一整套设计规则（40px 标题、1.95 行高、300px 库列、dialog
  而非抽屉），会持续回流到原生规格里，增加判断成本。
- **保留**：如果确实有人在用，则把 `web/` 明确标注为"内部预览，不属于产品"，
  `DESIGN.md` 里关于浏览器的条目降为附录，并按 §4 修掉它的排版问题。

无论选哪条，**顺序都是先做原生的 N 类修复**——那才是产品界面。

下面的 W 类只在你决定保留浏览器预览时才做。

## 1. 优先级

| 编号 | 问题 | 位置 | 优先级 |
| --- | --- | --- | --- |
| N1 | 原生宽表格只能横向滚动，没有提示 | `shell/native.go` | P0 |
| N2 | 搜索没有命中高亮 | `shell/workspace.go` | P0 |
| N3 | 列表标题截断后无法看全文 | `shell/workspace.go` | P1 |
| N4 | Ask 回答字号偏小 | `shell/ask_markdown.go` | P1 |
| N5 | 中文回退字重不一致 | `shell/design.go` | P1 |
| D | 删除 `web/` 及其全部引用 | 见 §2 | 独立执行 |
| W1 | 浏览器表格溢出 | `web/src/components/EditorPane.tsx` | 删除后作废 |
| W2 | 浏览器中文排版 | `web/src/styles/app.css` | 删除后作废 |

## 2. 删除 web 的执行计划

**不能直接 `rm -rf web/`**：构建脚本从 `web/public/` 取图标，删早了会打断 macOS 打包和
Windows 图标生成。必须按下面的顺序做。

### 2.1 先迁出图标（必须最先做）

`web/public/` 里真正被产品使用的是四个图标文件，其余（`favicon*`、`apple-touch-icon`、
`site.webmanifest`、`logo.svg`、`fonts/`）只服务浏览器预览，可以随目录删除。

新建 `assets/icons/`，迁入：

| 原路径 | 新路径 | 用途 |
| --- | --- | --- |
| `web/public/app-icon.icns` | `assets/icons/app-icon.icns` | macOS 包图标 |
| `web/public/app-icon.ico` | `assets/icons/app-icon.ico` | Windows 可执行文件图标 |
| `web/public/android-chrome-512x512.png` | `assets/icons/app-icon-512.png` | icongen 源图 |
| `web/public/android-chrome-192x192.png` | `assets/icons/app-icon-192.png` | README 头图 |

然后改这五处引用（全部已核对；`bundle.sh` 与 `winres.sh` 都会先 `cd` 到仓库根，
所以 `$PWD` 就是仓库根）：

- `shell/bundle.sh:16` → `ICNS="$PWD/assets/icons/app-icon.icns"`
- `shell/winres.sh:17-18` → `ART="$PWD/assets/icons/app-icon-512.png"`、
  `ICO="$PWD/assets/icons/app-icon.ico"`
- `shell/winres.sh:2-3` 与 `shell/build.sh:3-5` 的文件头注释：去掉"web preview"的说法
- `tools/icongen/main.go:4-5`（注释里的用法示例，同时更新 `shell/README.md:58` 的说明）
- `README.md:1` → `<img src="assets/icons/app-icon-192.png" ...>`

迁完后立刻验证打包，确认图标没丢：

```sh
bash shell/bundle.sh && ls "tmp/Joplin Lite.app/Contents/Resources/app-icon.icns"
bash shell/winres.sh && ls -l shell/rsrc_windows_amd64.syso
```

### 2.2 移除 Go 侧的浏览器预览支持

`web/` 删掉后，`--web-dir` 永远没有可服务的目录，留着就是死开关。一并移除：

- `internal/webui/`（`webui.go`、`webui_test.go` 整个包）
- `cmd/serve.go`：`--web-dir`、`--web-addr` 两个 flag（`cmd/serve.go:221-222`）、
  `webDir` 变量与 `webDirPath()`（`191-195`）、挂载分支（`131-132`）、`215` 行的字段
- `cmd/serve_test.go:34-35`：依赖 `GOJOPLIN_WEB_DIR` 的用例
- `GOJOPLIN_WEB_DIR` 环境变量在 `config.yaml.example` 或文档中的说明（如有）

这是**命令行接口的破坏性变更**：`--web-dir` 和 `--web-addr` 会消失。如果要保守一点，
可以只删 `web/` 目录、保留 `webui` 包和 flag（它能服务任意已构建的 SPA 目录），
但那样会留下一个没有内容可服务的开关。建议删干净，并在 CHANGELOG/发版说明里写明。

### 2.3 清理文档与配置

- `PRODUCT.md:7` 和 `:11`：去掉"optional standalone browser SPA in `web/`"和
  "An optional React/TanStack browser UI lives in `web/`"。
- `DESIGN.md`：删除或改写约 12 处浏览器专属条目（`:6`、`:22`、`:27`、`:36-37`、`:41`、
  `:66`、`:70`、`:77`、`:79-80`、`:89-90`、`:102`、`:132`、`:147`、`:153`）。
  其中 `:77` 的"keep ... in `shell/design.go` and `web/src/styles/app.css`"改为只剩
  `shell/design.go`；`:70` 的"700 CSS px"合并进原生测量宽度那一条。
- `shell/README.md:392-408`：整节 Browser preview 删除；`:58` 的 icongen 路径改到
  `assets/icons/`。
- `README.md:192`：删掉 "and the optional browser preview"。
- `shell/build.sh:3-5`：文件头注释里关于可选浏览器预览的两行删掉。
- `.gitignore:9-16`（`web/node_modules/`、`web/dist/`、`web/.output/`、`web/.vinxi/`、
  `web/.tanstack/`、`web/.vite/`、`shell/webdist/`）与 `:51-54`（`/web/.devcontainer/`、
  `/web/.vscode/`、`/web/.prettierignore`）删除。

### 2.4 已核对：以下东西不受影响，不要误删

- **CI 不构建 web**：`.github/workflows/ask.yml` 里的 `npm ci` 是 `cd agent` 下的
  助手包，与 `web/` 无关，保持不变。
- **`THIRD_PARTY_NOTICES.md` 不会变**：`tools/third-party-notices.mjs` 只清点 Go 包，
  从不读 `web/package.json`。附带结论是 web 的 npm 依赖本来就没进这个文件，而是靠
  `web/license-notices.ts` 单独生成——这也是删除它的又一个理由。
- **`qa/` 不受影响**：`qa/*.mjs`、`qa/*.sh`、`qa/*.py` 没有引用 `web/dist` 或
  `41185` 端口，不需要改。
- **`shell/fonts/` 保留**：原生的 Overpass TTF 与 `OFL.txt` 在这里，和 `web/public/fonts/`
  的 woff2 是两份独立副本。只删后者。

### 2.5 验收

```sh
go build ./... && go test ./...
(cd shell && go build ./... && go test ./... -count=1)
bash shell/build.sh && bash shell/bundle.sh
```

再全文搜一遍残留：

```sh
grep -rn "web/\|web-dir\|webui\|41185" --include=*.go --include=*.sh --include=*.md \
  --include=*.yml . | grep -v "^./tmp/"
```

预期只剩 `docs/` 里描述删除过程的文字。

## 3. 原生修复

### N1 表格列宽（P0）

**现状**：`shell/native.go` 的 `prepareMarkdownOptions()` 只设了 `TableBorder`、
`TableHeader`、`TableStripe`，没有设 `TableMaxColumn`。MyGO 在 `ui/markdown/theme.go:138`
把它默认成 `24`（em）。原生正文 `sizeBody = 17`，一列最宽就是 `24 × 17 ≈ 408 DIP`；
编辑器测量宽度 `contentMeasure = 700`（`shell/workspace.go:24`），**两列宽表格就已经
超过 700**，必然横向滚动。MyGO 的表格确实包在 `ScrollHorizontal` 里
（`ui/markdown/table.go`），内容没丢，但滚动条在 macOS 上平时不可见，看起来就像被裁掉。

**改法**：在主题里显式设一个适配测量宽度的列上限。

```go
// shell/native.go, prepareMarkdownOptions() 的 markdown.Theme 字面量里
TableMaxColumn: 15, // 15em × 17px ≈ 255 DIP，两列约 510 + padding，落在 700 内
```

这样两列中文表格不用滚动；三列以上仍由已有的 `ScrollHorizontal` 兜底。
若要进一步降低"看不出能滚"的困惑，可以在 `shell/preview.go` 的预览容器右侧加一条
与 `hairlineStrong` 同色的渐变遮罩，仅在内容溢出时显示——但这需要先测量内容宽度，
属于第二步，建议先只改列宽。

### N2 搜索命中高亮（P0）

**现状**：原生和浏览器都没有命中高亮（全仓库只有 `shell/design.go:537` 的注释里出现
"highlight" 一词）。这是外部反馈第 5 点里唯一真缺的部分，也确实是搜索体验的硬伤。

**改法**：`shell/workspace.go:1347` 目前是

```go
name := ui.Text(c, title).FontSize(sizeUI).LineHeight(1.25).SingleLine().Grow(1).MinWidth(0)
```

改为在有查询时走 `ui.RichText`。MyGO 的 `ui.Span` 支持 `Background` 和 `Radius`
（`ui/richtext.go:15`），`SingleLine` 对 `RichText` 同样有效（`docs/ui/text.md:83`）。

```go
// highlightSpans 把 title 按 q 切成普通段与命中段，按 rune 切分以免切坏中文。
func highlightSpans(title, q string) []ui.Span {
    if q == "" {
        return []ui.Span{{Text: title}}
    }
    var spans []ui.Span
    lower, needle := strings.ToLower(title), strings.ToLower(q)
    at := 0
    for {
        i := strings.Index(lower[at:], needle)
        if i < 0 {
            break
        }
        i += at
        if i > at {
            spans = append(spans, ui.Span{Text: title[at:i]})
        }
        spans = append(spans, ui.Span{
            Text:       title[i : i+len(needle)],
            Background: violet.Alpha(0.22),
            Radius:     3,
        })
        at = i + len(needle)
    }
    if at < len(title) {
        spans = append(spans, ui.Span{Text: title[at:]})
    }
    return spans
}
```

行渲染处：

```go
var name ui.Element
if a.query != "" {
    name = ui.RichText(c, highlightSpans(title, a.query)...).
        FontSize(sizeUI).LineHeight(1.25).SingleLine().Grow(1).MinWidth(0)
} else {
    name = ui.Text(c, title).FontSize(sizeUI).LineHeight(1.25).SingleLine().Grow(1).MinWidth(0)
}
```

注意：

- 只在 `a.query != ""` 时才走 RichText，常态保持原来的快速路径。
- 大小写不敏感用 `strings.ToLower` 逐段比对；因为 `ToLower` 可能改变字节长度
  （个别语言），中文与英文场景下安全，但如果要严谨，可以改用 `strings.EqualFold`
  配合 rune 滑窗。
- 命中色用 `violet`（选择色）而不是 `routeBlue`，遵循 DESIGN.md 的
  "route blue 只用于主操作与焦点"。
- 高亮是背景色，不能单独承载语义；标题文字本身仍是完整信息，符合无障碍要求。

### N3 标题全文（P1）

`shell/workspace.go:1347` 的 `name` 已有 `SingleLine()` 会截断。补一个 Tooltip 即可，
成本一行：

```go
name.Tooltip(title)
```

浏览器侧同样处理，见 W3。

### N4 Ask 回答字号（P1）

`shell/ask_markdown.go:27` 的 `FontSize: sizeUI`（13）与正文 17 差距过大，长回答吃力。
改为 15，并保留 1.5 行高：

```go
func (a *nativeApp) askMarkdownOptions(c *ui.Context) *markdown.Options {
    ...
    a.ask.mdTheme = &markdown.Theme{
        FontSize:   scaledText(c, 15),
        LineHeight: 1.55,
        ...
    }
}
```

需要给 `askMarkdownOptions` 加 `c *ui.Context` 参数（唯一调用点
`shell/ask_markdown.go:20` 的 `askAnswer` 已经有 `c`）。同理 `shell/ask.go:762`
的消息正文 `sizeUI` 也可一并提到 15。

### N5 中文回退（P1）

原生内置的 Overpass 只有拉丁字符，中文走系统回退，所以字重和行高会和拉丁文字不一致
——这正是外部反馈"中英混排字重差异明显"的原因。这一步要改的是 MyGO 的字体回退链，
不属于本仓库代码；建议：

1. 先在 `shell/native_cjk_bench_test.go` 已有的中文基准里加一张中英混排的截图用例，
   把现状固定下来（是回归基线，不是修复）。
2. 若要真正统一字重，需要在 `ui` 层给中文指定一个系统字体（macOS 的 PingFang SC、
   Windows 的 Microsoft YaHei），并按字号与拉丁字体做一次基线对齐。这属于 MyGO 的
   改动，要走 `shell/patches/` 的补丁流程，成本较高，建议单独评估。

## 4. 浏览器修复（**删除 web 后整节作废**；仅在决定保留时执行）

### W1 表格溢出

**现状**：`web/src/styles/app.css:299-310` 的 `.prose-note table` 没有宽度约束，单元格
也没有 `overflow-wrap`；`.editor-body` 是 `overflow-y: auto`，按 CSS 规范
`overflow-x` 会被计算为 `auto`，所以是整个编辑区横向滚动，表格第一列和表头会一起
滑走，比裁切更难受。

**改法**：在 markdown-it 渲染阶段给表格包一层滚动容器。`web/src/components/EditorPane.tsx:7`
已经有一个 `md` 实例，按它现有的 rule 覆盖方式加两条：

```ts
// 表格自己滚动，而不是把整个编辑区拖走。
const tableOpen = md.renderer.rules.table_open
md.renderer.rules.table_open = (tokens, idx, options, env, self) =>
  '<div class="table-scroll">' + (tableOpen
    ? tableOpen(tokens, idx, options, env, self)
    : self.renderToken(tokens, idx, options))
md.renderer.rules.table_close = () => '</table></div>'
```

`md.render` 的结果会经过 `DOMPurify.sanitize`（`EditorPane.tsx:94`），`div` 在默认允许
列表内，不会被清掉。

配套 CSS：

```css
.table-scroll { overflow-x: auto; margin: 0 0 14px; }
.prose-note th,
.prose-note td { overflow-wrap: anywhere; }
```

### W2 浏览器中文排版

三处独立改动，都只影响浏览器预览：

```css
/* app.css:4 —— 让中文有确定字体，减少与拉丁字的字重差异 */
--font-sans: 'Avenir Next', Avenir, 'Overpass', 'PingFang SC', 'Microsoft YaHei',
  ui-sans-serif, system-ui, sans-serif;

/* app.css:189 —— ch 以数字 "0" 为单位，一个汉字约 2ch，中文每行只有英文一半 */
.prose-note { max-width: 700px; }
```

第三处是 `lang`：`web/src/routes/__root.tsx:49` 固定 `lang="en"`，中文会按英文规则选
字形。最小改动是在渲染笔记正文的容器上按内容补：

```tsx
// EditorPane.tsx 预览容器
const cjk = /[\u3400-\u9FFF]/.test(note.body)
<div className="prose-note" lang={cjk ? 'zh-Hans' : undefined} ... />
```

不要直接改 `<html lang>`——界面文案本身是英文，改了会影响英文朗读和断词。

### W3 列表标题与字号

- `web/src/components/NoteList.tsx:113` 标题是 `truncate`，且没有 `title` 属性。
  加 `title={n.title}`，成本最低。
- 同一文件 119 行的元信息是 `text-[11.5px]`，148 行的 `time` 是 10px。建议提到
  12px 与 11px，这是"小字太小"反馈里唯一值得动的部分。
- `NoteList.tsx:54` 的 `text-[18px]` 是死代码：`app.css:144` 的
  `.note-list > header h2 { font-size: 14px }` 是未分层规则，优先级高于 Tailwind
  的 `@layer utilities`，实际生效的是 14px。删掉 `text-[18px]`，只留一处定义。

## 5. 验证

改完后按下面顺序确认，全部在改动提交前完成：

1. **原生表格**：在 `shell/screens_test.go` 的种子笔记里加一篇两列中文宽表格，
   跑 `go test -run TestCaptureScreens`，看 PNG 里两列是否不再需要横向滚动。
2. **搜索高亮**：加一个单测，构造 `a.query = "report"` 与标题
   `"Q3 report draft"`，断言 `highlightSpans` 返回三段、中间段带背景色；再在测试器里
   断言命中文字仍然存在（`tt.HasText`）。
3. **标题 tooltip**：确认 `SingleLine` 截断的标题在悬停时显示全文。
4. **Ask 字号**：截图对比改前改后。
5. **浏览器**（如保留）：`cd web && npm run dev`，在 1024px 与 1440px 下打开一篇
   同时含中文、长 URL、宽表格的笔记，检查表格只在自身容器内滚动、中文行宽不再过窄。

## 6. 明确不做的事

- 不加保存/新建动画。DESIGN.md 规定"No idle animation"，`docs/ask-ui-review.md`
  也对高频动作明确不加动效。状态用页脚文字表达（`shell/native.go:2099` 已有
  "Saving…" / "Saved HH:MM:SS"）。
- 不改原生笔记行高。`rowHeight = 36`（`shell/design.go:174`）与 DESIGN.md 的
  "compact and title-only" 一致，外部反馈里"约 40px 偏高"的数字也与代码不符。
- 不重排浏览器版的列表密度；如果 `web/` 最终保留，可以单独再议。
