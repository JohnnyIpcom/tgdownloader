package telegram

import (
	"context"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"go.uber.org/zap"
)

// Query is a query for channel members.
type Query interface {
	channelMembers(channel peers.Channel) *members.ChannelMembers
}

type recent struct{}

func (r recent) channelMembers(channel peers.Channel) *members.ChannelMembers {
	return members.Channel(channel)
}

func QueryRecent() Query {
	return recent{}
}

type querySearch struct {
	query string
}

func (q querySearch) channelMembers(channel peers.Channel) *members.ChannelMembers {
	return members.ChannelQuery{Channel: channel}.Search(q.query)
}

func QuerySearch(query string) Query {
	return querySearch{query: query}
}

type UserService interface {
	GetUser(ctx context.Context, userID int64) (peers.User, error)

	GetUsersFromMessageHistory(ctx context.Context, peer peers.Peer) (<-chan peers.User, error)
	GetUsersFromChat(ctx context.Context, chatID int64) (<-chan peers.User, int, error)
	GetUsersFromChannel(ctx context.Context, channelID int64, query Query) (<-chan peers.User, int, error)
}

type userService service

var _ UserService = (*userService)(nil)

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (s *userService) GetUsersFromMessageHistory(ctx context.Context, peer peers.Peer) (<-chan peers.User, error) {
	inputPeer, err := s.client.GetInputPeer(ctx, peer.TDLibPeerID())
	if err != nil {
		return nil, err
	}

	usersChan := make(chan peers.User)
	go func() {
		defer close(usersChan)

		uniqueIDs := make(map[int64]struct{})

		queryBuilder := query.Messages(s.client.API()).GetHistory(inputPeer)
		queryBuilder = queryBuilder.BatchSize(100)

		if err = queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
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

// GetUsersFromChat returns all users from chat.
func (s *userService) GetUsersFromChat(ctx context.Context, ID int64) (<-chan peers.User, int, error) {
	chat, err := s.client.peerMgr.ResolveChatID(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	chatMembers := members.Chat(chat)

	count, err := chatMembers.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	usersChan := make(chan peers.User)
	go func() {
		defer close(usersChan)

		chatMembers.ForEach(ctx, func(m members.Member) error {
			usersChan <- m.User()
			return nil
		})
	}()

	return usersChan, count, nil
}

// GetUsersFromChannel returns users from channel.
func (s *userService) GetUsersFromChannel(ctx context.Context, ID int64, query Query) (<-chan peers.User, int, error) {
	channel, err := s.client.peerMgr.ResolveChannelID(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	channelMembers := query.channelMembers(channel)

	count, err := channelMembers.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	usersChan := make(chan peers.User)
	go func() {
		defer close(usersChan)

		channelMembers.ForEach(ctx, func(m members.Member) error {
			usersChan <- m.User()
			return nil
		})
	}()

	return usersChan, count, nil
}
