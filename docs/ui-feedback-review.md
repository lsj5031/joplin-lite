# UI 反馈核对与改进清单

本文核对一份外部 UI 反馈（6 点），并补充代码与规范中发现的同类问题。
每条结论都标注了依据的文件与行号。未能复现的地方明确写出。

**范围与限制**

- 反馈中引用的内容（"Goodest – Only goodest design ins…"、"可能对肾脏…" 等）在仓库、
  `docs/screenshots/` 与测试数据中均找不到，无法复现。若它来自用户自己的笔记库，需要
  提供截图或笔记内容才能定位。
- 本次只做了源码与规范核对，没有渲染 web 或原生界面。CI 之外无法运行构建与测试。
- 原生 Markdown 渲染由 MyGO 提供（`tmp/mygo-patched-v0.9.0/docs/ui/markdown.md`），
  其表格宽度行为未在本次核对中确认。

## 总体判断

方向基本同意，但有三点与现状或规范不符：

1. 第 4 点的 "行高约 40px" 不符合现状：原生行最小 36px，web 行最小 84px。原生列表按
   DESIGN.md 本就是紧凑、只显示标题的设计，密度是有意为之。
2. 第 2 点的问题主要是字号偏小，而不是颜色对比度不足。
3. 第 5 点中，保存状态反馈已经存在（只是纯文字）；真正缺失的是搜索命中高亮。

项目规范（DESIGN.md "No idle animation"，docs/ask-ui-review.md 对高频动作不加动画）
限制了动效类反馈，建议优先用文字与颜色表达状态。

## 一、逐条核对

| # | 反馈 | 判断 | 依据 |
| --- | --- | --- | --- |
| 1 | 表格右侧内容被裁切 | 部分同意（web 可确认，原生未确认） | 见下文 1.1 |
| 2 | 次要文字太淡 | 同意，但问题是字号，颜色对比度基本达标 | 见下文 1.2 |
| 3 | 列表标题被截断，信息丢失 | 同意（web） | `web/src/components/NoteList.tsx:113,117` |
| 4 | 列表密度偏高，行高约 40px | 不同意现状描述 | `shell/design.go:174`，`web/src/styles/app.css:146` |
| 5 | 缺少状态反馈（搜索高亮、保存、新建） | 部分同意 | 见下文 1.5 |
| 6 | 中文排版：字重差异、列宽、标点对齐 | 同意，并能说出具体原因 | 见下文 1.6 |

### 1.1 表格裁切（第 1 点）

> 更正：本节初版判断有误。核对 MyGO 实现后，原生表格不会裁切，而是可以横向滚动。
> 具体修复方案见 `docs/ui-review-plan.md` 的 N1、W1。

- 原生：MyGO 的表格（`ui/markdown/table.go`）已经把表格放进 `ScrollHorizontal`，
  列宽上限是 `TableMaxColumn = 24em`，超出会换行。但 `prepareMarkdownOptions()`
  没有覆盖这个默认值，正文 17px 时一列最宽约 408 DIP，而编辑器测量宽度是 700 DIP，
  所以**两列宽表格就会超出**、需要横向滚动——内容没丢，只是滚动条在 macOS 上平时
  不可见，看起来像被裁掉。
- web：`.prose-note table`（`web/src/styles/app.css:299-310`）没有独立的滚动容器，
  单元格也没有 `overflow-wrap`。长 URL 会把表格撑出正文栏，进而让整个编辑区横向滚动，
  表头和首列一起滑走。这一点仍是真实问题。

### 1.2 次要文字偏淡（第 2 点）

- 颜色：`--color-ink-soft #606F5A`（`web/src/styles/app.css:16`）对纸面 `#F9FAF5` 约为
  5.1:1，符合 AA；对侧栏 sage 底色 `#E9EEE3` 约为 4.5:1，已贴近下限。
- 字号才是主要问题：
  - web 笔记行：标题 14px，摘要 12px，元信息 11.5px，时间 10px
    （`app.css:148`，`NoteList.tsx:113,117,119`）。
  - native：`sizeLabel` 11、`sizeMonoSm` 10（`shell/design.go:130-133`）。
  - DESIGN.md 把次要标签规定为 11–12 DIP，所以问题出在规范的下限本身偏小。
- 十几种中文字号搭配在一起时，小字更难读。

### 1.3 列表标题截断（第 3 点）

- web：标题与摘要都是 `truncate`（`NoteList.tsx:113,117`），标题元素没有 `title` 属性，
  完整标题无法通过悬停查看。
- native：`noteRow` 的标题文本（`shell/workspace.go:1295` 起）是否截断，本次未确认。

### 1.4 密度（第 4 点）

- native：行高 `MinHeight(rowHeight)`，`rowHeight = 36`（`shell/design.go:174`，
  `shell/workspace.go:1204`）。
- web：`.note-row { min-height: 84px }`（`app.css:146`），窄屏 108px（`app.css:181`）。
- DESIGN.md："Native note rows are compact and title-only ... the browser retains taller
  excerpt rows." 原生的紧凑是规范要求。
- web 行确实偏满（三行元信息加标题与摘要），可以考虑只在 web 上调整。

