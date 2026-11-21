# kbcli

> **简体中文** | [English (default)](README.md)

本文亦有英文版：[README.md](README.md)（英文为默认语言）。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![Go 1.27.4](https://img.shields.io/badge/Go-1.27.4-00ADD8)](https://go.dev/)

用一个 Go 二进制文件即可与本地文档目录对话——无服务端、无向量数据库、无云账号。

`kbcli` 把一个目录变成一个可携带的 `.kbvec` 向量文件，并打开三视图终端界面：两栏聊天、设置界面、索引确认对话框。默认即可离线使用。默认 embedder 是零依赖的进程内哈希模型。聊天后端不可达时，答案会退化为带 `[n]` 引用的确定性抽取式答案。索引永远是显式命令。启动时不扫描、不建索引。

聊天视图示意图（这是示意图，不是截图）：

```
+-------------------------------+-------------------------------+
| Conversation                  | Ranked evidence               |
|                               |                               |
| you > how is retry handled?   | [1] docs/retry.md             |
|                               | [2] docs/api.md               |
| kbcli > Retries use backoff   | [3] docs/config.md            |
| with jitter [1][2].           |                               |
+-------------------------------+-------------------------------+
| > :index ./docs                                              |
+---------------------------------------------------------------+
```

## 为什么选择 kbcli

- **一个可携带文件，零基础设施。** 一个目录只生成一个 `.kbvec` 二进制向量文件。无服务端、无向量数据库、无外部服务。移动或复制该目录，索引随之一起走。
- **默认离线，而非可选项。** 默认 embedder 是零依赖的进程内哈希模型。聊天后端不可达时，答案退化为带 `[n]` 引用的确定性抽取式答案。所有依赖 LLM 的检索阶段失败时都会回退到普通检索。
- **绝不在后台悄悄索引磁盘。** 启动时不扫描、不向上级目录查找、不建索引。`:index` 先做有界的只读预检（20000 个文件 / 12 层目录），再要求两步目录确认，之后才会写入。

## 功能

- 每个目录一个可携带的 `.kbvec`：magic 头 `KBCLI\x00`、gob 载荷、经 `os.Rename` 原子替换。旧版 `.kbcli.kbvec` 仍可加载。embedder 域隔离会拒绝加载由不同 embedder 构建的索引。
- 两种 embedder，均为 384 维并做 L2 归一化：零依赖的进程内哈希模型（FNV-1a 词一元语法 + 字符 3-gram），以及可选的纯 Go `rembed` 神经模型。
- 支持 OpenAI 兼容 Chat Completions 与 Anthropic Messages API 两类聊天后端（`chat_provider`、`base_url`、`model`、`api_key`）。后端不可达时，答案退化为依据排名最高证据生成的抽取式答案，并带 `[n]` 引用。
- 源自 Porto 的检索：`vector` / `bm25` / 加权 RRF `hybrid`（k=60），`cross_encoder` 或 `llm` 重排，`hyde` / `multi_query` / `decomposition` / `step_back` 查询变换，指代消解 condense 门，以及 kbcli 自有的 `min_score` 与 `context_expand`。
- 三个全屏视图，基于 `bubbletea` + `bubbles` + `lipgloss`。主题跟随终端背景（OSC 11 或 `COLORFGBG`）：两栏聊天（左侧会话、右侧排名证据）、主从式设置界面（分组栏 + 详情栏）、索引确认对话框。
- 双语界面与提示词，英文 / 简体中文，由 `language`（`en`|`zh`）选择。英文与原有界面逐字节一致。
- 每工作区会话记忆：`~/.kbcli/sessions/<sha256(root)[:16]>.json`（模式 `0600`）。含 4 类事实（`user_decision`、`user_preference`、`project_context`、`open_question`）、Jaccard 去重、上限淘汰、历史压缩，每轮注入。
- 设置目录恰好有 40 个字段，分 6 组（Foundation、Ingestion、Retrieval、Rerank、Query Transform、Session）。其中恰好 21 个是数值型（`FieldInt` 或 `FieldFloat`）。支持稀疏的工作区覆盖，INDEX STALE 横幅由 version-2 构建签名驱动。

## 安装

需要 Go 1.27.4。

```sh
make build      # bin/kbcli
make install    # go install ./cmd/kbcli
```

无 CGO、无 Python、无服务端。产物是单一二进制文件。

### 远程安装

使用 `go install` 直接安装已发布的版本：

```sh
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.1.1
# 或
go install github.com/ProjAnvil/kbcli/cmd/kbcli@latest
```

请使用 `go install`，而不是 `go get`：自 Go 1.17 起，`go get` 只修改 `go.mod`，不会安装二进制文件。二进制文件位于 `$(go env GOPATH)/bin/kbcli`；可设置 `GOBIN` 来覆盖该位置。该目录必须在你的 `PATH` 中。

模块路径使用组织名称的大小写形式 `ProjAnvil`。Go 模块路径区分大小写，因此请严格按上面所写使用 `github.com/ProjAnvil/kbcli`。

## 快速开始

```sh
kbcli index ./docs
cd ./docs && kbcli
kbcli search "retry policy" --index ./docs/.kbvec
kbcli config set api_key sk-...
```

在 TUI 中输入 `:index <dir>` 可为另一个目录建索引（需两步确认）。

启动时不建索引。当前目录存在 `.kbvec` 时，聊天立即开始。否则聊天栏会提示输入 `:index <dir>`，或运行 `kbcli index <dir>`。启动时不做任何扫描，也不向上级目录查找。

CLI 子命令：`index`、`reindex`、`update`、`config`、`settings`、`search`、`version`。TUI 内命令：`:reindex`、`:update`、`:settings`、`:stale`、`:clear`、`:help`、`:index`、`:q`（输入行为空时也可按 `q`）。`q`、`esc`、`ctrl+c` 均可退出。

## 配置

全局设置存放于 `~/.kbcli/config.yaml`（模式 `0600`）。工作区覆盖是稀疏的，存放于 `~/.kbcli/settings.json`：工作区继承 `config.yaml`，只覆盖你设定的键。

```sh
kbcli config keys              # 全部 40 个键，含分组、类型与范围
kbcli config list              # 生效值，密钥已遮蔽
kbcli config get top_k
kbcli config set top_k 9
kbcli config path              # ~/.kbcli/config.yaml
kbcli settings show            # 生效配置 + 索引状态
kbcli settings set top_k 9     # 一个稀疏的工作区覆盖
```

`chat_provider` 选择聊天 API 协议：`openai`（默认，OpenAI 兼容的 Chat Completions）或 `anthropic`（Anthropic Messages API）。`base_url` 必须指向与所选协议兼容的接口。使用 Anthropic 官方 API 时，请将 `base_url` 留空，或设为 `https://api.anthropic.com/v1`——agent 会自动回退到与协议匹配的默认地址（默认的 OpenAI `base_url` 仅适用于 `openai`）。

[设置界面](#settings-screen)（`:settings`）在 TUI 中编辑同一份目录。修改会影响索引的键——`chunk_size`、`chunk_overlap`、`embedder`、`embed_model`——会记入 version-2 构建签名。加载时发现偏差会显示 **INDEX STALE** 横幅。运行 `:reindex`（或 `kbcli reindex`）重建索引。

### 分块大小与重叠

kbcli 的默认值现在与 LangChain 的 `RecursiveCharacterTextSplitter` 字符切分默认值一致：`chunk_size: 4000`、`chunk_overlap: 200`。kbcli 按 **Go rune 计数**，即 Unicode 码点，相当于 LangChain 中 Python `len()` 的计数方式，而非 token。

也存在基于 token 的切分器，它们使用特定模型的 tokenizer；kbcli 的 rune 设置是面向用户、与模型无关的对应方式。如果选择 `embedder: rembed`，kbcli 还会在切分时把分块限制在该模型的 token 上限内——rune 配置仍是面向用户的设置。

`chunk_size` 与 `chunk_overlap` 都会影响索引：修改任一项都会使 `.kbvec` 索引失效，必须重建索引（`:reindex` / `kbcli reindex`）。请用 recall@k 指导调整。

背景参考：[LangChain text splitters](https://python.langchain.com/docs/concepts/text_splitters/)。

<a id="settings-screen"></a>

### 设置界面

`:settings` 打开主从视图：左侧为分组栏，右侧为详情栏。目录恰好有 **40 个字段**、**6 组**——Foundation、Ingestion、Retrieval、Rerank、Query Transform、Session——其中恰好 **21** 个为数值型（`FieldInt` 或 `FieldFloat`）。只有你标记的字段会写成稀疏覆盖，其余字段保留全局值。当已加载索引的构建设置不同时，顶部显示 **INDEX STALE** 横幅。按 `r`（或 `:reindex`）就地重建。

### 嵌入模型与重建索引

默认的本地 embedder 是纯 Go 哈希模型：不下载任何内容，完全离线可用。选择 `rembed` 神经嵌入模型后，首次使用会下载权重，之后复用本地缓存。`rembed.Load` 不提供字节级进度，因此 kbcli 只显示阶段级 spinner（`加载模型` → `扫描` → `建索引` → `保存`），而不是百分比进度条。重建索引或加载模型期间，全屏模态会阻止聊天输入；当前重建完成前无法启动第二次重建。

## 工作原理

`ingest` -> `chunk` -> `embed` -> `.kbvec` -> `retrieval` -> `agent` -> TUI。目录被遍历并切分，每个分块被嵌入为 384 维向量，整个索引写成一个 `.kbvec` 文件。查询时检索对证据排名，agent 组装答案，TUI 渲染聊天栏与证据栏。

```
cmd/kbcli/         entry point
internal/config/   global config.yaml and the field catalog
internal/settings/ sparse per-workspace overrides
internal/ingest/   per-format loaders and splitters
internal/embed/    hashing and neural embedders (both 384-dim)
internal/index/    .kbvec format, store, search
internal/scan/     bounded read-only preflight
internal/retrieval/ BM25, weighted RRF, rerank, query transforms, condense gate
internal/agent/    OpenAI-compatible chat, evidence, fallback answers
internal/session/  per-workspace memory and facts
internal/i18n/     English / Simplified Chinese catalogs
internal/tui/      three-view bubbletea TUI (chat, settings, confirmation)
internal/cli/      cobra commands
internal/app/      orchestration shared by CLI and TUI
```

## 平台兼容性

所有文件系统路径都用 Go 标准库辅助函数构造：`os.UserHomeDir`、`os.UserCacheDir`、`filepath.Join`、`filepath.Clean`、`filepath.VolumeName`、`filepath.Separator`、`runtime.GOOS`、`os.Getenv`。没有硬编码 POSIX 路径。整棵代码树可交叉编译到 Windows、macOS、Linux，且禁用 CGO：

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./...
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

Windows 运行时未在真机验证。目前只有交叉编译和静态检查。

## 文档

- [简体中文 README](README.zh-CN.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CHANGELOG.md](CHANGELOG.md)
- [SECURITY.md](SECURITY.md)

## 参与贡献

```sh
make test
make race
make vet
```

参见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

MIT。参见 [LICENSE](LICENSE)。
