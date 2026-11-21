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
- Porto-derived retrieval: `vector` / `bm25` / weighted-RRF `hybrid` (k=60), `cross_encoder` or `llm` reranking, `hyde` / `multi_query` / `decomposition` / `step_back` query transforms, an anaphora condense gate, plus kbcli's own `min_score` and `context_expand`.
- Three full-screen `bubbletea` + `bubbles` + `lipgloss` views with a theme that follows the terminal background (OSC 11, or `COLORFGBG`): a two-pane chat (conversation left, ranked evidence right), a master-detail settings screen (group rail + detail pane), and an index confirmation dialog.
- Bilingual English / Simplified Chinese UI and prompts, selected by `language` (`en`|`zh`); English is byte-for-byte the original UI.
- Per-workspace session memory at `~/.kbcli/sessions/<sha256(root)[:16]>.json` (mode `0600`): 4 fact categories (`user_decision`, `user_preference`, `project_context`, `open_question`), Jaccard dedup, cap eviction, history compaction, injected every round.
- A settings catalog of exactly 40 fields in 6 groups (Foundation, Ingestion, Retrieval, Rerank, Query Transform, Session), of which exactly 21 are numeric (`FieldInt` or `FieldFloat`), with sparse per-workspace overrides and an INDEX STALE banner driven by a version-2 build signature.

## Install

Requires Go 1.27.4.

```sh
make build      # bin/kbcli
make install    # go install ./cmd/kbcli
```

No CGO, no Python, no server: the result is a single binary.

### Remote install

Install a released version directly with `go install`:

```sh
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.1.1
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
kbcli config keys              # all 40 keys with group, type and range
kbcli config list              # effective values, secrets masked
kbcli config get top_k
kbcli config set top_k 9
kbcli config path              # ~/.kbcli/config.yaml
kbcli settings show            # effective config + index status
kbcli settings set top_k 9     # one sparse workspace override
```

`chat_provider` selects the chat API protocol: `openai` (default, OpenAI-compatible Chat Completions) or `anthropic` (Anthropic Messages API). `base_url` must point to an endpoint compatible with the chosen protocol. When using the official Anthropic API, leave `base_url` unset or set it to `https://api.anthropic.com/v1` — the agent automatically falls back to the protocol-appropriate default URL (the default OpenAI `base_url` is only correct for `openai`).

The [settings screen](#settings-screen) (`:settings`) edits the same catalog in the TUI. Changing an index-affecting key — `chunk_size`, `chunk_overlap`, `embedder`, `embed_model` — is recorded in a version-2 build signature; on load, drift raises an **INDEX STALE** banner. Run `:reindex` (or `kbcli reindex`) to rebuild.

### Chunk size and overlap

kbcli's defaults now match LangChain's `RecursiveCharacterTextSplitter` character-splitting defaults: `chunk_size: 4000` and `chunk_overlap: 200`. kbcli counts **Go runes**, the Unicode code-point analogue of Python's `len()` in LangChain, rather than tokens.

Token-based splitters exist and use model-specific tokenizers; kbcli's rune setting is the user-facing, model-independent analogue. If you select `embedder: rembed`, kbcli additionally caps chunks at that model's token limit at split time — the rune configuration remains the user-facing setting.

Both `chunk_size` and `chunk_overlap` are index-affecting: changing either invalidates the `.kbvec` index and requires a reindex (`:reindex` / `kbcli reindex`). Let recall@k guide tuning.

Background: [LangChain text splitters](https://python.langchain.com/docs/concepts/text_splitters/).

<a id="settings-screen"></a>

### Settings screen

`:settings` opens a master-detail view: a group rail on the left, a detail pane on the right. The catalog has exactly **40 fields** in **6 groups** — Foundation, Ingestion, Retrieval, Rerank, Query Transform, Session — and exactly **21** of them are numeric (`FieldInt` or `FieldFloat`). Only fields you mark are written as sparse overrides; everything else keeps its global value. The **INDEX STALE** banner appears at the top when the loaded index was built with different index-affecting settings; press `r` (or `:reindex`) to rebuild in place.

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
