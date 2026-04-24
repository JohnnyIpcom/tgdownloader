package telegram

import (
	"context"
	"testing"
)

func TestDialogServiceSendDialogCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan Dialog)
	s := &dialogService{}
	if err := s.sendDialog(ctx, out, Dialog{}); err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
