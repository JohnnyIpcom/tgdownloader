package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
)

type UserInfo struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

type UserClient interface {
	GetUsersFromMessageHistory(ctx context.Context, ID int64, Q string) (<-chan UserInfo, <-chan error)
	GetUser(ctx context.Context, userID int64) (UserInfo, error)
	GetUsersFromChat(ctx context.Context, chatID int64, query string) ([]UserInfo, error)
	GetUsersFromChannel(ctx context.Context, channelID int64, query string) ([]UserInfo, error)
	GetAllUsersFromChat(ctx context.Context, chatID int64) (<-chan UserInfo, int, error)
	GetAllUsersFromChannel(ctx context.Context, channelID int64) (<-chan UserInfo, int, error)
}

var _ UserClient = (*client)(nil)

func (c *client) getInfoFromUser(user peers.User) UserInfo {
	username := "<empty>"
	if uname, ok := user.Username(); ok {
		username = uname
	}

	firstName := "<empty>"
	if fname, ok := user.FirstName(); ok {
		firstName = fname
	}

	lastName := "<empty>"
	if lname, ok := user.LastName(); ok {
		lastName = lname
	}

	return UserInfo{
		ID:        user.ID(),
		Username:  username,
		FirstName: firstName,
		LastName:  lastName,
	}
}

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (c *client) GetUsersFromMessageHistory(ctx context.Context, ID int64, Q string) (<-chan UserInfo, <-chan error) {
	usersChan := make(chan UserInfo)
	errChan := make(chan error)

	go func() {
		defer close(usersChan)
		defer close(errChan)

		peer, err := c.getInputPeer(ctx, ID)
		if err != nil {
			errChan <- err
			return
		}

		uniqueIDs := make(map[int64]struct{})

		queryBuilder := query.Messages(c.client.API()).GetHistory(peer)
		queryBuilder = queryBuilder.BatchSize(100)

		if err = queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			users := elem.Entities.Users()
			for _, user := range users {
				if _, ok := uniqueIDs[user.GetID()]; ok {
					continue
				}

				uniqueIDs[user.GetID()] = struct{}{}

				peerUser, err := c.peerMgr.GetUser(ctx, user.AsInput())
				if err != nil {
					errChan <- fmt.Errorf("failed to get user: %w", err)
					continue
				}

				u := c.getInfoFromUser(peerUser)
				if Q != "" && !strings.Contains(u.FirstName, Q) && !strings.Contains(u.LastName, Q) && !strings.Contains(u.Username, Q) {
					continue
				}

				usersChan <- u
			}

			return nil
		}); err != nil {
			errChan <- err
			return
		}
	}()

	return usersChan, errChan
}

func (c *client) GetUser(ctx context.Context, ID int64) (UserInfo, error) {
	user, err := c.peerMgr.ResolveUserID(ctx, ID)
	if err != nil {
		return UserInfo{}, err
	}

	return c.getInfoFromUser(user), nil
}

// GetUsersFromChat returns users from chat by query.
func (c *client) GetUsersFromChat(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetUsersFromChannel returns users from channel by query.
func (c *client) GetUsersFromChannel(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	channel, err := c.peerMgr.ResolveChannelID(ctx, ID)
	if err != nil {
		return nil, err
	}

	var users []UserInfo

	channelMembers := members.ChannelQuery{Channel: channel}.Search(query)
	channelMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User()))
		return nil
	})

	return users, nil
}

// GetAllUsersFromChat returns all users from chat.
func (c *client) GetAllUsersFromChat(ctx context.Context, ID int64) (<-chan UserInfo, int, error) {
	chat, err := c.peerMgr.ResolveChatID(ctx, ID)
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
			usersChan <- c.getInfoFromUser(m.User())
			return nil
		})
	}()

	return usersChan, count, nil
}

// GetAllUsersFromChannel returns all users from channel.
func (c *client) GetAllUsersFromChannel(ctx context.Context, ID int64) (<-chan UserInfo, int, error) {
	channel, err := c.peerMgr.ResolveChannelID(ctx, ID)
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
			usersChan <- c.getInfoFromUser(m.User())
			return nil
		})
	}()

	return usersChan, count, nil
}
