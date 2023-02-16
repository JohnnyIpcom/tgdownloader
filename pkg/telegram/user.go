package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type User struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

type UserClient interface {
	GetUsers(ctx context.Context, chat Chat, query string, offset int) ([]User, error)
	GetAllUsers(ctx context.Context, chat Chat) ([]User, error)
}

var _ UserClient = (*client)(nil)

// GetUsers returns all users in a chat. This method has a limit of 100 users.
// If there are more than 100 users in a chat, you need to use the offset parameter to get the next 100 users.
func (c *client) GetUsers(ctx context.Context, chat Chat, query string, offset int) ([]User, error) {
	c.logger.Info("getting users", zap.String("chat", chat.Title), zap.String("query", query), zap.Int("offset", offset))

	participants, err := c.client.API().ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
		Channel: &tg.InputChannel{
			ChannelID:  chat.ID,
			AccessHash: chat.AccessHash,
		},
		Filter: &tg.ChannelParticipantsSearch{
			Q: query,
		},
		Offset: offset,
		Limit:  100,
	})

	if err != nil {
		return nil, err
	}

	users, ok := participants.AsModified()
	if !ok {
		return nil, fmt.Errorf("users is not modified")
	}

	var result []User
	for _, user := range users.GetUsers() {
		switch u := user.(type) {
		case *tg.User:
			result = append(result, User{
				ID:        u.ID,
				Username:  u.Username,
				FirstName: u.FirstName,
				LastName:  u.LastName,
			})

		case *tg.UserEmpty:
			continue

		default:
			return nil, fmt.Errorf("unknown user type: %T", u)
		}
	}

	return result, nil
}

// GetAllUsers returns all users in a chat.
func (c *client) GetAllUsers(ctx context.Context, chat Chat) ([]User, error) {
	var result []User

	for offset := 0; ; offset += 100 {
		users, err := c.GetUsers(ctx, chat, "", offset)
		if err != nil {
			return nil, err
		}

		result = append(result, users...)
		if len(users) < 100 {
			break
		}
	}

	return result, nil
}
