package renderer

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormatTableRendersAndAlignsAllColumnsAtWideWidth(t *testing.T) {
	table := TableData{
		Columns: []TableColumn{
			{Header: "Name", MinWidth: 8, Priority: 100, Required: true},
			{Header: "ID", MinWidth: 3, Priority: 10, Align: TableAlignRight},
			{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
			{Header: "Type", MinWidth: 4, Priority: 20},
		},
		Rows: [][]string{
			{"Short", "7", "0x0000000000000007", "User"},
			{"Longer name", "123", "0xFFFFFFFFFFFFFF85", "Channel"},
		},
	}

	lines := plainTableLines(FormatTable(table, 120))
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want header and two rows: %q", len(lines), lines)
	}
	for _, want := range []string{"NAME", "ID", "TDLIB PEER ID", "TYPE", "Longer name", "Channel"} {
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Fatalf("table missing %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	idColumn := strings.Index(lines[0], "ID")
	if strings.Index(lines[1], "7") != idColumn+1 {
		t.Fatalf("single-digit ID is not right aligned under header:\n%s", strings.Join(lines, "\n"))
	}
}

func TestFormatTableHidesOptionalColumnsBeforeRequiredColumns(t *testing.T) {
	table := TableData{
		Columns: []TableColumn{
			{Header: "Name", MinWidth: 10, Priority: 100, Required: true},
			{Header: "ID", MinWidth: 5, Priority: 10},
			{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
			{Header: "Type", MinWidth: 7, Priority: 20},
		},
		Rows: [][]string{{"Фотограф внутреннего танца", "123", "0xFFFFFFFFFFFFFF85", "Channel"}},
	}

	lines := plainTableLines(FormatTable(table, 36))
	joined := strings.Join(lines, "\n")
	if strings.Contains(lines[0], "TYPE") || strings.Count(lines[0], "ID") != 1 {
		t.Fatalf("optional columns remain in narrow table:\n%s", joined)
	}
	for _, want := range []string{"NAME", "TDLIB PEER ID", "0xFFFFFFFFFFFFFF85"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("required field %q missing:\n%s", want, joined)
		}
	}
}

func TestFormatTableNeverExceedsWidthAndPreservesUnicode(t *testing.T) {
	table := TableData{
		Columns: []TableColumn{
			{Header: "Name", MinWidth: 4, Priority: 100, Required: true},
			{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
		},
		Rows: [][]string{
			{"👩🏽‍💻👩🏽‍💻👩🏽‍💻 developer", "0x0000000000000001"},
			{"🇺🇦🇺🇦🇺🇦 flags", "0x0000000000000002"},
			{"e\u0301e\u0301e\u0301 combining", "0x0000000000000003"},
		},
	}

	for _, width := range []int{12, 24, 32, 50} {
		for _, line := range FormatTable(table, width) {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width=%d visible=%d line=%q", width, got, ansi.Strip(line))
			}
		}
	}
}

func TestFormatTableHandlesShortAndLongRows(t *testing.T) {
	table := TableData{
		Columns: []TableColumn{{Header: "A", Required: true}, {Header: "B", Required: true}},
		Rows:    [][]string{{"one"}, {"two", "three", "ignored"}},
	}

	lines := plainTableLines(FormatTable(table, 40))
	if len(lines) != 3 || !strings.Contains(lines[1], "one") || !strings.Contains(lines[2], "three") {
		t.Fatalf("malformed rows not rendered safely: %q", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "ignored") {
		t.Fatalf("extra cell leaked into table: %q", lines)
	}
}

func TestFormatTableSanitizesTerminalControlText(t *testing.T) {
	table := TableData{
		Columns: []TableColumn{{Header: "Na\x1b[31mme\n", Required: true}},
		Rows:    [][]string{{"Safe\x1b[31m Red\x1b[0m\nnext\u202e\x00"}},
	}

	lines := FormatTable(table, 80)
	if len(lines) != 2 {
		t.Fatalf("line count = %d, control newline changed table shape", len(lines))
	}
	if strings.Contains(lines[0], "\x1b[31m") || strings.Contains(ansi.Strip(lines[0]), "\n") {
		t.Fatalf("unsafe terminal text remains in header: %q", lines[0])
	}
	if strings.Contains(lines[1], "\x1b[31m") || strings.Contains(lines[1], "\n") ||
		strings.Contains(lines[1], "\u202e") || strings.Contains(lines[1], "\x00") {
		t.Fatalf("unsafe terminal text remains: %q", lines[1])
	}
	if plain := ansi.Strip(lines[1]); !strings.Contains(plain, "Safe Red next") {
		t.Fatalf("safe text lost during sanitization: %q", plain)
	}
}

func plainTableLines(lines []string) []string {
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = ansi.Strip(line)
	}
	return plain
}
