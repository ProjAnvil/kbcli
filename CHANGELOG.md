# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Porto-derived intent router with `off`, `binary` and `adaptive` modes. The
  `direct`, `quick_rag` and `deep_rag` routes select retrieval depth before
  the agent runs. An unusable classifier falls back to keyword rules.
- Five intent settings: `intent_mode`, `intent_provider`, `intent_api_key`,
  `intent_base_url` and `intent_model`. All provider fields inherit global
  settings by default. Classifier decisions appear as chat status lines.
- Added local `aikit` cross-encoder reranking with `rerank_type: aikit` and
  `rerank_model_path`. The in-process backend needs no HTTP service or Python.
- `ctrl+]` toggles the evidence pane and gives the full terminal width to chat.
- Full offline release archives whose names contain `full`: the binary plus
  `model/ms-marco-MiniLM-L-6-v2/` with all five model files. The first run
  copies them into `~/.kbcli/model/` with no network access. Each release also
  uploads `checksums.txt`.

### Changed

- Reranking is now on by default with the local `aikit` cross-encoder
  (`rerank_enabled: true`, `rerank_type: aikit`, `rerank_model: ms-marco-MiniLM-L-6-v2`).
  The first run downloads the five model files (~92 MB) with a progress line
  before the TUI starts; setting `rerank_model_path` uses an external local
  model and skips the download.

### Fixed

- Chat messages now wrap to the chat pane width. Long lines no longer
  disappear beneath the evidence pane.

## [0.1.1] - 2026-11-22

### Changed

- The Go module path now matches the GitHub organisation casing
  (`github.com/ProjAnvil/kbcli`); `go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.1.1`
  is the documented remote install.
- `kbcli version` now reports the installed module version for binaries
  installed with `go install pkg@version`, while Makefile `-ldflags` injection
  still takes precedence.

### Added

- A GitHub Actions release workflow cross-compiles darwin/amd64, darwin/arm64,
  linux/amd64, linux/arm64 and windows/amd64 binaries and uploads them as
  release assets on every `v*` tag push.

## [0.1.0] - 2026-11-21

### Added

- Single-file `.kbvec` vector index with a magic header, embedder-domain
  isolation, and atomic writes; legacy `.kbcli.kbvec` files remain loadable.
- Zero-dependency in-process hashing embedder by default and an optional
  pure-Go `rembed` embedder; both are 384-dimensional.
- OpenAI-compatible Chat Completions backend with a deterministic extractive
  fallback, so chat and search work offline.
- Porto-derived retrieval pipeline: vector, BM25, weighted-RRF hybrid,
  reranking, query transforms (HyDE, multi-query, decomposition, step-back),
  query condensation, plus `min_score` and `context_expand`; every
  LLM-dependent stage fails open.
- Bilingual English / Simplified Chinese TUI with a two-pane chat view, a
  master-detail settings screen, and per-workspace session facts with history
  compaction.
- Global `~/.kbcli/config.yaml` plus sparse per-workspace `settings.json`
  overrides, with an INDEX STALE banner driven by a version-2 build signature.
- Cobra commands `index`, `reindex`, `update`, `config`, `settings`, `search`,
  `version`, and in-TUI commands `:reindex`, `:update`, `:settings`, `:stale`,
  `:clear`, `:help`, `:index`.
- All filesystem paths use Go standard-library helpers; the tree cross-compiles
  for Windows, macOS, and Linux. The Windows runtime has NOT been verified on
  real hardware.
- `:index` requires a two-step directory confirmation backed by a bounded
  read-only preflight (20,000 files / 12 levels) whose risk grading protects
  home and system roots.

### Changed

- Startup no longer auto-indexes, no longer walks parent directories, and
  performs no directory walk at all; indexing is strictly on demand.

---

This is the first public release, so there are no prior entries.
