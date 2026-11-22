# kbcli

> **简体中文** | [English (default)](README.md)

本文亦有英文版：[README.md](README.md)（英文为默认语言）。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![Go 1.27.4](https://img.shields.io/badge/Go-1.27.4-00ADD8)](https://go.dev/)

用一个约 26–30 MB 的 Go 二进制文件即可与本地文档目录对话——无服务端、无向量数据库、无云账号、无常驻进程。把二进制放进 `PATH`，运行 `kbcli`，TUI 就直接打开了：不用手写配置文件、不用开端口、不用另起一个 Web 界面。

`kbcli` 把一个目录变成一个可携带的 `.kbvec` 向量文件，并打开四视图终端界面：两栏聊天、设置界面、索引确认对话框，以及首次运行的 API key 向导。默认即可离线使用。默认 embedder 是零依赖的进程内哈希模型。聊天后端不可达时，答案会退化为带 `[n]` 引用的确定性抽取式答案。索引永远是显式命令。启动时不扫描、不建索引。

## 轻到可以直接用

- **一个静态二进制，26–30 MB。** 使用 `CGO_ENABLED=0` 与 `-s -w` 构建。v0.3.0 普通档体积为 26.3 MB（linux/arm64）～30.3 MB（darwin/amd64）。无运行时、无 Python、无 cgo、无容器、无安装器。
- **TUI 就是整个产品。** 运行 `kbcli` 即可打开两栏聊天（左会话、右排名证据）、包含全部 49 个设置的 `:settings`、`:reindex`、多窗口标签栏，以及首次运行的设置向导。不需要再跑别的东西；无浏览器、无 localhost 端口、无独立前端。
- **两条命令就能用。** `kbcli index ./docs`，然后 `kbcli`。索引永远是显式命令；启动时不扫描、不向上级目录查找、不在后台偷偷建索引。
- **每个目录一个 `.kbvec` 文件。** 复制目录，索引跟着一起走。无需开通、迁移、备份或维护数据库。
- **上手不需要下载任何东西。** 默认 embedder 是零依赖的进程内哈希模型，没有权重文件。本地 cross-encoder 重排模型只会拉取一次到 `~/.kbcli/model`（TUI 启动屏显示进度），或直接打包在 `full` 完整档（约 92 MB）里，供离线环境使用。
- **试用不需要账号。** 首次运行的 API key 向导可以按 `ctrl+s` 跳过。聊天后端不可达时，答案会退化为带 `[n]` 引用的确定性抽取式证据，而不是报错。
- **不用改 YAML。** 所有设置都在 TUI 的 `:settings` 里，工作区只保存稀疏覆盖。SSH 里、tmux 窗格里、断网的笔记本上都能用。

## 本项目用到的 ProjAnvil 项目

`kbcli` 用到了两个 ProjAnvil 项目。

| 项目 | 用途 |
| --- | --- |
| [langchain-golang](https://github.com/ProjAnvil/langchain-golang) | 直接依赖的 Go 模块：`github.com/projanvil/langchain-golang v0.14.3`。`internal/agent` 用它的模型适配层、消息类型和工具接口。`internal/agent` 还用 `deepagents` 与 `langgraph/prebuilt` 运行聊天 agent。`internal/ingest/markdown.go` 用 `core/documents` 和 `textsplitters` 切分 Markdown。 |
| [Porto](https://github.com/ProjAnvil/Porto) | Porto 是 Python 项目，不是 Go 依赖。`kbcli` 把它的 RAG 算法移植成 Go。`internal/retrieval` 包含 BM25、加权 RRF、查询变换、重排与引用。`internal/intent` 实现意图路由。`internal/session` 实现工作区事实与摘要记忆。 |

构建时和运行时，`kbcli` 都不引入其他 ProjAnvil 仓库。外部依赖见 `go.mod`。

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
- 支持 OpenAI 兼容 Chat Completions 与 Anthropic Messages API 两类聊天后端（`chat_provider`、`base_url`、`model`、`api_key`）。首次运行的 TUI 向导会收集协议、遮蔽显示的 API key、接口地址与模型，做一次短连通性检查，并就地重建 agent；按 `ctrl+s` 可跳过，供离线使用。后端不可达时，答案退化为依据排名最高证据生成的抽取式答案，并带 `[n]` 引用。
- 源自 Porto 的检索：`vector` / `bm25` / 加权 RRF `hybrid`（k=60），**默认启用本地 `aikit` cross-encoder 重排**（无需服务、Python 或 cgo）。较宽的 `candidate_k` 候选池（默认 40）先喂给重排，`min_score` 在归一化后的重排分数上执行，改写问题的同时还会用原始问题再召回一次；grounded prompt 与 `kb_search` 共用一套引用编号。检索为空或失败时明确拒答，而不是凭空编造。外部 HTTP `cross_encoder` 与 `llm` 重排仍可配置。`hyde` / `multi_query` / `decomposition` / `step_back` 查询变换、指代消解 condense 门，以及 kbcli 自有的 `context_expand` 同样可用。
- 源自 Porto 的意图路由：`direct` 跳过检索，`quick_rag` 直接检索原问题，`deep_rag` 走完整管线。意图 provider 默认继承全局配置。`why?` 之类的短问不再绕过知识库。
- 结构化切分：Strategy + Registry 按文件后缀选择切分器（Markdown 标题、CSV 行组、HTML 标题、PDF 页、DOCX 章节），使一个分块尽量落在同一节内；`chunk_strategy: fixed` 可恢复旧版单一尺寸切分。
- 四个全屏视图，基于 `bubbletea` + `bubbles` + `lipgloss`。主题跟随终端背景（OSC 11 或 `COLORFGBG`）：两栏聊天（左侧会话、右侧排名证据；`ctrl+]` 收起证据栏）、主从式设置界面（分组栏 + 详情栏）、索引确认对话框，以及首次运行的 API key 向导。
- 双语界面与提示词，英文 / 简体中文，由 `language`（`en`|`zh`）选择。英文与原有界面逐字节一致。
- 每工作区会话记忆：`~/.kbcli/sessions/<sha256(root)[:16]>.json`（模式 `0600`）。含 4 类事实（`user_decision`、`user_preference`、`project_context`、`open_question`）、Jaccard 去重、上限淘汰、历史压缩，每轮注入。
- 设置目录恰好有 49 个字段，分 7 组（Foundation、Ingestion、Retrieval、Rerank、Query Transform、Intent、Session）。其中恰好 23 个是数值型（`FieldInt` 或 `FieldFloat`）。支持稀疏的工作区覆盖，INDEX STALE 横幅由 version-2 构建签名驱动。

## 安装

需要 Go 1.27.4。

```sh
make build      # bin/kbcli
make install    # go install ./cmd/kbcli
```

无 CGO、无 Python、无服务端。产物是单一二进制文件。

### 预编译二进制

每个 GitHub release 都为各平台发布两档压缩包。资产名包含操作系统与架构；完整档（full）的名称还包含 `full`。

| 档位 | 资产名 | 内容 |
| --- | --- | --- |
| 普通 | `kbcli-<version>-<os>-<arch>`（Windows 为 `kbcli-<version>-windows-amd64.exe`） | 仅二进制；重排模型在首次运行时下载。 |
| 完整 | `kbcli-full-<version>-<os>-<arch>.tar.gz`（Windows 为 `kbcli-full-<version>-windows-amd64.zip`） | 二进制加上 `model/ms-marco-MiniLM-L-6-v2/` 及全部五个模型文件；完全离线。 |

解压后从解压目录运行二进制（请将示例名称换成你的版本与平台）：

```sh
tar -xzf kbcli-full-v0.3.0-darwin-arm64.tar.gz   # Windows：unzip kbcli-full-v0.3.0-windows-amd64.zip
./kbcli
```

完整档包含：

```
kbcli                                  # Windows 为 kbcli.exe
model/ms-marco-MiniLM-L-6-v2/
  config.json
  model.safetensors
  tokenizer.json
  tokenizer_config.json
  vocab.txt
```

完整档首次运行时，kbcli 会把可执行文件目录下的 `model/` 复制到 `~/.kbcli/model/`，无需网络。每个 release 还会上传 `checksums.txt`，可用它校验下载：

```sh
sha256sum -c checksums.txt --ignore-missing
# macOS：shasum -a 256 -c checksums.txt --ignore-missing
```

### 远程安装

使用 `go install` 直接安装已发布的版本：

```sh
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.3.0
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

CLI 子命令：`index`、`reindex`、`update`、`config`、`settings`、`search`、`version`。TUI 内命令：`:reindex`、`:update`、`:settings`、`:stale`、`:clear`、`:help`、`:index`、`:q`（输入行为空时也可按 `q`）。`q` 和 `ctrl+c` 退出；`esc` 用于取消正在生成的回答，不再退出。

回答以流式方式逐字进入聊天栏。从按下 `enter` 到首个 token 出现之间（含意图路由、检索、重排与生成），思考图标会持续转动。这一过程中的任意时刻按 `esc` 即可取消整个请求；被取消的请求不会把助手回复或降级文案写入会话。工具轮的前导文字会从实时文本中清除，只有最终回答那一轮会进入聊天记录。

## 配置

全局设置存放于 `~/.kbcli/config.yaml`（模式 `0600`）。工作区覆盖是稀疏的，存放于 `~/.kbcli/settings.json`：工作区继承 `config.yaml`，只覆盖你设定的键。

```sh
kbcli config keys              # 全部 49 个键，含分组、类型与范围
kbcli config list              # 生效值，密钥已遮蔽
kbcli config get top_k
kbcli config set top_k 9
kbcli config path              # ~/.kbcli/config.yaml
kbcli settings show            # 生效配置 + 索引状态
kbcli settings set top_k 9     # 一个稀疏的工作区覆盖
```

`chat_provider` 选择聊天 API 协议：`openai`（默认，OpenAI 兼容的 Chat Completions）或 `anthropic`（Anthropic Messages API）。`base_url` 必须指向与所选协议兼容的接口。使用 Anthropic 官方 API 时，请将 `base_url` 留空，或设为 `https://api.anthropic.com/v1`——agent 会自动回退到与协议匹配的默认地址（默认的 OpenAI `base_url` 仅适用于 `openai`）。

### 首次运行的 API key 设置

当未配置 `api_key` 且向导尚未被跳过时，kbcli 会在进入聊天前打开全屏向导。依次填写 `chat_provider`（左右键切换，`enter` 确认）、遮蔽显示的 `api_key`、`base_url` 与 `model`；`enter` 前进，`esc` 返回，最后一步按 `enter` 保存。kbcli 会校验配置、对 `base_url` 做一次 5 秒 HTTP 探测（任何 HTTP 响应都视为可达），以 `0600` 模式写入 `~/.kbcli/config.yaml`，并重建 agent、retriever 与会话，使新密钥无需重启即可生效。探测失败只会追加一条警告，配置仍会保存。`ctrl+s`（第一步也可按 `s`）跳过向导并记录 `setup_done: true`，保持完全离线。也可以直接用 `kbcli config set api_key sk-...` 让向导不再出现。

重排**默认开启**：`rerank_enabled: true`、`rerank_type: aikit`、`rerank_model: ms-marco-MiniLM-L-6-v2`。当 `rerank_model_path` 为空时，模型位于受管目录 `~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`。普通发行版首次运行时，kbcli 会检查该目录；若缺少五个模型文件，会打开带 ASCII 标志和字节级进度条的启动界面，同时下载它们（约 92 MB）。下载或复制完成前无法开始聊天。将 `rerank_model_path` 设为已有的本地模型目录即可跳过该界面。详见[本地 cross-encoder 重排](#本地-cross-encoder-重排)，以及外部 HTTP 与 LLM 选项。

`intent_mode` 选择路由模式：`off`、`binary`（`direct` 或 `rag`）、`adaptive`（`direct`、`quick_rag`、`deep_rag`）。默认值为 `adaptive`。`direct` 跳过检索，只做简短回答。`quick_rag` 用原问题检索，不做事指消解与查询变换。`deep_rag` 走完整管线。

意图分类器由 `intent_provider`、`intent_api_key`、`intent_base_url`、`intent_model` 配置。默认的 `intent_provider: inherit` 复用全局聊天协议、接口、模型与密钥。其余三个字段留空时继承全局值。分类器不可用时回退到关键词规则，再回退到完整 RAG 路径。

[设置界面](#settings-screen)（`:settings`）在 TUI 中编辑同一份目录。修改会影响索引的键——`chunk_size`、`chunk_overlap`、`section_size`、`chunk_strategy`、`embedder`、`embed_model`——会记入构建签名（现在还带 `per-ext-v1` 切分器版本）。加载时发现偏差会显示 **INDEX STALE** 横幅。运行 `:reindex`（或 `kbcli reindex`）重建索引。`candidate_k` 等查询时参数不会让索引失效。

### 按文件类型切分

按 4000 rune 硬切对纯文本没问题，但会把结构化文件里互不相关的章节混进同一个分块。默认的 `chunk_strategy: auto` 通过 Strategy + Registry 按后缀选择结构化切分器；设为 `fixed` 可对所有格式强制使用旧版单一尺寸切分。

| 后缀 | 切分策略 | 尺寸上限 |
|---|---|---|
| `.md`、`.markdown`、`.mdx` | Markdown 标题（h1–h4），标题路径会注入嵌入文本 | `section_size` |
| `.txt`、`.text` | 纯文本递归切分 | `chunk_size` |
| `.csv` | 行组 | `section_size` |
| `.html`、`.htm` | HTML 标题 | `section_size` |
| `.pdf` | 单页 | `section_size` |
| `.docx` | 转换后的 Markdown 章节 | `section_size` |

`chunk_size: 4000` 与 `chunk_overlap: 200` 仍与 LangChain 的 `RecursiveCharacterTextSplitter` 默认值一致，但 `chunk_size` 现在**只限制纯文本分块**。结构化格式按节由 `section_size` 限制（默认 1000 rune）；过长的节会被递归切分，而不是与相邻节合并。kbcli 按 **Go rune 计数**，即 Unicode 码点，相当于 Python 的 `len()`，而非 token。如果选择 `embedder: rembed`，切分时还会把分块限制在该模型的 token 上限内。

### 本地 cross-encoder 重排

重排**默认开启**：`rerank_type: aikit` 通过
[`github.com/townsendmerino/aikit`](https://github.com/townsendmerino/aikit)
v1.78.0 在进程内运行 cross-encoder。它不需要 HTTP 服务、Python、ONNX Runtime 或 cgo；该 API 属于 Experimental，因此 kbcli 固定依赖版本。

**受管模型（默认）。** 当 `rerank_model_path` 为空时，kbcli 使用 `~/.kbcli/model/<rerank_model>`，默认为 `~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`。普通发行版首次运行时，kbcli 会检查该目录下的五个文件；若缺少任意一个，就会打开带 ASCII 标志和字节级进度条的启动界面，同时下载（约 92 MB，Hugging Face 修订版 `233902d25c440f23af6f7d6e94d2946bac0bee0a`）。下载或复制完成前无法开始聊天。[完整离线发行档](#预编译二进制)附带同样的五个文件，因此其首次运行只需复制到受管目录——无需网络。

**外部本地模型。** 将 `rerank_model_path` 设为包含 `config.json`、`model.safetensors` 和 tokenizer 文件的目录；kbcli 会将其视为外部管理并跳过下载：

```sh
kbcli config set rerank_enabled on
kbcli config set rerank_type aikit
kbcli config set rerank_model_path /path/to/ms-marco-MiniLM-L-6-v2
```

如需自行定制，也可手动下载参考模型：

```sh
huggingface-cli download cross-encoder/ms-marco-MiniLM-L-6-v2 \
  config.json model.safetensors tokenizer.json tokenizer_config.json vocab.txt \
  --local-dir /tmp/ms-marco-MiniLM-L-6-v2
```

**其他重排器。** `rerank_type: cross_encoder` 使用外部 HTTP cross-encoder，由 `rerank_base_url`、`rerank_model` 和 `rerank_api_key` 配置；`rerank_type: llm` 使用 LLM 重排器。两者仍可配置，但都需要你运行相应服务。

所有重排设置都只在查询时生效，不会让 `.kbvec` 索引失效。加载或打分失败时会 fail open，保留原始检索顺序。

`chunk_size`、`chunk_overlap`、`section_size` 与 `chunk_strategy` 都会影响索引：修改任一项都会使 `.kbvec` 索引失效，必须重建索引（`:reindex` / `kbcli reindex`）。`candidate_k` 只在查询时生效。请用 recall@k 指导调整。

背景参考：[LangChain text splitters](https://python.langchain.com/docs/concepts/text_splitters/)。

<a id="settings-screen"></a>

### 快捷键与窗口

聊天界面用 `ctrl+x` 作为窗口前缀，参照 Emacs 的 `C-x` 约定。不用 `ctrl+w`、`ctrl+a`、`ctrl+b`，因为这些键已被输入框用于编辑。按下前缀后再按：

| 按键 | 操作 |
|---|---|
| `ctrl+x t` | 新建窗口 |
| `ctrl+x n` / `ctrl+x p` | 下一个 / 上一个窗口 |
| `ctrl+x 0` … `ctrl+x 8` | 跳到编号窗口 |
| `ctrl+x x` | 关闭当前窗口 |
| `ctrl+x w` | 打开窗口选择器 |

帮助行上方有标签栏，高亮当前窗口。`ctrl+x w` 打开浮层选择器。用 `up`/`down` 选择，`enter` 进入，`esc` 取消。

每个窗口各自保存会话、证据与会话记忆。前缀 750 毫秒后超时。前缀后的按键不会进入输入框。`ctrl+c` 仍然退出。

切换标签不会中断正在运行的请求。标签栏为每个标签预留一个固定字符位用于转动图标：即使该标签在后台运行，也会持续显示转圈，因此请求开始或结束时标签栏宽度不会跳动。切回该标签后按 `esc` 即可取消。

### 设置界面

`:settings` 打开主从视图：左侧为分组栏，右侧为详情栏。目录恰好有 **49 个字段**、**7 组**——Foundation、Ingestion、Retrieval、Rerank、Query Transform、Intent、Session——其中恰好 **23** 个为数值型（`FieldInt` 或 `FieldFloat`）。只有你标记的字段会写成稀疏覆盖，其余字段保留全局值。当已加载索引的构建设置不同时，顶部显示 **INDEX STALE** 横幅。按 `r`（或 `:reindex`）就地重建。

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
internal/tui/      bubbletea TUI (chat, settings, confirmation, setup wizard)
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
