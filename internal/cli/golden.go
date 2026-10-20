package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/workspace"
)

// GoldenCache is the golden tree cache `golden list|gc` drives.
// *workspace.GoldenCache implements it.
type GoldenCache interface {
	List(ctx context.Context, projectID string) ([]workspace.GoldenInfo, error)
	GC(ctx context.Context, projectID string, budgetBytes int64) (workspace.GCReport, error)
}

func newGoldenCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	list := &cobra.Command{
		Use:   "list",
		Short: "List golden trees",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGoldenList(cmd.Context(), ios, deps, st)
		},
	}
	gc := &cobra.Command{
		Use:   "gc",
		Short: "Remove unused golden trees",
		Long: "Remove the least recently used golden trees until the cache fits " +
			"[scheduler].golden_cache_gb. A tree in use is never removed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGoldenGC(cmd.Context(), ios, deps, st)
		},
	}
	return group("golden", "Manage golden trees", list, gc)
}

// goldenCache opens the backend and returns its golden cache.
func goldenCache(ctx context.Context, deps Deps, command string) (*openedBackend, GoldenCache, error) {
	o, err := openProject(ctx, deps, command)
	if err != nil {
		return nil, nil, err
	}
	if o.be.Golden == nil {
		o.close()
		return nil, nil, &ExitError{Code: exitUnsupported, Err: fmt.Errorf("%s is not available in this build", command)}
	}
	return o, o.be.Golden, nil
}

// goldenDoc is one entry of the golden list --json array.
type goldenDoc struct {
	SHA      string `json:"sha"`
	Path     string `json:"path"`
	Size     int64  `json:"size_bytes"`
	Files    int    `json:"files"`
	State    string `json:"state"`
	InUse    int    `json:"in_use"`
	LastUsed string `json:"last_used"` // RFC 3339 UTC; empty when never used
}

func runGoldenList(ctx context.Context, ios IOStreams, deps Deps, st *runState) error {
	o, cache, err := goldenCache(ctx, deps, "golden list")
	if err != nil {
		return err
	}
	defer o.close()
	infos, err := cache.List(ctx, o.project.ID)
	if err != nil {
		return workspaceErr(err)
	}
	docs := make([]goldenDoc, 0, len(infos))
	for _, g := range infos {
		docs = append(docs, goldenDoc{
			SHA: g.BaseSHA, Path: g.Dir, Size: g.SizeBytes, Files: g.FileCount,
			State: string(g.State), InUse: g.InUse, LastUsed: formatTime(g.LastUsedAt),
		})
	}
	return printResult(ios, st.json, docs, func(w io.Writer) error {
		if len(docs) == 0 {
			_, err := io.WriteString(w, "no golden trees\n")
			return err
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "SHA\tSIZE\tFILES\tSTATE\tIN_USE\tLAST_USED")
		for _, d := range docs {
			last := d.LastUsed
			if last == "" {
				last = "-"
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\n", d.SHA, formatBytes(d.Size), d.Files, d.State, d.InUse, last)
		}
		return tw.Flush()
	})
}

// gcDoc is the golden gc --json document.
type gcDoc struct {
	Removed      []string `json:"removed"`
	FreedBytes   int64    `json:"freed_bytes"`
	SkippedInUse []string `json:"skipped_in_use"`
	Pending      []string `json:"pending"`
	FailedRows   []string `json:"failed_rows"`
	Warnings     []string `json:"warnings"`
}

func runGoldenGC(ctx context.Context, ios IOStreams, deps Deps, st *runState) error {
	o, cache, err := goldenCache(ctx, deps, "golden gc")
	if err != nil {
		return err
	}
	defer o.close()
	budget := int64(o.cfg.Scheduler.GoldenCacheGB) << 30
	rep, err := cache.GC(ctx, o.project.ID, budget)
	if err != nil {
		return workspaceErr(err)
	}
	for _, w := range rep.Warnings {
		// stderr only, so the --json document on stdout stays parseable.
		_, _ = fmt.Fprintf(ios.Err, "warning: %s\n", w)
	}
	doc := gcDoc{
		Removed: nonNil(rep.Removed), FreedBytes: rep.FreedBytes, SkippedInUse: nonNil(rep.SkippedInUse),
		Pending: nonNil(rep.Pending), FailedRows: nonNil(rep.FailedRows), Warnings: nonNil(rep.Warnings),
	}
	return printResult(ios, st.json, doc, func(w io.Writer) error {
		lines := []string{fmt.Sprintf("removed %d golden tree(s), freed %s", len(doc.Removed), formatBytes(doc.FreedBytes))}
		for _, s := range doc.Removed {
			lines = append(lines, "  removed  "+s)
		}
		for _, s := range doc.SkippedInUse {
			lines = append(lines, "  skipped  "+s+" (in use)")
		}
		for _, s := range doc.Pending {
			lines = append(lines, "  pending  "+s+" (removal failed, retried by the next gc)")
		}
		for _, s := range doc.FailedRows {
			lines = append(lines, "  cleaned  "+s+" (failed build)")
		}
		_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
		return err
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatBytes renders n in binary units, as in "1.2 GB" (GB = 2^30, like
// the sizes cowflow accepts).
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}
