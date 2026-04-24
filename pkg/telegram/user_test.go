package telegram

import (
	"context"
	"testing"

	"github.com/gotd/td/telegram/peers"
)

func TestUserServiceSendUserCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan peers.User)
	s := &userService{}
	if err := s.sendUser(ctx, out, peers.User{}); err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
