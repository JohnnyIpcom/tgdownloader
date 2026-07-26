package renderer

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

const (
	progressLabelMaxWidth = 50
	progressLabelMinWidth = 4
	progressBarMaxWidth   = 23
	progressBarMinWidth   = 3
	progressSeparator     = " ... "
)

var (
	progressMessageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	progressErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	progressPercentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	progressTrackerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	progressStatsStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	progressTimeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	progressValueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

type progressRow struct {
	label      string
	separator  string
	percentage string
	bar        string
	status     string
	value      string
	elapsed    string
	eta        string
}

// FormatProgress reproduces the legacy progress layout with responsive truncation.
func FormatProgress(event Event, width int, frame int) string {
	if width <= 0 {
		return ""
	}

	labelWidth := progressLabelMaxWidth
	barWidth := progressBarMaxWidth
	row := newProgressRow(event, labelWidth, barWidth, frame)

	if row.width() > width {
		row.eta = ""
	}
	if row.width() > width {
		row.value = ""
	}
	if row.width() > width && isTerminalProgress(event.Kind) {
		row.elapsed = ""
	}
	for row.width() > width && labelWidth > progressLabelMinWidth {
		labelWidth--
		row.label = formatProgressLabel(event.Label, labelWidth)
	}
	for row.width() > width && barWidth > progressBarMinWidth {
		barWidth--
		row.bar = progressBar(event, barWidth, frame)
	}
	if row.width() > width {
		row.percentage = ""
	}
	if row.width() > width {
		row.elapsed = ""
	}
	if row.width() > width {
		row.separator = " "
	}

	plain := row.plain()
	if lipgloss.Width(plain) > width {
		return truncateProgressText(plain, width)
	}
	return row.styled(event.Kind)
}

func newProgressRow(event Event, labelWidth, barWidth, frame int) progressRow {
	row := progressRow{
		label:     formatProgressLabel(event.Label, labelWidth),
		separator: progressSeparator,
		bar:       progressBar(event, barWidth, frame),
		elapsed:   formatProgressDuration(event.Elapsed, isTerminalProgress(event.Kind)),
	}

	if event.Total > 0 {
		percentage := float64(event.Current) * 100 / float64(event.Total)
		percentage = math.Max(0, math.Min(100, percentage))
		row.percentage = fmt.Sprintf("%5.1f%%", percentage)
	}

	if event.Unit == ProgressUnitBytes {
		row.value = formatProgressBytes(event.Current)
	} else if event.Current > 0 || event.Total > 0 {
		row.value = formatProgressNumber(event.Current)
	}
	if eta, ok := formatProgressETA(event); ok && !isTerminalProgress(event.Kind) {
		row.eta = eta
	}

	switch event.Kind {
	case EventProgressDone:
		row.status = "done!"
	case EventProgressFail:
		row.status = "fail!"
	}
	return row
}

func (r progressRow) plain() string {
	var out strings.Builder
	out.WriteString(r.label)
	out.WriteString(r.separator)
	if r.percentage != "" {
		out.WriteString(r.percentage)
		out.WriteByte(' ')
	}
	out.WriteString(r.bar)
	if r.status != "" {
		out.WriteByte(' ')
		out.WriteString(r.status)
	}
	if stats := r.plainStats(); stats != "" {
		out.WriteByte(' ')
		out.WriteString(stats)
	}
	return out.String()
}

func (r progressRow) styled(kind EventKind) string {
	var out strings.Builder
	out.WriteString(progressMessageStyle.Render(r.label + r.separator))
	if r.percentage != "" {
		out.WriteString(progressPercentStyle.Render(r.percentage))
		out.WriteByte(' ')
	}
	out.WriteString(progressTrackerStyle.Render(r.bar))
	if r.status != "" {
		out.WriteByte(' ')
		style := progressMessageStyle
		if kind == EventProgressFail {
			style = progressErrorStyle
		}
		out.WriteString(style.Render(r.status))
	}
	if r.value != "" || r.elapsed != "" || r.eta != "" {
		out.WriteByte(' ')
		out.WriteString(progressStatsStyle.Render("["))
		if r.value != "" {
			out.WriteString(progressValueStyle.Render(r.value))
		}
		if r.value != "" && r.elapsed != "" {
			out.WriteString(progressStatsStyle.Render(" in "))
		}
		if r.elapsed != "" {
			out.WriteString(progressTimeStyle.Render(r.elapsed))
		}
		if r.eta != "" {
			out.WriteString(progressStatsStyle.Render("; ~ETA: "))
			out.WriteString(progressTimeStyle.Render(r.eta))
		}
		out.WriteString(progressStatsStyle.Render("]"))
	}
	return out.String()
}

func (r progressRow) plainStats() string {
	if r.value == "" && r.elapsed == "" && r.eta == "" {
		return ""
	}
	var out strings.Builder
	out.WriteByte('[')
	if r.value != "" {
		out.WriteString(r.value)
	}
	if r.value != "" && r.elapsed != "" {
		out.WriteString(" in ")
	}
	if r.elapsed != "" {
		out.WriteString(r.elapsed)
	}
	if r.eta != "" {
		out.WriteString("; ~ETA: ")
		out.WriteString(r.eta)
	}
	out.WriteByte(']')
	return out.String()
}

func (r progressRow) width() int {
	return lipgloss.Width(r.plain())
}

func progressBar(event Event, width int, frame int) string {
	width = max(1, width)
	if event.Total <= 0 {
		switch event.Kind {
		case EventProgressDone:
			return "[" + strings.Repeat("#", width) + "]"
		case EventProgressFail:
			return "[" + strings.Repeat(".", width) + "]"
		}
		return progressIndeterminateBar(width, frame)
	}

	percentage := float64(event.Current) / float64(event.Total)
	percentage = math.Max(0, math.Min(1, percentage))
	filled := int(math.Floor(percentage * float64(width)))
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

func progressIndeterminateBar(width int, frame int) string {
	indicator := "<#>"
	if width < len(indicator) {
		indicator = "#"
	}
	maxPosition := max(0, width-len(indicator))
	position := 0
	if maxPosition > 0 {
		period := maxPosition * 2
		position = frame % period
		if position < 0 {
			position += period
		}
		if position > maxPosition {
			position = period - position
		}
	}
	return "[" + strings.Repeat(".", position) + indicator + strings.Repeat(".", width-position-len(indicator)) + "]"
}

func formatProgressNumber(value int64) string {
	return formatProgressScaled(value, [...]progressScale{
		{1_000_000_000_000_000, "Q"},
		{1_000_000_000_000, "T"},
		{1_000_000_000, "B"},
		{1_000_000, "M"},
		{1_000, "K"},
	})
}

func formatProgressBytes(value int64) string {
	if value < 1000 {
		return fmt.Sprintf("%dB", value)
	}
	return formatProgressScaled(value, [...]progressScale{
		{1_000_000_000_000_000, "PB"},
		{1_000_000_000_000, "TB"},
		{1_000_000_000, "GB"},
		{1_000_000, "MB"},
		{1_000, "KB"},
	})
}

type progressScale struct {
	value  int64
	suffix string
}

func formatProgressScaled(value int64, scales [5]progressScale) string {
	for _, scale := range scales {
		if value >= scale.value {
			return fmt.Sprintf("%.2f%s", float64(value)/float64(scale.value), scale.suffix)
		}
	}
	return fmt.Sprintf("%d", value)
}

func formatProgressDuration(value time.Duration, terminal bool) string {
	precision := time.Microsecond
	if terminal {
		precision = time.Millisecond
	}
	return value.Round(precision).String()
}

func formatProgressETA(event Event) (string, bool) {
	if event.Total <= 0 || event.Current <= 0 || event.Current >= event.Total || event.Elapsed <= 0 {
		return "", false
	}
	remaining := float64(event.Total-event.Current) / float64(event.Current)
	eta := time.Duration(float64(event.Elapsed) * remaining).Round(time.Second)
	return eta.String(), eta > time.Second
}

func isTerminalProgress(kind EventKind) bool {
	return kind == EventProgressDone || kind == EventProgressFail
}

func formatProgressLabel(value string, width int) string {
	value = truncateProgressWithSuffix(value, width, "~")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func truncateProgressText(value string, width int) string {
	return truncateProgressWithSuffix(value, width, "…")
}

func truncateProgressWithSuffix(value string, width int, suffix string) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}

	limit := width - lipgloss.Width(suffix)
	if limit <= 0 {
		return suffix
	}

	var result strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		cluster := graphemes.Str()
		clusterWidth := lipgloss.Width(cluster)
		if used+clusterWidth > limit {
			break
		}
		result.WriteString(cluster)
		used += clusterWidth
	}
	return result.String() + suffix
}
