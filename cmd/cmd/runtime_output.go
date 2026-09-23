package cmd

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

func renderRuntimeText(block promptOutputBlock, width int) []string {
	lines := strings.Split(ansi.Wrap(block.text, max(1, width), " "), "\n")

	// Color each physical line so wrapping and viewport scrolling cannot
	// discard the style that started on a previous line.
	for i, line := range lines {
		lines[i] = renderer.FormatMessageLine(line, block.level)
	}

	return lines
}
