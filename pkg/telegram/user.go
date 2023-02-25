package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

type UserInfo struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

type UserClient interface {
	GetUsersFromMessageHistory(ctx context.Context, ID int64, Q string) (<-chan UserInfo, <-chan error)
	GetUsersFromChat(ctx context.Context, ID int64, Q string) ([]UserInfo, error)
	GetUsersFromChannel(ctx context.Context, ID int64, Q string) ([]UserInfo, error)
	GetAllUsersFromChat(ctx context.Context, ID int64) ([]UserInfo, int, error)
	GetAllUsersFromChannel(ctx context.Context, ID int64) ([]UserInfo, int, error)
}

var _ UserClient = (*client)(nil)

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

				u := c.getInfoFromUser(user)
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

func (c *client) getInfoFromUser(user *tg.User) UserInfo {
	username := "<empty>"
	if uname, ok := user.GetUsername(); ok {
		username = uname
	}

	firstName := "<empty>"
	if fname, ok := user.GetFirstName(); ok {
		firstName = fname
	}

	lastName := "<empty>"
	if lname, ok := user.GetLastName(); ok {
		lastName = lname
	}

	return UserInfo{
		ID:        user.GetID(),
		Username:  username,
		FirstName: firstName,
		LastName:  lastName,
	}
}

// GetUsersFromChat returns users from chat by query.
func (c *client) GetUsersFromChat(ctx context.Context, ID int64, Q string) ([]UserInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetUsersFromChannel returns users from channel by query.
func (c *client) GetUsersFromChannel(ctx context.Context, ID int64, Q string) ([]UserInfo, error) {
	channel, err := c.getChannel(ctx, ID)
	if err != nil {
		return nil, err
	}

	var users []UserInfo

	channelMembers := members.ChannelQuery{Channel: channel}.Search(Q)
	channelMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User().Raw()))
		return nil
	})

	return users, nil
}

// GetAllUsersFromChat returns all users from chat.
func (c *client) GetAllUsersFromChat(ctx context.Context, ID int64) ([]UserInfo, int, error) {
	chat, err := c.getChat(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	var users []UserInfo

	chatMembers := members.Chat(chat)
	chatMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User().Raw()))
		return nil
	})

	return users, len(users), nil
}

// GetAllUsersFromChannel returns all users from channel.
func (c *client) GetAllUsersFromChannel(ctx context.Context, ID int64) ([]UserInfo, int, error) {
	channel, err := c.getChannel(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	var users []UserInfo

	channelMembers := members.Channel(channel)
	channelMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User().Raw()))
		return nil
	})

	return users, len(users), nil
}