### 1.5 状态反馈（第 5 点）

- 已有：
  - native 页脚：`statusText()` 返回 "Saving…" / "Saved 15:04:05" / 错误信息
    （`shell/native.go:2099-2110`）。
  - web：`StatusStrip` 显示 "Saving locally…" / "Saved locally"（`web/src/components/StatusStrip.tsx`）。
- 缺失：
  - 搜索命中高亮：在 web 与 native 代码中都找不到高亮实现（已搜索 `highlight`、`<mark>`）。
    这是真正的缺口。
  - 新建笔记的入口反馈：未找到相关实现。
- 保存动画：DESIGN.md 规定没有空闲动画，状态需要带文字。建议用文字与小圆点表达，
  不加动画。

### 1.6 中文排版（第 6 点）

每一条都能在代码中找到原因：

- `<html lang="en">`（`web/src/routes/__root.tsx:49`）：中文内容会按英文规则选择 CJK 字形，
  常见结果是字形与预期字体不一致，字重观感不同。
- web 字体栈没有中文字体（`app.css:4-7`）。
- native 的 Overpass 只包含拉丁字符（`shell/fonts/`），中文字符由系统回退。
  回退字体的字重与拉丁字符不一致，正符合 "字重差异明显" 的描述。
- `.prose-note { max-width: 72ch }`（`app.css:189`）：`ch` 以数字 "0" 的宽度计算，
  而中文字约为 2ch 宽，所以一行中文只有约 36 个字，远窄于英文。DESIGN.md 规定的是 700 CSS px。
- 表格单元格（`app.css:305-310`）没有最小或最大宽度，也没有换行策略，中文列宽分配随内容变化。
- 中英文之间没有间距处理。

## 二、补充发现的类似问题

1. **表格溢出无提示**（同 1.1）。单元格缺少 `overflow-wrap`，表格缺少宽度约束与滚动容器。
2. **web 元信息字号层级过多**：14 / 12 / 11.5 / 11 / 10px 混用（`app.css:148`，`NoteList.tsx:113-119`）。
   建议收敛为三级：标题 14px，摘要与元信息 12px，时间与计数 11px 等宽字。
3. **列表标题样式互相冲突**：`NoteList.tsx:49` 的 `h2` 写了 `text-[18px]`，
   而 `app.css:144` 的 `.note-list > header h2` 写了 14px。实际生效哪个取决于 CSS 层叠，
   需要渲染确认。应只保留一处定义。
4. **原生 Ask 回答字号偏小**：Ask 的 Markdown 正文用 `sizeUI`（13）、行高 1.5
   （`shell/ask_markdown.go:27-28`），比编辑器正文的 17 小很多。长回答阅读吃力。
5. **标题不可查看全文**：web 与 native 都没有悬停显示完整标题的途径。
   web 的标题元素可以直接加 `title` 属性，成本最低。

## 三、建议的改动（按优先级）

**P1：影响阅读，改动小**

- 表格：给 `.prose-note table` 外包一层 `overflow-x: auto` 的容器，并给 `td`/`th`
  加 `overflow-wrap: anywhere`。包裹容器最好在 markdown-it 的渲染规则里加，
  而不是给 `table` 改 `display: block`（那会破坏表格布局）。
- 中文字体：在 `--font-sans` 中加入 `'PingFang SC', 'Microsoft YaHei'`，
  让中文字形有确定的字体，减少字重差异。
- 正文宽度：`.prose-note` 的 `max-width: 72ch` 改为 `700px`，与 DESIGN.md 及编辑器宽度一致。
- `<html lang>`：至少在内容为中文时设置 `lang="zh-CN"`。最简单的做法是按设置或内容判断，
  不必做逐笔记识别。

**P2：提高信息量与层级**

- web 列表标题加 `title` 属性；标题可以允许两行（`line-clamp: 2`），仅限 web 行。
- 收敛字号到三级（见第二节第 2 点），次要文字不低于 12px，sage 底色上保证对比度。
- 删除或合并重复的标题字号规则（见第二节第 3 点）。
- Ask 回答的正文字号与编辑器对齐或至少提高到 14–15。

**P3：状态与反馈**

- 搜索命中高亮：web 在标题与摘要中用 `<mark>`，native 用 violet 底色标出命中文字。
  native 的 MyGO 文本组件是否支持片段着色，需要先查 `tmp/mygo-patched-v0.9.0/docs/ui/` 确认。
- 新建笔记：创建后把焦点移到标题，并在页脚显示一行文字 "New note created"。不加动画。
- 行高：保持原生紧凑设计不变（DESIGN.md 已规定），只对 web 行做调整。

## 四、如何验证

- web：用 `web/` 的开发服务器，在 1024px 与 1440px 宽度下打开一篇同时含中文、
  长 URL 与宽表格的笔记，分别检查表格裁切、中文字形与行宽。
- native：在 `shell/screens_test.go` 的截图流程中加入一篇中文宽表格笔记与一个长标题，
  生成 PNG 后目视检查。截图是合成数据，只能说明布局，不能代替 Mac 与 Windows 上的实机检查。
- 以上验证都需要在本机或 CI 中运行，本次核对未执行。
