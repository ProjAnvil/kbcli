# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
