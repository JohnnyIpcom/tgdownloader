package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	cmd "github.com/johnnyipcom/tgdownloader/cmd/main"
)

func version() string {
	return "0.3.0"
}

func main() {
	root, err := cmd.NewRoot(version())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := root.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
