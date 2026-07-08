package renderer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
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

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		fmt.Println(text.Colors{text.FgRed}.Sprintf("Error (%s) at %s: %v\n", appErr.Kind, appErr.Op, appErr.Err))
		return
	}

	fmt.Println(text.Colors{text.FgRed}.Sprintf("Error: %s\n", err))
}
