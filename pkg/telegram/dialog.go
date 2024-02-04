package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type Dialog struct {
	peers.Peer
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

		queryBuilder.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			peer, err := s.client.peerMgr.FromInputPeer(ctx, elem.Peer)
			if err != nil {
				dialogsChan <- Dialog{err: err}
				return nil
			}

			switch dlg := elem.Dialog.GetPeer().(type) {
			case *tg.PeerUser:
				user, ok := elem.Entities.User(dlg.GetUserID())
				if !ok {
					dialogsChan <- Dialog{err: fmt.Errorf("user not found: %d", dlg.GetUserID())}
					return nil
				}

				s.client.CacheService.CacheUser(ctx, user)

			case *tg.PeerChat:
				chat, ok := elem.Entities.Chat(dlg.GetChatID())
				if !ok {
					dialogsChan <- Dialog{err: fmt.Errorf("chat not found: %d", dlg.GetChatID())}
					return nil
				}

				s.client.CacheService.CacheChat(ctx, chat)

			case *tg.PeerChannel:
				channel, ok := elem.Entities.Channel(dlg.GetChannelID())
				if !ok {
					dialogsChan <- Dialog{err: fmt.Errorf("channel not found: %d", dlg.GetChannelID())}
					return nil
				}

				s.client.CacheService.CacheChat(ctx, channel)
			}

			dialogsChan <- Dialog{Peer: peer}
			return nil
		})
	}()

	return dialogsChan, count, err
}
