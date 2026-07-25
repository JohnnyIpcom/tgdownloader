package renderer

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

func RenderBye(writer io.Writer) {
	fmt.Fprintln(outputWriter(writer), text.Colors{text.FgCyan}.Sprint("Bye! ^_^"))
}

func RenderError(writer io.Writer, err error) {
	writer = outputWriter(writer)
	if err == nil {
		return
	} else if errors.Is(err, context.Canceled) {
		fmt.Fprintln(writer, text.Colors{text.FgYellow}.Sprint("Interrupted"))
		return
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		fmt.Fprintln(writer, text.Colors{text.FgRed}.Sprintf("Error (%s) at %s: %v\n", appErr.Kind, appErr.Op, appErr.Err))
		return
	}

	fmt.Fprintln(writer, text.Colors{text.FgRed}.Sprintf("Error: %s\n", err))
}

func RenderErrorConcise(writer io.Writer, err error) {
	writer = outputWriter(writer)
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(writer, text.Colors{text.FgYellow}.Sprint("Interrupted"))
		return
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		err = appErr.Err
	}
	fmt.Fprintln(writer, text.Colors{text.FgRed}.Sprintf("Error: %s\n", err))
}

func RenderDownloadSummary(writer io.Writer, downloaded, skipped, failed int64) {
	fmt.Fprintln(outputWriter(writer), text.Colors{text.FgCyan}.Sprintf(
		"Summary: downloaded=%d skipped=%d failed=%d",
		downloaded,
		skipped,
		failed,
	))
}

func outputWriter(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
