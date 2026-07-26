package renderer

import (
	"context"
	"fmt"
	"io"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"golang.org/x/sync/errgroup"
)

func RenderTDLibPeerID(peerID constant.TDLibPeerID) string {
	return fmt.Sprintf("0x%016X", uint64(peerID))
}

func RenderAccessHash(accessHash int64) string {
	return fmt.Sprintf("0x%016X", uint64(accessHash))
}

func RenderName(name string) string {
	return truncateProgressWithSuffix(name, 35, "...")
}

func getUsername(user peers.User) string {
	username, ok := user.Username()
	if !ok {
		return "<empty>"
	}
	return username
}

func getVisibleName(p peers.Peer) string {
	if name := p.VisibleName(); name != "" {
		return name
	}

	if user, ok := p.(peers.User); ok {
		if username, ok := user.Username(); ok && username != "" {
			return "@" + username
		}
		if user.Deleted() {
			return "<deleted user>"
		}
		return fmt.Sprintf("<user %d>", user.ID())
	}
	return fmt.Sprintf("<peer %d>", p.ID())
}

func getPeerTypename(p peers.Peer) string {
	switch p.(type) {
	case peers.User:
		return "User"
	case peers.Chat:
		return "Chat"
	case peers.Channel:
		return "Channel"
	default:
		return fmt.Sprintf("Unknown peer type: %T", p)
	}
}

func renderAsync[T any](
	ctx context.Context,
	writer io.Writer,
	ch <-chan T,
	message string,
	total int,
	renderSync func(io.Writer, []T),
) error {
	progress := NewProgressForContext(ctx)
	tracker := progress.UnitsTracker(message, total)

	var collection []T
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case data, ok := <-ch:
				if !ok {
					return nil
				}
				tracker.Increment(1)
				collection = append(collection, data)
			}
		}
	})

	if err := g.Wait(); err != nil {
		tracker.Fail()
		return err
	}
	tracker.Done()
	progress.WaitAndStop(ctx)
	renderSync(writer, collection)
	return nil
}
