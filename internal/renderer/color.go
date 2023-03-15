package renderer

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/text"
)

type Color int

// Base colors -- attributes in reality
const (
	Reset Color = iota
	Bold
	Faint
	Italic
	Underline
	BlinkSlow
	BlinkRapid
	ReverseVideo
	Concealed
	CrossedOut
)

// Foreground colors
const (
	FgBlack Color = iota + 30
	FgRed
	FgGreen
	FgYellow
	FgBlue
	FgMagenta
	FgCyan
	FgWhite
)

// Foreground Hi-Intensity colors
const (
	FgHiBlack Color = iota + 90
	FgHiRed
	FgHiGreen
	FgHiYellow
	FgHiBlue
	FgHiMagenta
	FgHiCyan
	FgHiWhite
)

// Background colors
const (
	BgBlack Color = iota + 40
	BgRed
	BgGreen
	BgYellow
	BgBlue
	BgMagenta
	BgCyan
	BgWhite
)

// Background Hi-Intensity colors
const (
	BgHiBlack Color = iota + 100
	BgHiRed
	BgHiGreen
	BgHiYellow
	BgHiBlue
	BgHiMagenta
	BgHiCyan
	BgHiWhite
)

type Colors []Color

// Sprint colorizes and prints the given string(s).
func Sprint(c Colors, a ...interface{}) string {
	colors := make([]text.Color, len(c))
	for i, color := range c {
		colors[i] = text.Color(color)
	}

	return colorize(fmt.Sprint(a...), text.Colors(colors).EscapeSeq())
}

// Sprintf formats and colorizes and prints the given string(s).
func Sprintf(c Colors, format string, a ...interface{}) string {
	colors := make([]text.Color, len(c))
	for i, color := range c {
		colors[i] = text.Color(color)
	}

	return colorize(fmt.Sprintf(format, a...), text.Colors(colors).EscapeSeq())
}

// Printf colorizes and prints the given string(s).
func Printf(c Colors, format string, a ...interface{}) {
	text := Sprintf(c, format, a...)
	fmt.Printf("%s", text)
}

// Println colorizes and prints the given string(s).
func Println(c Colors, a ...interface{}) {
	text := Sprint(c, a...)
	fmt.Println(text)
}

func colorize(s string, escapeSeq string) string {
	if !text.ANSICodesSupported || escapeSeq == "" {
		return s
	}

	return text.Escape(s, escapeSeq)
}
