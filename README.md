# kbcli

> **English (default)** | [简体中文](README.zh-CN.md)

This document is also available in Simplified Chinese: [README.zh-CN.md](README.zh-CN.md).

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![Go 1.27.4](https://img.shields.io/badge/Go-1.27.4-00ADD8)](https://go.dev/)

Chat with any local document directory from a single ~26–30 MB Go binary — no server, no vector database, no cloud account, no daemon. Drop the binary on your `PATH`, run `kbcli`, and the TUI is already there: no config file to write by hand, no port to open, no web UI to host.

`kbcli` turns one directory into one portable `.kbvec` vector file and opens a four-view terminal UI: a two-pane chat, a settings screen, an index confirmation dialog, and a first-run API key wizard. It is offline-first out of the box: kbcli starts in `lite` mode with a zero-dependency in-process hashing embedder, and when the chat backend is unreachable the answer falls back to a deterministic extractive answer with `[n]` citations. Indexing is always an explicit command; startup never scans or builds anything.

## Lightweight by design

- **One static binary, 26–30 MB.** Built with `CGO_ENABLED=0` and `-s -w`. The v0.3.0 normal tier is 26.3 MB (linux/arm64) to 30.3 MB (darwin/amd64). No runtime, no Python, no cgo, no container, no installer.
- **The TUI is the whole product.** `kbcli` opens a two-pane chat with ranked evidence, `:settings` for all 52 settings, `:reindex`, multi-window tabs and a first-run setup wizard. Nothing else needs to be running. No browser, no localhost port, no separate front end.
- **Two commands to a working answer.** `kbcli index ./docs` then `kbcli`. Indexing is always explicit; startup never scans, never walks parent directories and never builds anything behind your back.
- **One `.kbvec` file per directory.** Copy the folder and the index travels with it. No database to provision, migrate, back up or keep alive.
- **No downloads to get started.** kbcli starts in `lite` mode: a zero-dependency in-process hashing embedder with no weights and no reranker. Switching to `pro` downloads the multilingual embedding model once (~450 MB) into the local rembed cache. The `full` offline archive bundles both the `pro` weights and the legacy cross-encoder for air-gapped machines.
- **No account required to try it.** The first-run API key wizard is skippable with `ctrl+s`. If the chat backend is unreachable, answers degrade to deterministic extractive evidence with `[n]` citations instead of failing.
- **No YAML editing.** Every setting lives in the in-TUI `:settings` screen, with sparse per-workspace overrides. Works over SSH, in a tmux pane, and on an offline laptop.

## ProjAnvil projects used by kbcli

`kbcli` uses two ProjAnvil projects:

| Project | Role in `kbcli` |
| --- | --- |
| [langchain-golang](https://github.com/ProjAnvil/langchain-golang) | Direct Go dependency: `github.com/projanvil/langchain-golang v0.14.3`. `internal/agent` uses its model adapters, message types, tools, `deepagents`, and `langgraph/prebuilt` to build and run the chat agent. `internal/ingest/markdown.go` uses `core/documents` and `textsplitters` to split Markdown. |
| [Porto](https://github.com/ProjAnvil/Porto) | Porto is a Python project, not a Go dependency. `kbcli` ports its RAG algorithms into Go. `internal/retrieval` covers BM25, weighted RRF, query transforms, reranking and citations. `internal/intent` holds the intent router. `internal/session` holds per-workspace facts and summary memory. |

No other ProjAnvil repository is imported at build time or at run time. See `go.mod` for external dependencies.

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
- Porto-derived retrieval: `vector` / `bm25` / weighted-RRF `hybrid` (k=60). **`hybrid` is now the default**; it fuses dense vectors with BM25 before reranking so exact identifiers, version strings and Chinese terms survive the first stage. `vector` and `bm25` select a single channel. **Local `aikit` cross-encoder reranking is on by default** (no service, no Python, no cgo). A broad `candidate_k` pool (default 40) feeds reranking, `min_score` is applied to normalized post-rerank scores, the original question is recalled alongside a condensed rewrite, and one shared citation ledger numbers both the grounded prompt and `kb_search` results. Empty or failed retrieval returns an explicit refusal instead of a fabricated answer. External HTTP `cross_encoder` and `llm` reranking remain configurable, as do `hyde` / `multi_query` / `decomposition` / `step_back` query transforms, an anaphora condense gate, and kbcli's own `context_expand`.
- Porto-derived intent router: `direct` skips retrieval, `quick_rag` searches the original question, `deep_rag` runs the full pipeline. The intent provider inherits global settings by default. Short questions such as `why?` no longer bypass the knowledge base.
- Structure-aware chunking: a Strategy + Registry selects a splitter per file extension (Markdown headings, CSV row groups, HTML headings, PDF pages, DOCX sections) so one chunk stays inside one section; `chunk_strategy: fixed` restores the legacy single-size splitter.
- Four full-screen `bubbletea` + `bubbles` + `lipgloss` views with a theme that follows the terminal background (OSC 11, or `COLORFGBG`): a two-pane chat (conversation left, ranked evidence right; `ctrl+]` collapses the evidence pane), a master-detail settings screen (group rail + detail pane), an index confirmation dialog, and the first-run API key wizard.
- Bilingual English / Simplified Chinese UI and prompts, selected by `language` (`en`|`zh`); English is byte-for-byte the original UI.
- Per-workspace session memory at `~/.kbcli/sessions/<sha256(root)[:16]>.json` (mode `0600`): 4 fact categories (`user_decision`, `user_preference`, `project_context`, `open_question`), Jaccard dedup, cap eviction, history compaction, injected every round.
- A settings catalog of exactly 52 fields in 8 groups (Profile & Models, LLM, Foundation, Ingestion, Retrieval, Query Transform, Intent, Session), of which exactly 23 are numeric (`FieldInt` or `FieldFloat`), with sparse per-workspace overrides and an INDEX STALE banner driven by a version-2 build signature.

## Install

Requires Go 1.27.4.

```sh
make build      # bin/kbcli
make install    # go install ./cmd/kbcli
```

No CGO, no Python, no server: the result is a single binary.

### Prebuilt binaries

Each GitHub release publishes two distribution tiers per platform. Asset names carry the OS and architecture; full-tier names also contain `full`. `full` here is an **archive tier**, not the runtime `pro` profile — it means "every model bundled for offline use".

| Tier | Asset name | Contents |
| --- | --- | --- |
| Normal | `kbcli-<version>-<os>-<arch>` (`kbcli-<version>-windows-amd64.exe`) | Binary only. `lite` needs no model; `pro` downloads its weights on first use; `custom`+`aikit` downloads the cross-encoder on first use. |
| Full | `kbcli-full-<version>-<os>-<arch>.tar.gz` (`kbcli-full-<version>-windows-amd64.zip`) | Binary plus both offline model trees: the `pro` multilingual embedder and the legacy `custom`+`aikit` cross-encoder. Fully offline for every tier. |

Extract an archive and run the binary from the extracted directory (replace the example name with your version and platform):

```sh
tar -xzf kbcli-full-v0.4.1-darwin-arm64.tar.gz   # Windows: unzip kbcli-full-v0.4.1-windows-amd64.zip
./kbcli
```

The full archive contains:

```
kbcli                                  # kbcli.exe on Windows
model/
  rembed/intfloat--multilingual-e5-small/   # pro tier, rembed cache layout
    config.json
    model.safetensors
    modules.json
    sentencepiece.bpe.model
    tokenizer_config.json
    1_Pooling/config.json
  ms-marco-MiniLM-L-6-v2/                   # legacy custom+aikit tier
    config.json
    model.safetensors
    tokenizer.json
    tokenizer_config.json
    vocab.txt
```

The two trees are found automatically, with no configuration and no network:

- The `pro` weights sit at `model/rembed/<org>--<name>/`, the exact layout rembed uses below its cache root. kbcli detects that directory next to the executable and points `REMBED_CACHE` at `model/rembed/`, so every weight file is already present and `pro` indexing and querying work air-gapped. Set `REMBED_CACHE` explicitly to override.
- The cross-encoder files are copied from `model/ms-marco-MiniLM-L-6-v2/` into `~/.kbcli/model/` by the normal boot screen when `custom`+`aikit` is enabled.

The `pro` weights are ~450 MB, so a full archive is substantially larger than a normal binary. Both model sets are pinned to Hugging Face revisions and verified against `assets/model.sha256` during the release build. Each release also uploads `checksums.txt`; verify a download with:

```sh
sha256sum -c checksums.txt --ignore-missing
# macOS: shasum -a 256 -c checksums.txt --ignore-missing
```

### Remote install

Install a released version directly with `go install`:

```sh
go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.4.1
# or
go install github.com/ProjAnvil/kbcli/cmd/kbcli@latest
```

Use `go install`, not `go get`: since Go 1.17, `go get` only edits `go.mod` and does not install a binary. The binary lands in `$(go env GOPATH)/bin/kbcli`; set `GOBIN` to override that location. That directory must be on your `PATH`.

The module path uses the organisation casing `ProjAnvil`. Go module paths are case-sensitive, so use `github.com/ProjAnvil/kbcli` exactly as written above.

## Quickstart

```sh
kbcli config set profile pro      # optional: lite is the default
kbcli index ./docs
cd ./docs && kbcli
kbcli search "retry policy" --index ./docs/.kbvec
kbcli search "retry policy" --json --index ./docs/.kbvec
kbcli ask "what is the retry policy?" --json --index ./docs/.kbvec
kbcli eval --samples 50 --index ./docs/.kbvec
kbcli config set api_key sk-...
```

`kbcli eval` is a dependency-free self-retrieval smoke test. It reports
`recall@1`, `recall@k`, MRR and p50/p95 query latency with no API key, no
labelled data and no model download. Use it to compare retrieval settings on
your own corpus. Recall is optimistic because queries are built from chunk
bodies, so treat the numbers as a regression signal, not an answer-quality
score. `kbcli search --json` prints the same ranked hits as a stable JSON
array for tool consumers.

`kbcli ask` is the headless answer command. Use `--json` for a stable envelope containing the answer, ranked hits, active profile, intent route, detected query language, detected KB language, translation diagnostics and degradation reason. stdout carries only the answer or JSON; diagnostics go to stderr with `--explain`. Exit codes are `0` success, `2` config error, `3` no index, `4` LLM failure and `5` retrieval failure.

### Retrieval profiles

`profile` picks a product tier: a supported combination of embedder, query translation and reranker. The settings screen groups it with the embedding and rerank knobs under `Profile & Models`, and shows the effective stack plus every forced change.

| Profile | What you get | Embedder | Translation | Rerank | Downloads |
| --- | --- | --- | --- | --- | --- |
| `lite` **(default)** | Zero-setup, offline-first, fastest | zero-weight hashing | auto | off | nothing |
| `pro` | Best answer quality, cross-language | `rembed` / `intfloat/multilingual-e5-small` | auto | `local_maxsim` | ~450 MB once |
| `custom` | Manual control | manual | manual | manual | depends on your choices |

**Why these names.** `lite` and `pro` describe capability tiers, not implementation details. `pro` is not called `full` because `full` is already the name of the *offline release archive*, because "full" describes nothing about what is included, and because `custom` actually exposes more knobs than `pro`. The old implementation-flavoured name `multilingual` undersold the tier: neural embeddings also improve same-language conceptual recall, so the value is answer quality, of which cross-language is one dimension.

Tiers are enforced. Switching to `lite` disables reranking and forces the hashing embedder, so kbcli starts with no model download at all. Switching to `pro` forces `rembed`, hybrid retrieval, a wider candidate pool and token-level MaxSim reranking from the *same* model (no second reranker is downloaded). Each forced change is reported with a reason in the TUI.

**Upgrading.** `lite` is the default for fresh installs. An existing `config.yaml` that has no `profile` key is pinned to `custom` on load, preserving its previous behaviour instead of silently turning off a configured reranker. The v0.4.0 names `local` and `multilingual` remain accepted aliases for `lite` and `pro` — in config files, workspace overrides and `kbcli config set` — and are rewritten to the canonical names on the next save.

A warm benchmark on a 4.5 MB mixed Chinese/English interview corpus, using the same definition-style query family, showed:

| Query form | `lite` | `pro` |
| --- | --- | --- |
| Pure Chinese (`什么是系统设计`) | ~250 ms, fast literal recall | ~1.6 s, stronger conceptual recall |
| Pure English against Chinese KB (`what is system design`) | ~1.3 s, unstable target | ~4.8 s, stable target |
| Mixed (`what's 系统设计`) | ~1.6 s, literal-heavy | ~3.0 s, stable target |

Guidance: stay on `lite` for pure-Chinese corpora where exact identifiers dominate and startup latency matters — it is the default for a reason. Choose `pro` for mixed-language users, English queries against Chinese/non-English content, or when answer stability matters more than warm-query latency. Cold start is higher with `pro` because the neural weights load once; later queries reuse the cached model. The two tiers use incompatible embedding domains, so switching between `lite` and `pro` requires `kbcli reindex` once; query-time settings such as `top_k` and `candidate_k` never invalidate an index.

Inside the TUI, type `:index <dir>` to index a different directory (two-step confirmation).

Startup never indexes. If `.kbvec` exists in the current directory, chat starts immediately; otherwise the chat pane shows a hint to type `:index <dir>` or run `kbcli index <dir>`. No scan and no parent-directory walk happen at startup.

CLI subcommands: `index`, `reindex`, `update`, `config`, `settings`, `search`, `ask`, `eval`, `version`. In-TUI commands: `:reindex`, `:update`, `:settings`, `:stale`, `:clear`, `:help`, `:index`, `:q` (also `q` on an empty input line). `q` and `ctrl+c` quit; `esc` cancels an in-flight answer instead of quitting.

Answers stream into the chat pane as the model produces them. The thinking indicator spins from the moment `enter` is pressed until the first answer token arrives — covering intent routing, retrieval, reranking and generation. Press `esc` at any point during that window to cancel the whole request; a cancelled run writes no assistant turn and no fallback answer into the session. Tool-call preambles are discarded from the live transcript, so only the final answer round reaches the chat.

## Configuration

Global settings live in `~/.kbcli/config.yaml` (mode `0600`). Per-workspace overrides are sparse and live in `~/.kbcli/settings.json`: a workspace inherits `config.yaml` and overrides only the keys you set.

```sh
kbcli config keys              # all 52 keys with group, type and range
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

Reranking is **off in `lite`** (the default) because the tier downloads no model, and **on in `pro`** with `rerank_type: local_maxsim`, which reuses the multilingual embedder's token vectors instead of a second model. The `aikit` cross-encoder remains available under `custom`: set `rerank_enabled: on`, `rerank_type: aikit` and `rerank_model: ms-marco-MiniLM-L-6-v2`. With `rerank_model_path` empty, that model lives in `~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`; on first use kbcli opens a boot screen with an ASCII logo and a byte-level progress bar while it downloads the five files (~92 MB). Chat stays blocked until the download or copy finishes. Set `rerank_model_path` to an existing local model directory to skip the boot screen. See [Local cross-encoder reranking](#local-cross-encoder-reranking) for details and for the external HTTP and LLM options.

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

Reranking is **on in `pro`** via `rerank_type: local_maxsim`, which
reuses token vectors from the active `rembed` model. The `aikit`
cross-encoder is the legacy `custom` option: `rerank_type: aikit` runs a
cross-encoder in-process through
[`github.com/townsendmerino/aikit`](https://github.com/townsendmerino/aikit)
v1.78.0. It needs no HTTP service, Python, ONNX Runtime, or cgo; the API is
Experimental, so kbcli pins the dependency version.

**Managed model (aikit only).** With `rerank_model_path` empty, kbcli uses
`~/.kbcli/model/<rerank_model>` — by default
`~/.kbcli/model/ms-marco-MiniLM-L-6-v2/`. On the first run with `aikit`
enabled, kbcli checks for five files there; if any is missing it opens a boot
screen with an ASCII wordmark and a byte-level progress bar while it downloads
them (~92 MB, Hugging Face revision `233902d25c440f23af6f7d6e94d2946bac0bee0a`).
Chat stays blocked until the download or copy finishes. A [full offline release](#prebuilt-binaries) ships the same five
files, so its first run only copies them into the managed directory — no network.
The same archive also ships the `pro` multilingual weights under
`model/rembed/`, which kbcli consumes directly without a copy step.

**External local model.** Set `rerank_model_path` to a directory containing
`config.json`, `model.safetensors`, and tokenizer files; kbcli treats it as
externally managed and skips the download:

```sh
kbcli config set profile custom     # managed tiers force incompatible switches
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

`:settings` opens a master-detail view: a group rail on the left, a detail pane on the right. The catalog has exactly **52 fields** in **8 groups** — Profile & Models, LLM, Foundation, Ingestion, Retrieval, Query Transform, Intent, Session — and exactly **23** of them are numeric (`FieldInt` or `FieldFloat`). Profile & Models is first so switching tiers shows the effective embedder/rerank stack and every forced change; LLM holds the chat credentials separately. Only fields you mark are written as sparse overrides; everything else keeps its global value. The **INDEX STALE** banner appears at the top when the loaded index was built with different index-affecting settings; press `r` (or `:reindex`) to rebuild in place.

### Embedding models and reindexing

The `lite` tier uses a pure-Go hashing embedder: it downloads nothing and works fully offline. The `pro` tier uses `rembed` with `intfloat/multilingual-e5-small` (384 dimensions, mean pooling, e5 `query: ` / `passage: ` prefixes, weight-only int8). Weights are downloaded once on first use into `$REMBED_CACHE` — by default `<user cache dir>/rembed` — and reused afterwards; a [full offline archive](#prebuilt-binaries) ships the same weights under `model/rembed/` and is detected automatically. `HF_TOKEN` is honoured for gated or rate-limited Hubs. `rembed.Load` exposes no byte-level progress, so kbcli shows a phase-level spinner (`loading model` → `scanning` → `indexing` → `saving`) instead of a percentage bar. While a reindex or model load runs, a full-screen modal blocks chat input, and a second reindex cannot be started until the current one finishes.

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
