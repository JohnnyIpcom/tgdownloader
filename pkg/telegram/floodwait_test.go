package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type nestedTransferInvoker struct {
	middleware *reentrantFloodWaiter
}

func (i *nestedTransferInvoker) Invoke(ctx context.Context, _ bin.Encoder, _ bin.Decoder) error {
	return i.middleware.Handle(tgclient.InvokeFunc(func(context.Context, bin.Encoder, bin.Decoder) error {
		return nil
	})).Invoke(markAuthTransfer(ctx), &tg.HelpGetNearestDCRequest{}, &tg.NearestDC{})
}

func TestReentrantFloodWaiterDoesNotDeadlockAuthTransfer(t *testing.T) {
	t.Parallel()

	waiter := newReentrantFloodWaiter(nil)
	outer := waiter.Handle(&nestedTransferInvoker{middleware: waiter})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- waiter.Run(ctx, func(context.Context) error {
			return outer.Invoke(ctx, &tg.HelpGetNearestDCRequest{}, &tg.NearestDC{})
		})
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("nested auth transfer failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("nested auth transfer deadlocked")
	}
}
