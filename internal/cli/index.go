package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/projanvil/kbcli/internal/app"
	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/scan"
)

// newIndexCmd builds "kbcli index [dir]".
func newIndexCmd(cfg *config.Config, g *globals) *cobra.Command {
	var out string
	var chunkSize, chunkOverlap int
	var embedder, model string
	var yes bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "index [dir]",
		Short: "Build a vector index for a directory",
		Long:  "Walk a directory, chunk supported documents, embed them, and write one portable vector file.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := ""
			if len(args) == 1 {
				explicit = args[0]
			}
			root, err := g.resolveRoot(explicit)
			if err != nil {
				return err
			}

			// Flags override persisted settings only when the user passed them
			// explicitly. Flag defaults otherwise clobber a configured model
			// reference with an empty string.
			if cmd.Flags().Changed("chunk-size") {
				cfg.ChunkSize = chunkSize
			}
			if cmd.Flags().Changed("chunk-overlap") {
				cfg.ChunkOverlap = chunkOverlap
			}
			if cmd.Flags().Changed("embedder") {
				cfg.Embedder = embedder
			}
			if cmd.Flags().Changed("model") {
				cfg.EmbedModel = model
			}

			resolveOut := out
			if resolveOut == "" {
				resolveOut = g.index
			}
			indexPath := app.ResolveIndexPath(root, resolveOut, cfg)

			// Bounded preflight: report the real corpus size and refuse to
			// auto-index a home directory, a filesystem root or a truncated
			// scan. This runs before any file is read or embedded.
			report, err := app.Preflight(root)
			if err != nil {
				return fmt.Errorf("preflight: %w", err)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "preflight: %d files · %s · max depth %d · %d dirs · risk %s\n",
				report.Files, scan.HumanBytes(report.Bytes), report.MaxDepth, report.Dirs, report.RiskLabel())
			for _, warn := range report.Warnings {
				fmt.Fprintf(w, "  warning: %s\n", warn)
			}
			if dryRun {
				fmt.Fprintln(w, "dry-run: no index written")
				return nil
			}
			// Explicitly typed path is the first confirmation. A large or
			// protected corpus needs a second one: --yes.
			if !report.CanAutoIndex() && !yes {
				return fmt.Errorf("refusing to auto-index %s (risk: critical); choose a subdirectory or pass --yes", root)
			}
			if report.NeedsSecondConfirm() && !yes {
				return fmt.Errorf("%s looks large (%d files, %s); pass --yes to confirm this directory", root, report.Files, scan.HumanBytes(report.Bytes))
			}

			// A full rebuild must not load the old index: switching the
			// embedder changes the retrieval domain and Load would reject the
			// very file the user is asking to replace.
			a, err := app.OpenFresh(cfg, root)
			if err != nil {
				return fmt.Errorf("open knowledge base: %w", err)
			}

			start := time.Now()
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spin := 0
			n, err := a.BuildWithProgress(commandContext(cmd), root, indexPath, func(p app.Progress) {
				if p.Phase != "indexing" {
					return
				}
				fmt.Fprintf(w, "\r%s indexing %d/%d · %d chunks · %s",
					frames[spin%len(frames)], p.FilesDone, p.FilesTotal, p.Chunks, p.Current)
				spin++
			})
			if spin > 0 {
				fmt.Fprintln(w)
			}
			if err != nil {
				return fmt.Errorf("build index: %w", err)
			}
			elapsed := time.Since(start).Round(time.Millisecond)

			fmt.Fprintf(w, "chunks indexed: %d\n", n)
			fmt.Fprintf(w, "index: %s\n", indexPath)
			fmt.Fprintf(w, "elapsed: %s\n", elapsed)
			return nil
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "output vector file (default: <dir>/.kbvec)")
	cmd.Flags().IntVar(&chunkSize, "chunk-size", cfg.ChunkSize, "chunk size in runes")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", cfg.ChunkOverlap, "overlap between chunks in runes")
	cmd.Flags().StringVar(&embedder, "embedder", cfg.Embedder, "embedding provider (local|rembed)")
	cmd.Flags().StringVar(&model, "model", cfg.EmbedModel, "embedding model reference")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm a large or protected directory before indexing")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the preflight report without indexing")
	return cmd
}

// newReindexCmd builds "kbcli reindex".
func newReindexCmd(cfg *config.Config, g *globals) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the current index from scratch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := g.resolveRoot("")
			if err != nil {
				return err
			}
			indexPath := g.index
			if indexPath == "" {
				indexPath = app.Discover(cwd, cfg)
			}
			if indexPath == "" {
				return errNoIndex
			}
			if _, err := os.Stat(indexPath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return errNoIndex
				}
				return fmt.Errorf("stat index %s: %w", indexPath, err)
			}

			// Read the recorded root from the header without loading vectors.
			// This keeps reindex working across an embedder change and from a
			// different working directory, including when --out is used.
			root, err := app.IndexRoot(indexPath)
			if err != nil {
				return fmt.Errorf("read index header: %w", err)
			}
			if resolved, err := app.ResolveRoot(root); err == nil {
				root = resolved
			}

			a, err := app.OpenFresh(cfg, root)
			if err != nil {
				return fmt.Errorf("open knowledge base: %w", err)
			}
			outPath := indexPath
			if out != "" {
				outPath = app.ResolveIndexPath(root, out, cfg)
			}
			n, err := a.Build(commandContext(cmd), root, outPath)
			if err != nil {
				return fmt.Errorf("reindex: %w", err)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "chunks indexed: %d\n", n)
			fmt.Fprintf(w, "index: %s\n", outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output vector file (default: overwrite the current index)")
	return cmd
}

// newUpdateCmd builds "kbcli update [path]".
func newUpdateCmd(cfg *config.Config, g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [path]",
		Short: "Incrementally update the index for a file or directory",
		Long:  "Re-index changed files under a path (or the whole index root when omitted).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := g.openExisting(cfg)
			if err != nil {
				return err
			}
			// An empty target tells the app to use the recorded index root.
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			n, err := a.Update(commandContext(cmd), target)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "chunks added/updated: %d\n", n)
			return nil
		},
	}
	return cmd
}
