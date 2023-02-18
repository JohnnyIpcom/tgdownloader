package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/query/hasher"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type User struct {
	ID         int64
	AccessHash int64
	Username   string
	FirstName  string
	LastName   string
}

type UserClient interface {
	GetUsers(ctx context.Context, chat Chat, query string, offset int) ([]User, error)
	GetUsersFromMessageHistory(ctx context.Context, chat Chat) (<-chan User, <-chan error)
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
				ID:         u.ID,
				AccessHash: u.AccessHash,
				Username:   u.Username,
				FirstName:  u.FirstName,
				LastName:   u.LastName,
			})

		case *tg.UserEmpty:
			continue

		default:
			return nil, fmt.Errorf("unknown user type: %T", u)
		}
	}

	return result, nil
}

// GetUsersFromMessageHistory returns chan with users from message history. Sometimes chat doesn't provide list of users.
// This method is a workaround for this problem.
func (c *client) GetUsersFromMessageHistory(ctx context.Context, chat Chat) (<-chan User, <-chan error) {
	usersChan := make(chan User)
	errChan := make(chan error)

	hasher := hasher.Hasher{}

	go func() {
		defer close(usersChan)
		defer close(errChan)

		var offsetID int
		for {
			history, err := c.client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
				Peer: &tg.InputPeerChannel{
					ChannelID:  chat.ID,
					AccessHash: chat.AccessHash,
				},
				OffsetID:   offsetID,
				OffsetDate: 0,
				AddOffset:  0,
				Limit:      100,
				MaxID:      0,
				MinID:      0,
				Hash:       hasher.Sum(),
			})
			if err != nil {
				errChan <- err
				return
			}

			messages, ok := history.AsModified()
			if !ok {
				errChan <- fmt.Errorf("unexpected response type: %T", history)
				return
			}

			for _, message := range messages.GetMessages() {
				switch message := message.(type) {
				case *tg.Message:
					offsetID = message.GetID()

					fromID, ok := message.GetFromID()
					if !ok {
						continue
					}

					switch fromID := fromID.(type) {
					case *tg.PeerUser:
						c.logger.Info("got fromID", zap.Int64("fromID", fromID.UserID))
						participant, err := c.client.API().ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{
							Channel: &tg.InputChannel{
								ChannelID:  chat.ID,
								AccessHash: chat.AccessHash,
							},

							Participant: &tg.InputPeerUser{
								UserID:     fromID.UserID,
								AccessHash: chat.AccessHash,
							},
						})

						if err != nil {
							errChan <- err
							continue
						}

						for _, user := range participant.GetUsers() {
							switch u := user.(type) {
							case *tg.User:
								hasher.Update64(uint64(u.ID))
								usersChan <- User{
									ID:         u.ID,
									AccessHash: u.AccessHash,
									Username:   u.Username,
									FirstName:  u.FirstName,
									LastName:   u.LastName,
								}

							case *tg.UserEmpty:
								continue

							default:
								errChan <- fmt.Errorf("unknown user type: %T", u)
								continue
							}
						}

					default:
						continue
					}

				case *tg.MessageEmpty:
					continue

				default:
					errChan <- fmt.Errorf("unknown message type: %T", message)
					return
				}
			}

			if len(messages.GetMessages()) < 100 {
				break
			}
		}
	}()

	return usersChan, errChan
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
