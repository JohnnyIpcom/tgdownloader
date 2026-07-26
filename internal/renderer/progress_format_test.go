package renderer

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormatProgressKnownTotalUsesASCIIBar(t *testing.T) {
	row := plainProgressRow(FormatProgress(Event{
		Kind:    EventProgressUpdate,
		Label:   "video.mp4",
		Current: 50,
		Total:   100,
		Elapsed: 1500 * time.Millisecond,
	}, 140, 0))

	want := "video.mp4" + strings.Repeat(" ", 41) + " ... 50.0% [###########............] [50 in 1.5s; ~ETA: 2s]"
	if row != want {
		t.Fatalf("row = %q\nwant  %q", row, want)
	}
	if strings.ContainsAny(row, "█░") {
		t.Fatalf("row = %q, contains non-ASCII bar characters", row)
	}
}

func TestFormatProgressPlacesTerminalStatusAfterBar(t *testing.T) {
	for _, kind := range []EventKind{EventProgressDone, EventProgressFail} {
		row := plainProgressRow(FormatProgress(Event{
			Kind:    kind,
			Label:   "Authentication",
			Current: 1,
			Total:   1,
			Elapsed: 55 * time.Millisecond,
		}, 140, 0))
		status := "done!"
		if kind == EventProgressFail {
			status = "fail!"
		}
		barEnd := strings.Index(row, "]")
		statusStart := strings.Index(row, status)
		if barEnd < 0 || statusStart <= barEnd {
			t.Fatalf("row = %q, status must follow bar", row)
		}
		if !strings.Contains(row, "[#######################] "+status+" [1 in 55ms]") {
			t.Fatalf("row = %q, want terminal status and elapsed", row)
		}
	}
}

func TestFormatProgressFormatsByteValues(t *testing.T) {
	row := plainProgressRow(FormatProgress(Event{
		Kind:    EventProgressUpdate,
		Label:   "archive.bin",
		Current: 3_670_000,
		Total:   9_180_000,
		Unit:    ProgressUnitBytes,
		Elapsed: time.Second,
	}, 140, 0))

	if !strings.Contains(row, " ... 40.0% [#########..............] [3.67MB in 1s; ~ETA: 2s]") {
		t.Fatalf("row = %q, want legacy decimal byte stats", row)
	}
}

func TestFormatProgressTerminalBytesIncludeTransferredAndElapsed(t *testing.T) {
	row := plainProgressRow(FormatProgress(Event{
		Kind:    EventProgressFail,
		Label:   "video.mp4",
		Current: 3_670_000,
		Total:   9_180_000,
		Unit:    ProgressUnitBytes,
		Elapsed: 4221 * time.Millisecond,
	}, 140, 0))

	if !strings.Contains(row, "[#########..............] fail! [3.67MB in 4.221s]") {
		t.Fatalf("row = %q, want status, transferred bytes, and elapsed", row)
	}
}

func TestFormatProgressUnknownTotalAnimatesInsideASCIIBar(t *testing.T) {
	first := plainProgressRow(FormatProgress(Event{Label: "Connecting"}, 80, 0))
	second := plainProgressRow(FormatProgress(Event{Label: "Connecting"}, 80, 1))

	if first == second {
		t.Fatalf("unknown-total rows did not animate: %q", first)
	}
	for _, row := range []string{first, second} {
		start, end := strings.Index(row, "["), strings.Index(row, "]")
		if start < 0 || end <= start || !strings.Contains(row[start+1:end], "<#>") {
			t.Fatalf("row = %q, want legacy moving marker", row)
		}
	}
}

func TestFormatProgressUnknownTerminalBarHasStableState(t *testing.T) {
	done := plainProgressRow(FormatProgress(Event{Kind: EventProgressDone, Label: "Connecting"}, 100, 7))
	failed := plainProgressRow(FormatProgress(Event{Kind: EventProgressFail, Label: "Connecting"}, 100, 7))

	if !strings.Contains(done, "[#######################]") {
		t.Fatalf("done row = %q, want full terminal bar", done)
	}
	if !strings.Contains(failed, "[.......................]") {
		t.Fatalf("failed row = %q, want empty terminal bar", failed)
	}
}

func TestFormatProgressAlignsBarsForDifferentLabels(t *testing.T) {
	short := plainProgressRow(FormatProgress(Event{Label: "a", Current: 1, Total: 2}, 72, 0))
	long := plainProgressRow(FormatProgress(Event{Label: "a much longer filename.mp4", Current: 1, Total: 2}, 72, 0))

	shortBar := lipgloss.Width(short[:strings.Index(short, "[")])
	longBar := lipgloss.Width(long[:strings.Index(long, "[")])
	if shortBar != longBar {
		t.Fatalf("bar columns differ: short=%d long=%d\n%s\n%s", shortBar, longBar, short, long)
	}
}

func TestFormatProgressNeverExceedsWidthAndPreservesGraphemes(t *testing.T) {
	labels := []string{
		"Фотограф внутреннего танца",
		"e\u0301e\u0301e\u0301 long combining name",
		"👩🏽‍💻👩🏽‍💻👩🏽‍💻 developer channel",
		"🇺🇦🇺🇦🇺🇦 regional flags",
	}
	for _, width := range []int{12, 20, 30, 50, 80} {
		for _, label := range labels {
			row := FormatProgress(Event{Label: label, Current: 1, Total: 2}, width, 0)
			if got := lipgloss.Width(row); got > width {
				t.Fatalf("width=%d visible=%d row=%q", width, got, plainProgressRow(row))
			}
		}
	}
}

func TestTruncateProgressTextPreservesWholeGraphemes(t *testing.T) {
	tests := []struct {
		value string
		width int
		want  string
	}{
		{value: "e\u0301e\u0301e\u0301", width: 2, want: "e\u0301…"},
		{value: "👩🏽‍💻xx", width: 3, want: "👩🏽‍💻…"},
		{value: "🇺🇦xx", width: 3, want: "🇺🇦…"},
	}
	for _, test := range tests {
		if got := truncateProgressText(test.value, test.width); got != test.want {
			t.Fatalf("truncateProgressText(%q, %d) = %q, want %q", test.value, test.width, got, test.want)
		}
	}
}

func plainProgressRow(value string) string {
	return ansi.Strip(value)
}
