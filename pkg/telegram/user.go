package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"go.uber.org/zap"
)

// Query is a query for channel members.
type Query interface {
	Members(ctx context.Context, peer peers.Peer) (members.Members, error)
}

type recent struct{}

func (r recent) Members(ctx context.Context, peer peers.Peer) (members.Members, error) {
	switch {
	case peer.TDLibPeerID().IsChannel():
		channel, err := peer.Manager().ResolveChannelID(ctx, peer.ID())
		if err != nil {
			return nil, err
		}

		return members.Channel(channel), nil

	case peer.TDLibPeerID().IsChat():
		chat, err := peer.Manager().ResolveChatID(ctx, peer.ID())
		if err != nil {
			return nil, err
		}

		return members.Chat(chat), nil

	default:
		return nil, fmt.Errorf("unsupported peer type")
	}
}

func QueryRecent() Query {
	return recent{}
}

type querySearch struct {
	query string
}

func (q querySearch) Members(ctx context.Context, peer peers.Peer) (members.Members, error) {
	switch {
	case peer.TDLibPeerID().IsChannel():
		channel, err := peer.Manager().ResolveChannelID(ctx, peer.ID())
		if err != nil {
			return nil, err
		}

		return members.ChannelQuery{Channel: channel}.Search(q.query), nil

	case peer.TDLibPeerID().IsChat():
		chat, err := peer.Manager().ResolveChatID(ctx, peer.ID())
		if err != nil {
			return nil, err
		}

		return members.Chat(chat), nil

	default:
		return nil, fmt.Errorf("unsupported peer type")
	}
}

func QuerySearch(query string) Query {
	return querySearch{query: query}
}

type UserService interface {
	GetSelf(ctx context.Context) (peers.User, error)
	GetUser(ctx context.Context, userID int64) (peers.User, error)

	GetUsersFromMessageHistory(ctx context.Context, peer peers.Peer) (<-chan peers.User, error)
	GetUsers(ctx context.Context, peer peers.Peer, query Query) (<-chan peers.User, int, error)
}

type userService service

var _ UserService = (*userService)(nil)

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (s *userService) GetUsersFromMessageHistory(ctx context.Context, peer peers.Peer) (<-chan peers.User, error) {
	usersChan := make(chan peers.User)
	go func() {
		defer close(usersChan)

		uniqueIDs := make(map[int64]struct{})

		queryBuilder := query.Messages(s.client.API()).GetHistory(peer.InputPeer())
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			users := elem.Entities.Users()
			for _, user := range users {
				if _, ok := uniqueIDs[user.GetID()]; ok {
					continue
				}

				uniqueIDs[user.GetID()] = struct{}{}

				peerUser, err := s.client.peerMgr.GetUser(ctx, user.AsInput())
				if err != nil {
					s.logger.Error("failed to get peer user", zap.Error(err))
					continue
				}

				usersChan <- peerUser
			}

			return nil
		}); err != nil {
			s.logger.Error("failed to get users from message history", zap.Error(err))
			return
		}
	}()

	return usersChan, nil
}

func (s *userService) GetUser(ctx context.Context, ID int64) (peers.User, error) {
	user, err := s.client.peerMgr.ResolveUserID(ctx, ID)
	if err != nil {
		return peers.User{}, err
	}

	return user, nil
}

func (s *userService) GetSelf(ctx context.Context) (peers.User, error) {
	self, err := s.client.peerMgr.Self(ctx)
	if err != nil {
		return peers.User{}, err
	}

	return self, nil
}

// GetUsers returns users from channel.
func (s *userService) GetUsers(ctx context.Context, peer peers.Peer, query Query) (<-chan peers.User, int, error) {
	m, err := query.Members(ctx, peer)
	if err != nil {
		return nil, 0, err
	}

	count, err := m.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	usersChan := make(chan peers.User)
	go func() {
		defer close(usersChan)

		m.ForEach(ctx, func(m members.Member) error {
			usersChan <- m.User()
			return nil
		})
	}()

	return usersChan, count, nil
}
