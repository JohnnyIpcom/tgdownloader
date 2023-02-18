package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/peers/members"
	"github.com/gotd/td/tg"
)

type UserInfo struct {
	ID          int64
	Username    string
	VisibleName string
}

type UserClient interface {
	GetUsersFromMessageHistory(ctx context.Context, ID int64) (<-chan UserInfo, <-chan error)
	GetUsersFromChat(ctx context.Context, ID int64, query string) ([]UserInfo, error)
	GetUsersFromChannel(ctx context.Context, ID int64, query string) ([]UserInfo, error)
	GetAllUsersFromChat(ctx context.Context, ID int64) ([]UserInfo, int, error)
	GetAllUsersFromChannel(ctx context.Context, ID int64) ([]UserInfo, int, error)
}

var _ UserClient = (*client)(nil)

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (c *client) GetUsersFromMessageHistory(ctx context.Context, ID int64) (<-chan UserInfo, <-chan error) {
	usersChan := make(chan UserInfo)
	errChan := make(chan error)

	go func() {
		defer close(usersChan)
		defer close(errChan)
	}()

	return usersChan, errChan
}

func (c *client) getInfoFromUser(user peers.User) UserInfo {
	username := "<empty>"
	if uname, ok := user.Username(); ok {
		username = uname
	}

	return UserInfo{
		ID:          user.ID(),
		Username:    username,
		VisibleName: user.VisibleName(),
	}
}

// GetUsersFromChat returns users from chat by query.
func (c *client) GetUsersFromChat(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetUsersFromChannel returns users from channel by query.
func (c *client) GetUsersFromChannel(ctx context.Context, ID int64, query string) ([]UserInfo, error) {
	channel, err := c.peerMgr.GetChannel(ctx, &tg.InputChannel{
		ChannelID: ID,
	})

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
func (c *client) GetAllUsersFromChat(ctx context.Context, ID int64) ([]UserInfo, int, error) {
	chat, err := c.peerMgr.GetChat(ctx, ID)
	if err != nil {
		return nil, 0, err
	}

	var users []UserInfo

	chatMembers := members.Chat(chat)
	chatMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User()))
		return nil
	})

	return users, len(users), nil
}

// GetAllUsersFromChannel returns all users from channel.
func (c *client) GetAllUsersFromChannel(ctx context.Context, ID int64) ([]UserInfo, int, error) {
	channel, err := c.peerMgr.GetChannel(ctx, &tg.InputChannel{
		ChannelID: ID,
	})

	if err != nil {
		return nil, 0, err
	}

	var users []UserInfo

	channelMembers := members.Channel(channel)
	channelMembers.ForEach(ctx, func(m members.Member) error {
		users = append(users, c.getInfoFromUser(m.User()))
		return nil
	})

	return users, len(users), nil
}
