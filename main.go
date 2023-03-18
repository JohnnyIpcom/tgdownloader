package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/johnnyipcom/tgdownloader/cmd/cmd"
	"github.com/johnnyipcom/tgdownloader/cmd/version"
)

func main() {
	root, err := cmd.NewRoot(version.Version())
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		panic(err)
	}
}
