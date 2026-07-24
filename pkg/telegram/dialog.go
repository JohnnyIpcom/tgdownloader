package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type Dialog struct {
	DialogPeer
	err error
}

func (d Dialog) Err() error {
	return d.err
}

type DialogService interface {
	GetAllDialogs(ctx context.Context) (<-chan Dialog, int, error)
}

type dialogService service

var _ DialogService = (*dialogService)(nil)

func (s *dialogService) sendDialog(ctx context.Context, out chan<- Dialog, dialog Dialog) error {
	select {
	case out <- dialog:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *dialogService) GetAllDialogs(ctx context.Context) (<-chan Dialog, int, error) {
	queryBuilder := query.GetDialogs(s.client.API())
	queryBuilder.BatchSize(100)

	dialogsChan := make(chan Dialog)

	count, err := queryBuilder.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	go func() {
		defer close(dialogsChan)

		peers := make([]storage.Peer, 0, count)
		refreshErr := queryBuilder.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			peer, ok := dialogPeer(elem)
			if !ok {
				err := fmt.Errorf("dialog peer entity not found for %T", elem.Dialog.GetPeer())
				if sendErr := s.sendDialog(ctx, dialogsChan, Dialog{err: err}); sendErr != nil {
					return sendErr
				}
				return err
			}

			peers = append(peers, peer)

			if err := s.sendDialog(ctx, dialogsChan, Dialog{DialogPeer: DialogPeer{Peer: peer}}); err != nil {
				return err
			}
			return nil
		})
		if refreshErr != nil {
			if !errors.Is(refreshErr, context.Canceled) {
				s.logger.Error("failed to get dialogs", zap.Error(refreshErr))
			}
			return
		}

		if err := s.client.dialogCache.ReplaceDialogs(ctx, peers); err != nil {
			s.logger.Error("failed to replace dialog cache", zap.Error(err))
			_ = s.sendDialog(ctx, dialogsChan, Dialog{err: err})
		}
	}()

	return dialogsChan, count, err
}

func (c *Client) bootstrapDialogCache(ctx context.Context) error {
	dialogsChan, _, err := c.DialogService.GetAllDialogs(ctx)
	if err != nil {
		return err
	}
	for dialog := range dialogsChan {
		if dialog.Err() != nil {
			return dialog.Err()
		}
	}
	return nil
}

func dialogPeer(elem dialogs.Elem) (storage.Peer, bool) {
	var peer storage.Peer
	switch dialog := elem.Dialog.GetPeer().(type) {
	case *tg.PeerUser:
		user, ok := elem.Entities.User(dialog.UserID)
		if !ok || !peer.FromUser(user) {
			return storage.Peer{}, false
		}
	case *tg.PeerChat:
		chat, ok := elem.Entities.Chat(dialog.ChatID)
		if !ok || !peer.FromChat(chat) {
			return storage.Peer{}, false
		}
	case *tg.PeerChannel:
		channel, ok := elem.Entities.Channel(dialog.ChannelID)
		if !ok || !peer.FromChat(channel) {
			return storage.Peer{}, false
		}
	default:
		return storage.Peer{}, false
	}

	return peer, true
}
