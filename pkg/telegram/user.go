package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"go.uber.org/zap"
)

type UserInfo struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

type UserService interface {
	GetUsersFromMessageHistory(ctx context.Context, peer PeerInfo, Q string) (<-chan UserInfo, error)
	GetUser(ctx context.Context, userID int64) (UserInfo, error)
	GetUsersFromChat(ctx context.Context, chatID int64, query string) ([]UserInfo, error)
	GetUsersFromChannel(ctx context.Context, channelID int64, query string) ([]UserInfo, error)
	GetAllUsersFromChat(ctx context.Context, chatID int64) (<-chan UserInfo, int, error)
	GetAllUsersFromChannel(ctx context.Context, channelID int64) (<-chan UserInfo, int, error)
}

type userService service

var _ UserService = (*userService)(nil)

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (s *userService) GetUsersFromMessageHistory(ctx context.Context, peer PeerInfo, Q string) (<-chan UserInfo, error) {
	inputPeer, err := s.client.getInputPeer(ctx, peer.TDLibPeerID())
	if err != nil {
		return nil, err
	}

	usersChan := make(chan UserInfo)
	go func() {
		defer close(usersChan)

		logger := ctxlogger.FromContext(ctx)

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
					logger.Error("failed to get peer user", zap.Error(err))
					continue
				}

				u := getUserInfoFromUser(peerUser)
				if Q != "" && !strings.Contains(u.FirstName, Q) && !strings.Contains(u.LastName, Q) && !strings.Contains(u.Username, Q) {
					continue
				}

				usersChan <- u
			}

			return nil
		}); err != nil {
			logger.Error("failed to get users from message history", zap.Error(err))
			return
		}
	}()

	return usersChan, nil
}

func (s *userService) GetUser(ctx context.Context, ID int64) (UserInfo, error) {
	user, err := s.client.peerMgr.ResolveUserID(ctx, ID)
	if err != nil {
		return UserInfo{}, err
	}

	return getUserInfoFromUser(user), nil
}

// GetUsersFromChat returns users from chat by query.
func (c *userService) GetUsersFromChat(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetUsersFromChannel returns users from channel by query.
func (s *userService) GetUsersFromChannel(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	channel, err := s.client.peerMgr.ResolveChannelID(ctx, ID)
	if err != nil {
		return nil, err
	}

	var users []UserInfo

	channelMembers := members.ChannelQuery{Channel: channel}.Search(query)
	channelMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, getUserInfoFromUser(m.User()))
		return nil
	})

	return users, nil
}

// GetAllUsersFromChat returns all users from chat.
func (s *userService) GetAllUsersFromChat(ctx context.Context, ID int64) (<-chan UserInfo, int, error) {
	chat, err := s.client.peerMgr.ResolveChatID(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	chatMembers := members.Chat(chat)

	count, err := chatMembers.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	usersChan := make(chan UserInfo)
	go func() {
		defer close(usersChan)

		chatMembers.ForEach(ctx, func(m members.Member) error {
			usersChan <- getUserInfoFromUser(m.User())
			return nil
		})
	}()

	return usersChan, count, nil
}

// GetAllUsersFromChannel returns all users from channel.
func (s *userService) GetAllUsersFromChannel(ctx context.Context, ID int64) (<-chan UserInfo, int, error) {
	channel, err := s.client.peerMgr.ResolveChannelID(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	channelMembers := members.Channel(channel)

	count, err := channelMembers.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	usersChan := make(chan UserInfo)
	go func() {
		defer close(usersChan)

		channelMembers.ForEach(ctx, func(m members.Member) error {
			usersChan <- getUserInfoFromUser(m.User())
			return nil
		})
	}()

	return usersChan, count, nil
}
