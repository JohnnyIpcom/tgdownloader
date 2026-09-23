package renderer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

var (
	simpleCyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	simpleRedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	simpleYellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

func RenderBye(writer io.Writer) {
	renderSimpleLine(writer, simpleCyanStyle, "Bye! ^_^")
}

func RenderError(writer io.Writer, err error) {
	if err == nil {
		return
	}

	if errors.Is(err, context.Canceled) {
		renderMessageLine(writer, ErrorEvent(err))

		return
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		renderMessageLine(writer, Event{
			Kind:  EventLine,
			Level: LineError,
			Text:  fmt.Sprintf("Error (%s) at %s: %v", appErr.Kind, appErr.Op, appErr.Err),
		})

		return
	}

	renderMessageLine(writer, ErrorEvent(err))
}

func RenderErrorConcise(writer io.Writer, err error) {
	if err == nil {
		return
	}

	renderMessageLine(writer, ErrorEvent(err))
}

// ErrorEvent is the shared concise diagnostic for interactive renderers.
func ErrorEvent(err error) Event {
	if err == nil {
		return Event{Kind: EventLine}
	}

	if errors.Is(err, context.Canceled) {
		return Event{Kind: EventLine, Level: LineWarning, Text: "Interrupted"}
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		err = appErr.Err
	}

	return Event{Kind: EventLine, Level: LineError, Text: fmt.Sprintf("Error: %s", err)}
}

// FormatMessageLine applies styling after callers sanitize and wrap the text.
func FormatMessageLine(text string, level LineLevel) string {
	switch level {
	case LineError:
		// Severity comes from the producer; the prefix only controls emphasis.
		if prefix, rest, ok := strings.Cut(text, ":"); ok && strings.HasPrefix(prefix, "Error") {
			return simpleRedStyle.Bold(true).Render(prefix+":") + simpleRedStyle.Render(rest)
		}

		return simpleRedStyle.Render(text)
	case LineWarning:
		return simpleYellowStyle.Render(text)
	default:
		return text
	}
}

func renderMessageLine(writer io.Writer, event Event) {
	writer = outputWriter(writer)
	if structured, ok := writer.(interface{ EmitLine(string, LineLevel) }); ok {
		structured.EmitLine(event.Text, event.Level)

		return
	}

	text := event.Text
	if terminal, ok := writer.(interface{ Fd() uintptr }); ok && term.IsTerminal(terminal.Fd()) {
		text = FormatMessageLine(text, event.Level)
	}

	fmt.Fprintln(writer, text)
}

func RenderDownloadSummary(writer io.Writer, downloaded, skipped, failed int64) {
	renderSimpleLine(writer, simpleCyanStyle, fmt.Sprintf(
		"Summary: downloaded=%d skipped=%d failed=%d",
		downloaded,
		skipped,
		failed,
	))
}

func renderSimpleLine(writer io.Writer, style lipgloss.Style, value string) {
	writer = outputWriter(writer)
	if terminal, ok := writer.(interface{ Fd() uintptr }); ok && term.IsTerminal(terminal.Fd()) {
		value = style.Render(value)
	}
	fmt.Fprintln(writer, value)
}

func outputWriter(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
