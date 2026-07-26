package renderer

import (
	"context"
	"errors"
	"fmt"
	"io"

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
		renderSimpleLine(writer, simpleYellowStyle, "Interrupted")
		return
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		renderSimpleLine(writer, simpleRedStyle, fmt.Sprintf("Error (%s) at %s: %v", appErr.Kind, appErr.Op, appErr.Err))
		return
	}
	renderSimpleLine(writer, simpleRedStyle, fmt.Sprintf("Error: %s", err))
}

func RenderErrorConcise(writer io.Writer, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		renderSimpleLine(writer, simpleYellowStyle, "Interrupted")
		return
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		err = appErr.Err
	}
	renderSimpleLine(writer, simpleRedStyle, fmt.Sprintf("Error: %s", err))
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
