package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/johnnyipcom/tgdownloader/cmd/cmd"
	"github.com/johnnyipcom/tgdownloader/cmd/version"
)

func main() {
	root, err := cmd.NewRoot(version.Version())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root.RunPrompt(ctx)
}
