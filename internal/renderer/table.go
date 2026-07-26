package renderer

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

type TableAlign uint8

const (
	TableAlignLeft TableAlign = iota
	TableAlignRight
)

type TableColumn struct {
	Header   string
	MinWidth int
	Priority int
	Align    TableAlign
	Required bool
}

type TableData struct {
	Columns []TableColumn
	Rows    [][]string
}

var tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))

// FormatTable lays out a structured table within the supplied terminal width.
func FormatTable(data TableData, width int) []string {
	if width <= 0 || len(data.Columns) == 0 {
		return nil
	}

	columns := make([]tableLayoutColumn, len(data.Columns))
	for i, column := range data.Columns {
		column.Header = SanitizeTerminalText(column.Header, false)
		columns[i] = tableLayoutColumn{
			index:    i,
			column:   column,
			included: true,
			width:    max(1, max(column.MinWidth, lipgloss.Width(strings.ToUpper(column.Header)))),
		}
	}
	for _, row := range data.Rows {
		for i := range columns {
			if i < len(row) {
				columns[i].width = max(columns[i].width, lipgloss.Width(SanitizeTerminalText(row[i], true)))
			}
		}
	}

	for tableLayoutWidth(columns) > width {
		candidate := optionalTableColumn(columns)
		if candidate < 0 {
			break
		}
		columns[candidate].included = false
	}
	shrinkTableColumns(columns, width)

	lines := make([]string, 0, len(data.Rows)+1)
	header := make([]string, len(columns))
	for i, layout := range columns {
		header[i] = strings.ToUpper(layout.column.Header)
	}
	lines = append(lines, tableHeaderStyle.Render(formatTableRow(columns, header)))
	for _, row := range data.Rows {
		lines = append(lines, formatTableRow(columns, row))
	}
	return lines
}

// CloneTableData makes table events independent from producer-owned slices.
func CloneTableData(data TableData) TableData {
	clone := TableData{Columns: append([]TableColumn(nil), data.Columns...)}
	clone.Rows = make([][]string, len(data.Rows))
	for i, row := range data.Rows {
		clone.Rows[i] = append([]string(nil), row...)
	}
	return clone
}

type structuredTableWriter interface {
	EmitTable(TableData)
}

func renderTableData(writer io.Writer, data TableData) string {
	rendered := strings.Join(FormatTable(data, 120), "\n")
	if structured, ok := writer.(structuredTableWriter); ok {
		structured.EmitTable(data)
		return rendered
	}
	if rendered != "" {
		fmt.Fprintln(outputWriter(writer), rendered)
	}
	return rendered
}

type tableLayoutColumn struct {
	index    int
	column   TableColumn
	width    int
	included bool
}

func optionalTableColumn(columns []tableLayoutColumn) int {
	candidates := make([]int, 0, len(columns))
	included := 0
	for i, column := range columns {
		if !column.included {
			continue
		}
		included++
		if !column.column.Required {
			candidates = append(candidates, i)
		}
	}
	if included <= 1 || len(candidates) == 0 {
		return -1
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := columns[candidates[i]], columns[candidates[j]]
		if left.column.Priority == right.column.Priority {
			return left.index < right.index
		}
		return left.column.Priority < right.column.Priority
	})
	return candidates[0]
}

func shrinkTableColumns(columns []tableLayoutColumn, width int) {
	for tableLayoutWidth(columns) > width {
		candidate := -1
		for i, column := range columns {
			if !column.included || column.width <= max(1, column.column.MinWidth) {
				continue
			}
			if candidate < 0 || column.width > columns[candidate].width {
				candidate = i
			}
		}
		if candidate < 0 {
			break
		}
		columns[candidate].width--
	}
	for tableLayoutWidth(columns) > width {
		candidate := -1
		for i, column := range columns {
			if column.included && column.width > 1 && (candidate < 0 || column.width > columns[candidate].width) {
				candidate = i
			}
		}
		if candidate < 0 {
			break
		}
		columns[candidate].width--
	}
}

func tableLayoutWidth(columns []tableLayoutColumn) int {
	width := 0
	count := 0
	for _, column := range columns {
		if column.included {
			width += column.width
			count++
		}
	}
	return width + max(0, count-1)*2
}

func formatTableRow(columns []tableLayoutColumn, row []string) string {
	cells := make([]string, 0, len(columns))
	for _, layout := range columns {
		if !layout.included {
			continue
		}
		value := ""
		if layout.index < len(row) {
			value = SanitizeTerminalText(row[layout.index], true)
		}
		value = truncateProgressText(value, layout.width)
		padding := strings.Repeat(" ", max(0, layout.width-lipgloss.Width(value)))
		if layout.column.Align == TableAlignRight {
			value = padding + value
		} else {
			value += padding
		}
		cells = append(cells, value)
	}
	return strings.Join(cells, "  ")
}
