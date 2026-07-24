package telegram

import (
	"context"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/bin"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type authTransferContextKey struct{}

type reentrantFloodWaiter struct {
	scheduled *floodwait.Waiter
	direct    *floodwait.SimpleWaiter
}

func newReentrantFloodWaiter(log *zap.Logger) *reentrantFloodWaiter {
	scheduled := floodwait.NewWaiter()
	if log != nil {
		scheduled = scheduled.WithCallback(func(_ context.Context, wait floodwait.FloodWait) {
			log.Named("floodwait").Warn("telegram flood wait", zap.Duration("wait", wait.Duration))
		})
	}

	return &reentrantFloodWaiter{
		scheduled: scheduled,
		direct: floodwait.NewSimpleWaiter().
			WithMaxRetries(5).
			WithMaxWait(time.Minute),
	}
}

func (w *reentrantFloodWaiter) Handle(next tg.Invoker) tgclient.InvokeFunc {
	scheduled := w.scheduled.Handle(next)
	direct := w.direct.Handle(next)

	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		if isAuthTransfer(ctx) {
			return direct(ctx, input, output)
		}
		return scheduled(ctx, input, output)
	}
}

func (w *reentrantFloodWaiter) Run(ctx context.Context, fn func(context.Context) error) error {
	return w.scheduled.Run(ctx, fn)
}

func markAuthTransfer(ctx context.Context) context.Context {
	return context.WithValue(ctx, authTransferContextKey{}, true)
}

func isAuthTransfer(ctx context.Context) bool {
	marked, _ := ctx.Value(authTransferContextKey{}).(bool)
	return marked
}
