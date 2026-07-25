package renderer

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestFormatDownloadPlanShowsResolvedTargetAndPolicy(t *testing.T) {
	got := FormatDownloadPlan(DownloadPlan{
		Name:      "Cherry Channel",
		Type:      "Channel",
		PeerID:    "0x000000000000007B",
		OutputDir: "./downloads",
		DryRun:    true,
	})

	for _, want := range []string{
		"Cherry Channel",
		"Channel",
		"0x000000000000007B",
		"./downloads",
		"skip existing, dry run",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderDownloadPlanWritesThroughEventWriter(t *testing.T) {
	sink := &recordingSink{}
	RenderDownloadPlan(NewEventWriter(sink), DownloadPlan{Name: "Cherry", Type: "Channel"})

	if got := eventTexts(sink.Events()); !reflect.DeepEqual(got, []string{
		"Target: Cherry | Channel | ",
		"Output:  | skip existing",
	}) {
		t.Fatalf("events = %q", got)
	}
}

func TestRenderDownloadSummaryPreservesOneShotText(t *testing.T) {
	var output bytes.Buffer
	RenderDownloadSummary(&output, 3, 2, 1)

	if got, want := output.String(), "Summary: downloaded=3 skipped=2 failed=1\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
