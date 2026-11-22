# kbcli

> **English (default)** | [简体中文](README.zh-CN.md)

This document is also available in Simplified Chinese: [README.zh-CN.md](README.zh-CN.md).

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![Go 1.27.4](https://img.shields.io/badge/Go-1.27.4-00ADD8)](https://go.dev/)

Chat with any local document directory from a single Go binary — no server, no vector database, no cloud account.

`kbcli` turns one directory into one portable `.kbvec` vector file and opens a three-view terminal UI: a two-pane chat, a settings screen, and an index confirmation dialog. It is offline-first out of the box: the default embedder is a zero-dependency in-process hashing model, and when the chat backend is unreachable the answer falls back to a deterministic extractive answer with `[n]` citations. Indexing is always an explicit command; startup never scans or builds anything.

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
- Chat backends for both OpenAI-compatible Chat Completions and the Anthropic Messages API (`chat_provider`, `base_url`, `model`, `api_key`); when the backend is unreachable, answers degrade to a deterministic extractive answer from the top evidence with `[n]` citations.
- Porto-derived retrieval: `vector` / `bm25` / weighted-RRF `hybrid` (k=60), with **local `aikit` cross-encoder reranking on by default** (no service, no Python, no cgo); external HTTP `cross_encoder` and `llm` reranking remain configurable, as do `hyde` / `multi_query` / `decomposition` / `step_back` query transforms, an anaphora condense gate, and kbcli's own `min_score` and `context_expand`.
- Porto-derived intent router: `direct` skips retrieval, `quick_rag` searches the original question, `deep_rag` runs the full pipeline. The intent provider inherits global settings by default.
- Three full-screen `bubbletea` + `bubbles` + `lipgloss` views with a theme that follows the terminal background (OSC 11, or `COLORFGBG`): a two-pane chat (conversation left, ranked evidence right; `ctrl+]` collapses the evidence pane), a master-detail settings screen (group rail + detail pane), and an index confirmation dialog.
- Bilingual English / Simplified Chinese UI and prompts, selected by `language` (`en`|`zh`); English is byte-for-byte the original UI.
- Per-workspace session memory at `~/.kbcli/sessions/<sha256(root)[:16]>.json` (mode `0600`): 4 fact categories (`user_decision`, `user_preference`, `project_context`, `open_question`), Jaccard dedup, cap eviction, history compaction, injected every round.
- A settings catalog of exactly 46 fields in 7 groups (Foundation, Ingestion, Retrieval, Rerank, Query Transform, Intent, Session), of which exactly 21 are numeric (`FieldInt` or `FieldFloat`), with sparse per-workspace overrides and an INDEX STALE banner driven by a version-2 build signature.

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
tar -xzf kbcli-full-v0.2.0-darwin-arm64.tar.gz   # Windows: unzip kbcli-full-v0.2.0-windows-amd64.zip
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
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.2.0
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

CLI subcommands: `index`, `reindex`, `update`, `config`, `settings`, `search`, `version`. In-TUI commands: `:reindex`, `:update`, `:settings`, `:stale`, `:clear`, `:help`, `:index`, `:q` (also `q` on an empty input line). `q`, `esc` and `ctrl+c` also quit.

## Configuration

Global settings live in `~/.kbcli/config.yaml` (mode `0600`). Per-workspace overrides are sparse and live in `~/.kbcli/settings.json`: a workspace inherits `config.yaml` and overrides only the keys you set.

```sh
kbcli config keys              # all 46 keys with group, type and range
kbcli config list              # effective values, secrets masked
kbcli config get top_k
kbcli config set top_k 9
kbcli config path              # ~/.kbcli/config.yaml
kbcli settings show            # effective config + index status
kbcli settings set top_k 9     # one sparse workspace override
```

`chat_provider` selects the chat API protocol: `openai` (default, OpenAI-compatible Chat Completions) or `anthropic` (Anthropic Messages API). `base_url` must point to an endpoint compatible with the chosen protocol. When using the official Anthropic API, leave `base_url` unset or set it to `https://api.anthropic.com/v1` — the agent automatically falls back to the protocol-appropriate default URL (the default OpenAI `base_url` is only correct for `openai`).

Reranking is **on by default**: `rerank_enabled: true`, `rerank_type: aikit`, and `rerank_model: ms-marco-MiniLM-L-6-v2`. With `rerank_model_path` empty, the model lives in the managed directory `~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`. On the first run of a normal release, kbcli opens a boot screen with an ASCII logo and a byte-level progress bar while it downloads the five model files (~92 MB). Chat stays blocked until the download or copy finishes. Set `rerank_model_path` to an existing local model directory to skip the boot screen. See [Local cross-encoder reranking](#local-cross-encoder-reranking) for details and for the external HTTP and LLM options.

`intent_mode` selects the router: `off`, `binary` (`direct` or `rag`), or `adaptive` (`direct`, `quick_rag`, `deep_rag`). The default is `adaptive`. A `direct` message skips retrieval and gets one short answer. A `quick_rag` message searches the original question without condense or query transforms. A `deep_rag` message runs the full pipeline.

Configure the classifier with `intent_provider`, `intent_api_key`, `intent_base_url` and `intent_model`. The default `intent_provider: inherit` reuses the global chat provider, endpoint, model and key. Leave the three credential fields empty to inherit their global values. An unusable classifier falls back to keyword rules, then to the full RAG path.

The [settings screen](#settings-screen) (`:settings`) edits the same catalog in the TUI. Changing an index-affecting key — `chunk_size`, `chunk_overlap`, `embedder`, `embed_model` — is recorded in a version-2 build signature; on load, drift raises an **INDEX STALE** banner. Run `:reindex` (or `kbcli reindex`) to rebuild.

### Chunk size and overlap

kbcli's defaults now match LangChain's `RecursiveCharacterTextSplitter` character-splitting defaults: `chunk_size: 4000` and `chunk_overlap: 200`. kbcli counts **Go runes**, the Unicode code-point analogue of Python's `len()` in LangChain, rather than tokens.

Token-based splitters exist and use model-specific tokenizers; kbcli's rune setting is the user-facing, model-independent analogue. If you select `embedder: rembed`, kbcli additionally caps chunks at that model's token limit at split time — the rune configuration remains the user-facing setting.

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

Both `chunk_size` and `chunk_overlap` are index-affecting: changing either invalidates the `.kbvec` index and requires a reindex (`:reindex` / `kbcli reindex`). Let recall@k guide tuning.

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

<a id="settings-screen"></a>

### Settings screen

`:settings` opens a master-detail view: a group rail on the left, a detail pane on the right. The catalog has exactly **46 fields** in **7 groups** — Foundation, Ingestion, Retrieval, Rerank, Query Transform, Intent, Session — and exactly **21** of them are numeric (`FieldInt` or `FieldFloat`). Only fields you mark are written as sparse overrides; everything else keeps its global value. The **INDEX STALE** banner appears at the top when the loaded index was built with different index-affecting settings; press `r` (or `:reindex`) to rebuild in place.

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
internal/tui/      three-view bubbletea TUI (chat, settings, confirmation)
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
