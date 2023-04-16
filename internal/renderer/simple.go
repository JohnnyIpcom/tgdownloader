package renderer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jedib0t/go-pretty/v6/text"
)

func RenderBye() {
	fmt.Println(text.Colors{text.FgCyan}.Sprint("Bye! ^_^"))
}

func RenderError(err error) {
	if err == nil {
		return
	} else if errors.Is(err, context.Canceled) {
		fmt.Println(text.Colors{text.FgYellow}.Sprint("Interrupted"))
		return
	}

	fmt.Println(text.Colors{text.FgRed}.Sprintf("Error: %s\n", err))
}
