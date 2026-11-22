# kbcli

> **English (default)** | [简体中文](README.zh-CN.md)

This document is also available in Simplified Chinese: [README.zh-CN.md](README.zh-CN.md).

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![Go 1.27.4](https://img.shields.io/badge/Go-1.27.4-00ADD8)](https://go.dev/)

Chat with any local document directory from a single ~26–30 MB Go binary — no server, no vector database, no cloud account, no daemon. Drop the binary on your `PATH`, run `kbcli`, and the TUI is already there: no config file to write by hand, no port to open, no web UI to host.

`kbcli` turns one directory into one portable `.kbvec` vector file and opens a four-view terminal UI: a two-pane chat, a settings screen, an index confirmation dialog, and a first-run API key wizard. It is offline-first out of the box: the default embedder is a zero-dependency in-process hashing model, and when the chat backend is unreachable the answer falls back to a deterministic extractive answer with `[n]` citations. Indexing is always an explicit command; startup never scans or builds anything.

## Lightweight by design

- **One static binary, 26–30 MB.** Built with `CGO_ENABLED=0` and `-s -w`. The v0.3.0 normal tier is 26.3 MB (linux/arm64) to 30.3 MB (darwin/amd64). No runtime, no Python, no cgo, no container, no installer.
- **The TUI is the whole product.** `kbcli` opens a two-pane chat with ranked evidence, `:settings` for all 49 settings, `:reindex`, multi-window tabs and a first-run setup wizard. Nothing else needs to be running. No browser, no localhost port, no separate front end.
- **Two commands to a working answer.** `kbcli index ./docs` then `kbcli`. Indexing is always explicit; startup never scans, never walks parent directories and never builds anything behind your back.
- **One `.kbvec` file per directory.** Copy the folder and the index travels with it. No database to provision, migrate, back up or keep alive.
- **No downloads to get started.** The default embedder is a zero-dependency in-process hashing model with no weights. The local cross-encoder reranker is fetched once into `~/.kbcli/model` (shown in the TUI boot screen) or shipped inside the `full` archive (~92 MB) for air-gapped machines.
- **No account required to try it.** The first-run API key wizard is skippable with `ctrl+s`. If the chat backend is unreachable, answers degrade to deterministic extractive evidence with `[n]` citations instead of failing.
- **No YAML editing.** Every setting lives in the in-TUI `:settings` screen, with sparse per-workspace overrides. Works over SSH, in a tmux pane, and on an offline laptop.

## Also from ProjAnvil

`kbcli` is part of [ProjAnvil — Infrastructure for the Agent Era](https://projanvil.github.io): 14 public, MIT-licensed, Go-first repositories. Each layer does one job and works on its own; `kbcli` is the knowledge layer for your terminal.

| Project | What it does |
| --- | --- |
| [langchain-golang](https://github.com/ProjAnvil/langchain-golang) | A community Go port of LangChain / LangGraph — the agent runtime `kbcli` is built on. |
| [Porto](https://github.com/ProjAnvil/Porto) | Turns a PRD into grounded engineering specs; `kbcli`'s retrieval pipeline is derived from it. |
| [Vectra](https://github.com/ProjAnvil/Vectra) | Embedded in-memory graph engine for Go, with optional server mode and MCP interfaces. |
| [Corpit](https://github.com/ProjAnvil/Corpit) | One Go binary that creates and runs a virtual company of AI agents. |
| [Knot](https://github.com/ProjAnvil/Knot) | API documentation hub with a built-in MCP server, readable by humans and agents. |
| [WoCA](https://github.com/ProjAnvil/WoCA) | Single-binary coding agent harness with an explicit, inspectable agent graph. |
| [MindForge](https://github.com/ProjAnvil/MindForge) · [SCV](https://github.com/ProjAnvil/SCV) · [LadyM](https://github.com/ProjAnvil/LadyM) · [Belt](https://github.com/ProjAnvil/Belt) · [Jiade](https://github.com/ProjAnvil/Jiade) · [Smelt](https://github.com/ProjAnvil/Smelt) · [Bossy](https://github.com/ProjAnvil/Bossy) · [claude-agent-sdk-golang](https://github.com/ProjAnvil/claude-agent-sdk-golang) | Skills, codebase knowledge, agent memory, scaffolding, verification, ChatBI and the Go SDK. |

[Browse all repositories →](https://github.com/orgs/ProjAnvil/repositories) · [How the stack fits together →](https://projanvil.github.io/#architecture)

Schematic of the chat view (not a screenshot):

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

## Why kbcli

- **One portable file, zero infrastructure.** One directory produces one `.kbvec` binary vector file — no server, no vector database, no external service. Move or copy the folder and the index travels with it.
- **Offline-first by default, not an opt-in.** The default embedder is a zero-dependency in-process hashing model, and an unreachable chat backend falls back to a deterministic extractive answer with `[n]` citations. Every LLM-dependent retrieval stage fails open to plain retrieval.
- **Never silently indexes your disk.** Startup never scans, never walks parent directories and never builds an index; `:index` runs a bounded read-only preflight (20000 files / 12 directory levels) and requires a two-step directory confirmation before any write.

## Features

- One portable `.kbvec` per directory: magic header `KBCLI\x00`, gob payload, atomic rename via `os.Rename`. Legacy `.kbcli.kbvec` still loads, and embedder-domain isolation refuses an index built by a different embedder.
- Two embedders, both 384-dimensional and L2-normalized: a zero-dependency in-process hashing model (FNV-1a word unigrams + character 3-grams) and an optional pure-Go `rembed` neural model.
- Chat backends for both OpenAI-compatible Chat Completions and the Anthropic Messages API (`chat_provider`, `base_url`, `model`, `api_key`). A first-run TUI wizard collects the provider, a masked API key, the endpoint and the model, runs a short connectivity check, and rebuilds the live agent in place; `ctrl+s` skips it for offline use. When the backend is unreachable, answers degrade to a deterministic extractive answer from the top evidence with `[n]` citations.
- Porto-derived retrieval: `vector` / `bm25` / weighted-RRF `hybrid` (k=60), with **local `aikit` cross-encoder reranking on by default** (no service, no Python, no cgo). A broad `candidate_k` pool (default 40) feeds reranking, `min_score` is applied to normalized post-rerank scores, the original question is recalled alongside a condensed rewrite, and one shared citation ledger numbers both the grounded prompt and `kb_search` results. Empty or failed retrieval returns an explicit refusal instead of a fabricated answer. External HTTP `cross_encoder` and `llm` reranking remain configurable, as do `hyde` / `multi_query` / `decomposition` / `step_back` query transforms, an anaphora condense gate, and kbcli's own `context_expand`.
- Porto-derived intent router: `direct` skips retrieval, `quick_rag` searches the original question, `deep_rag` runs the full pipeline. The intent provider inherits global settings by default. Short questions such as `why?` no longer bypass the knowledge base.
- Structure-aware chunking: a Strategy + Registry selects a splitter per file extension (Markdown headings, CSV row groups, HTML headings, PDF pages, DOCX sections) so one chunk stays inside one section; `chunk_strategy: fixed` restores the legacy single-size splitter.
- Four full-screen `bubbletea` + `bubbles` + `lipgloss` views with a theme that follows the terminal background (OSC 11, or `COLORFGBG`): a two-pane chat (conversation left, ranked evidence right; `ctrl+]` collapses the evidence pane), a master-detail settings screen (group rail + detail pane), an index confirmation dialog, and the first-run API key wizard.
- Bilingual English / Simplified Chinese UI and prompts, selected by `language` (`en`|`zh`); English is byte-for-byte the original UI.
- Per-workspace session memory at `~/.kbcli/sessions/<sha256(root)[:16]>.json` (mode `0600`): 4 fact categories (`user_decision`, `user_preference`, `project_context`, `open_question`), Jaccard dedup, cap eviction, history compaction, injected every round.
- A settings catalog of exactly 49 fields in 7 groups (Foundation, Ingestion, Retrieval, Rerank, Query Transform, Intent, Session), of which exactly 23 are numeric (`FieldInt` or `FieldFloat`), with sparse per-workspace overrides and an INDEX STALE banner driven by a version-2 build signature.

## Install

Requires Go 1.27.4.

```sh
make build      # bin/kbcli
make install    # go install ./cmd/kbcli
```

No CGO, no Python, no server: the result is a single binary.

### Prebuilt binaries

Each GitHub release publishes two tiers of archives per platform. Asset names carry the OS and architecture; full-tier names also contain `full`.

| Tier | Asset name | Contents |
| --- | --- | --- |
| Normal | `kbcli-<version>-<os>-<arch>` (`kbcli-<version>-windows-amd64.exe`) | Binary only; the rerank model downloads on first run. |
| Full | `kbcli-full-<version>-<os>-<arch>.tar.gz` (`kbcli-full-<version>-windows-amd64.zip`) | Binary plus `model/ms-marco-MiniLM-L-6-v2/` with all five model files; fully offline. |

Extract an archive and run the binary from the extracted directory (replace the example name with your version and platform):

```sh
tar -xzf kbcli-full-v0.3.0-darwin-arm64.tar.gz   # Windows: unzip kbcli-full-v0.3.0-windows-amd64.zip
./kbcli
```

The full archive contains:

```
kbcli                                  # kbcli.exe on Windows
model/ms-marco-MiniLM-L-6-v2/
  config.json
  model.safetensors
  tokenizer.json
  tokenizer_config.json
  vocab.txt
```

On the first run of a full release, kbcli copies `model/` from the executable's directory into `~/.kbcli/model/` — no network access. Each release also uploads `checksums.txt`; verify a download with:

```sh
sha256sum -c checksums.txt --ignore-missing
# macOS: shasum -a 256 -c checksums.txt --ignore-missing
```

### Remote install

Install a released version directly with `go install`:

```sh
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.3.0
# or
go install github.com/ProjAnvil/kbcli/cmd/kbcli@latest
```

Use `go install`, not `go get`: since Go 1.17, `go get` only edits `go.mod` and does not install a binary. The binary lands in `$(go env GOPATH)/bin/kbcli`; set `GOBIN` to override that location. That directory must be on your `PATH`.

The module path uses the organisation casing `ProjAnvil`. Go module paths are case-sensitive, so use `github.com/ProjAnvil/kbcli` exactly as written above.

## Quickstart

```sh
kbcli index ./docs
cd ./docs && kbcli
kbcli search "retry policy" --index ./docs/.kbvec
kbcli config set api_key sk-...
```

Inside the TUI, type `:index <dir>` to index a different directory (two-step confirmation).

Startup never indexes. If `.kbvec` exists in the current directory, chat starts immediately; otherwise the chat pane shows a hint to type `:index <dir>` or run `kbcli index <dir>`. No scan and no parent-directory walk happen at startup.

CLI subcommands: `index`, `reindex`, `update`, `config`, `settings`, `search`, `version`. In-TUI commands: `:reindex`, `:update`, `:settings`, `:stale`, `:clear`, `:help`, `:index`, `:q` (also `q` on an empty input line). `q` and `ctrl+c` quit; `esc` cancels an in-flight answer instead of quitting.

Answers stream into the chat pane as the model produces them. The thinking indicator spins from the moment `enter` is pressed until the first answer token arrives — covering intent routing, retrieval, reranking and generation. Press `esc` at any point during that window to cancel the whole request; a cancelled run writes no assistant turn and no fallback answer into the session. Tool-call preambles are discarded from the live transcript, so only the final answer round reaches the chat.

## Configuration

Global settings live in `~/.kbcli/config.yaml` (mode `0600`). Per-workspace overrides are sparse and live in `~/.kbcli/settings.json`: a workspace inherits `config.yaml` and overrides only the keys you set.

```sh
kbcli config keys              # all 49 keys with group, type and range
kbcli config list              # effective values, secrets masked
kbcli config get top_k
kbcli config set top_k 9
kbcli config path              # ~/.kbcli/config.yaml
kbcli settings show            # effective config + index status
kbcli settings set top_k 9     # one sparse workspace override
```

`chat_provider` selects the chat API protocol: `openai` (default, OpenAI-compatible Chat Completions) or `anthropic` (Anthropic Messages API). `base_url` must point to an endpoint compatible with the chosen protocol. When using the official Anthropic API, leave `base_url` unset or set it to `https://api.anthropic.com/v1` — the agent automatically falls back to the protocol-appropriate default URL (the default OpenAI `base_url` is only correct for `openai`).

### First-run API key setup

When no `api_key` is configured and setup has not been dismissed, kbcli opens a full-screen wizard before chat. Step through `chat_provider` (left/right toggles, `enter` confirms), a masked `api_key`, `base_url` and `model`; `enter` advances, `esc` goes back, and on the last step `enter` saves. kbcli validates the config, runs a 5-second HTTP probe against `base_url` (any HTTP response counts as reachable), writes `~/.kbcli/config.yaml` with mode `0600`, and rebuilds the agent, retriever and session so the new key takes effect without a restart. A failed probe only adds a warning — the config is still saved. `ctrl+s` (or `s` on the first step) skips the wizard and records `setup_done: true`, keeping kbcli fully offline. You can also avoid the wizard entirely with `kbcli config set api_key sk-...`.

Reranking is **on by default**: `rerank_enabled: true`, `rerank_type: aikit`, and `rerank_model: ms-marco-MiniLM-L-6-v2`. With `rerank_model_path` empty, the model lives in the managed directory `~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`. On the first run of a normal release, kbcli opens a boot screen with an ASCII logo and a byte-level progress bar while it downloads the five model files (~92 MB). Chat stays blocked until the download or copy finishes. Set `rerank_model_path` to an existing local model directory to skip the boot screen. See [Local cross-encoder reranking](#local-cross-encoder-reranking) for details and for the external HTTP and LLM options.

`intent_mode` selects the router: `off`, `binary` (`direct` or `rag`), or `adaptive` (`direct`, `quick_rag`, `deep_rag`). The default is `adaptive`. A `direct` message skips retrieval and gets one short answer. A `quick_rag` message searches the original question without condense or query transforms. A `deep_rag` message runs the full pipeline.

Configure the classifier with `intent_provider`, `intent_api_key`, `intent_base_url` and `intent_model`. The default `intent_provider: inherit` reuses the global chat provider, endpoint, model and key. Leave the three credential fields empty to inherit their global values. An unusable classifier falls back to keyword rules, then to the full RAG path.

The [settings screen](#settings-screen) (`:settings`) edits the same catalog in the TUI. Changing an index-affecting key — `chunk_size`, `chunk_overlap`, `section_size`, `chunk_strategy`, `embedder`, `embed_model` — is recorded in the build signature (now also versioned with the `per-ext-v1` chunker); on load, drift raises an **INDEX STALE** banner. Run `:reindex` (or `kbcli reindex`) to rebuild. Query-time knobs such as `candidate_k` do not invalidate the index.

### Chunking by file type

A 4000-rune hard split is fine for plain text but merges unrelated sections in structured files. `chunk_strategy: auto` (the default) selects a structure-aware splitter per extension through a Strategy + Registry; `chunk_strategy: fixed` forces the legacy single-size splitter on every format.

| Extensions | Strategy | Size cap |
|---|---|---|
| `.md`, `.markdown`, `.mdx` | Markdown headings (h1–h4), with the heading path injected into the embedding text | `section_size` |
| `.txt`, `.text` | Plain-text recursive split | `chunk_size` |
| `.csv` | Row groups | `section_size` |
| `.html`, `.htm` | HTML headings | `section_size` |
| `.pdf` | One page | `section_size` |
| `.docx` | Converted Markdown sections | `section_size` |

`chunk_size: 4000` and `chunk_overlap: 200` still match LangChain's `RecursiveCharacterTextSplitter` defaults, but `chunk_size` now caps **plain-text chunks only**. Structured formats are capped per section by `section_size` (default 1000 runes); an over-long section is recursively split instead of being merged into its neighbour. kbcli counts **Go runes**, the Unicode code-point analogue of Python's `len()`, rather than tokens. If you select `embedder: rembed`, chunks are additionally capped at that model's token limit at split time.

### Local cross-encoder reranking

Reranking is **on by default**: `rerank_type: aikit` runs a cross-encoder
in-process through
[`github.com/townsendmerino/aikit`](https://github.com/townsendmerino/aikit)
v1.78.0. It needs no HTTP service, Python, ONNX Runtime, or cgo; the API is
Experimental, so kbcli pins the dependency version.

**Managed model (default).** With `rerank_model_path` empty, kbcli uses
`~/.kbcli/model/<rerank_model>` — by default
`~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`. On the first run of a normal release,
kbcli checks for five files there; if any is missing it opens a boot screen
with an ASCII wordmark and a byte-level progress bar while it downloads them
(~92 MB, Hugging Face revision `233902d25c440f23af6f7d6e94d2946bac0bee0a`).
Chat stays blocked until the download or copy finishes. A [full offline release](#prebuilt-binaries) ships the same five
files, so its first run only copies them into the managed directory — no network.

**External local model.** Set `rerank_model_path` to a directory containing
`config.json`, `model.safetensors`, and tokenizer files; kbcli treats it as
externally managed and skips the download:

```sh
kbcli config set rerank_enabled on
kbcli config set rerank_type aikit
kbcli config set rerank_model_path /path/to/ms-marco-MiniLM-L-6-v2
```

For a custom setup you can fetch the reference model yourself:

```sh
huggingface-cli download cross-encoder/ms-marco-MiniLM-L-6-v2 \
  config.json model.safetensors tokenizer.json tokenizer_config.json vocab.txt \
  --local-dir /tmp/ms-marco-MiniLM-L-6-v2
```

**Other rerankers.** `rerank_type: cross_encoder` uses an external HTTP
cross-encoder via `rerank_base_url`, `rerank_model` and `rerank_api_key`;
`rerank_type: llm` uses an LLM reranker. Both remain configurable and require
you to run the corresponding service.

All rerank settings are query-time only and never invalidate the `.kbvec`
index. Any load or scoring error fails open and keeps the original retrieval
order.

`chunk_size`, `chunk_overlap`, `section_size` and `chunk_strategy` are index-affecting: changing any of them invalidates the `.kbvec` index and requires a reindex (`:reindex` / `kbcli reindex`). `candidate_k` is query-time only. Let recall@k guide tuning.

Background: [LangChain text splitters](https://python.langchain.com/docs/concepts/text_splitters/).

<a id="keyboard-shortcuts"></a>

### Keyboard shortcuts and windows

The chat screen uses `ctrl+x` as a window prefix, following the Emacs `C-x` convention. `ctrl+w`, `ctrl+a` and `ctrl+b` are avoided because the text input uses them for editing. Press the prefix, then one of:

| Keys | Action |
|---|---|
| `ctrl+x t` | new window |
| `ctrl+x n` / `ctrl+x p` | next / previous window |
| `ctrl+x 0` … `ctrl+x 8` | jump to window number |
| `ctrl+x x` | close the current window |
| `ctrl+x w` | open the window picker |

A tab bar sits above the help line and highlights the active window. `ctrl+x w` opens a toast-style picker. Press `up`/`down` to choose a window and `enter` to open it. `esc` cancels.

Each window keeps its own transcript, evidence and session memory. The prefix times out after 750 ms. Keys pressed after the prefix never reach the text input. `ctrl+c` still quits.

Switching tabs never stops a running request. Every tab reserves one fixed cell in the tab bar for a spinner; a busy tab shows the rotating frame there even when it is in the background, so the bar width never jumps when a request starts or finishes. Switch back to that tab and press `esc` to cancel it.

<a id="settings-screen"></a>

### Settings screen

`:settings` opens a master-detail view: a group rail on the left, a detail pane on the right. The catalog has exactly **49 fields** in **7 groups** — Foundation, Ingestion, Retrieval, Rerank, Query Transform, Intent, Session — and exactly **23** of them are numeric (`FieldInt` or `FieldFloat`). Only fields you mark are written as sparse overrides; everything else keeps its global value. The **INDEX STALE** banner appears at the top when the loaded index was built with different index-affecting settings; press `r` (or `:reindex`) to rebuild in place.

### Embedding models and reindexing

The default local embedder is a pure-Go hashing model: it downloads nothing and works fully offline. Selecting the `rembed` neural embedder downloads its weights on first use and reuses the local cache afterwards. `rembed.Load` exposes no byte-level progress, so kbcli shows a phase-level spinner (`loading model` → `scanning` → `indexing` → `saving`) instead of a percentage bar. While a reindex or model load runs, a full-screen modal blocks chat input, and a second reindex cannot be started until the current one finishes.

## How it works

`ingest` -> `chunk` -> `embed` -> `.kbvec` -> `retrieval` -> `agent` -> TUI. A directory is walked and split, each chunk is embedded into a 384-dimensional vector, and the whole index is written as one `.kbvec` file. At query time retrieval ranks evidence, the agent assembles an answer, and the TUI renders the chat and the evidence panes.

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

## Platform compatibility

All filesystem paths are built with Go standard-library helpers — `os.UserHomeDir`, `os.UserCacheDir`, `filepath.Join`, `filepath.Clean`, `filepath.VolumeName`, `filepath.Separator`, `runtime.GOOS` and `os.Getenv` — so no POSIX path is hardcoded. The tree cross-compiles for Windows, macOS and Linux with CGO disabled:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./...
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

Windows runtime has not been verified on real hardware; only cross-compilation and static checks.

## Documentation

- [简体中文 README](README.zh-CN.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CHANGELOG.md](CHANGELOG.md)
- [SECURITY.md](SECURITY.md)

## Contributing

```sh
make test
make race
make vet
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
