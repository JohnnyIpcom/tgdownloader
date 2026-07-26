package renderer

import (
	"strings"
	"unicode"
)

// SanitizeTerminalText removes terminal control sequences while preserving display Unicode.
func SanitizeTerminalText(value string, preserveSpaces bool) string {
	var out strings.Builder
	lastWasSpace := true
	escapeState := 0
	for _, r := range value {
		switch escapeState {
		case 1:
			switch r {
			case '[':
				escapeState = 2
			case ']':
				escapeState = 3
			default:
				escapeState = 0
			}
			continue
		case 2:
			if r >= 0x40 && r <= 0x7e {
				escapeState = 0
			}
			continue
		case 3:
			if r == '\a' {
				escapeState = 0
			} else if r == 0x1b {
				escapeState = 4
			}
			continue
		case 4:
			if r == '\\' {
				escapeState = 0
			} else {
				escapeState = 3
			}
			continue
		}

		switch {
		case r == 0x1b:
			escapeState = 1
		case r == 0x9b:
			escapeState = 2
		case r == 0x9d:
			escapeState = 3
		case isTerminalBidiControl(r):
			continue
		case r == ' ':
			if preserveSpaces || !lastWasSpace {
				out.WriteRune(r)
			}
			lastWasSpace = true
		case unicode.IsSpace(r):
			if !lastWasSpace {
				out.WriteByte(' ')
				lastWasSpace = true
			}
		case unicode.IsControl(r):
			if !preserveSpaces && !lastWasSpace {
				out.WriteByte(' ')
				lastWasSpace = true
			}
		case r == 0x200c || r == 0x200d:
			out.WriteRune(r)
		case unicode.IsGraphic(r):
			out.WriteRune(r)
			lastWasSpace = false
		}
	}
	return strings.TrimSpace(out.String())
}

func isTerminalBidiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f ||
		(r >= 0x202a && r <= 0x202e) ||
		(r >= 0x2066 && r <= 0x2069)
}
