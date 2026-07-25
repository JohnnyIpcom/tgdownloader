package renderer

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type DownloadPlan struct {
	Name      string
	Type      string
	PeerID    string
	OutputDir string
	Rewrite   bool
	DryRun    bool
}

func FormatDownloadPlan(plan DownloadPlan) string {
	policy := "skip existing"
	if plan.Rewrite {
		policy = "rewrite existing"
	}
	if plan.DryRun {
		policy += ", dry run"
	}

	name := strings.TrimSpace(plan.Name)
	if name == "" {
		name = "<unnamed>"
	}
	return fmt.Sprintf(
		"Target: %s | %s | %s\nOutput: %s | %s",
		name,
		plan.Type,
		plan.PeerID,
		plan.OutputDir,
		policy,
	)
}

func RenderDownloadPlan(writer io.Writer, plan DownloadPlan) {
	fmt.Fprintln(outputWriter(writer), FormatDownloadPlan(plan))
}

func RenderDownloadSummaryDetails(writer io.Writer, downloaded, skipped, failed int64, elapsed time.Duration, outputDir string) {
	fmt.Fprintf(
		outputWriter(writer),
		"Summary: downloaded=%d skipped=%d failed=%d | elapsed=%s | output=%s\n",
		downloaded,
		skipped,
		failed,
		elapsed.Round(time.Millisecond),
		outputDir,
	)
}
