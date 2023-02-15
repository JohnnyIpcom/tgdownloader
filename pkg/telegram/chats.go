package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type ChatType int

const (
	ChatTypeChat ChatType = iota
	ChatTypeChannel
	ChatTypeChatForbidden
	ChatTypeChannelForbidden
)

type Chat struct {
	Type       ChatType
	ID         int64
	AccessHash int64
	Title      string
}

type ChatClient interface {
	GetAllChats(ctx context.Context) ([]Chat, error)
	FindChat(ctx context.Context, ID int64) (Chat, error)
}

var _ ChatClient = (*client)(nil)

// GetAllChats returns all chats.
func (c *client) GetAllChats(ctx context.Context) ([]Chat, error) {
	chats, err := c.client.API().MessagesGetAllChats(ctx, []int64{})
	if err != nil {
		return nil, err
	}

	var result []Chat
	for _, chatClass := range chats.GetChats() {
		switch chat := chatClass.(type) {
		case *tg.Chat:
			result = append(result, Chat{
				Type:  ChatTypeChat,
				ID:    chat.ID,
				Title: chat.Title,
			})

		case *tg.Channel:
			result = append(result, Chat{
				Type:       ChatTypeChannel,
				ID:         chat.ID,
				AccessHash: chat.AccessHash,
				Title:      chat.Title,
			})

		case *tg.ChatForbidden:
			result = append(result, Chat{
				Type:  ChatTypeChatForbidden,
				ID:    chat.ID,
				Title: chat.Title,
			})

		case *tg.ChannelForbidden:
			result = append(result, Chat{
				Type:       ChatTypeChannelForbidden,
				ID:         chat.ID,
				AccessHash: chat.AccessHash,
				Title:      chat.Title,
			})

		default:
			c.logger.Warn("unknown chat type", zap.Any("chat", chat))
		}
	}

	return result, nil
}

// FindChat returns chat by ID.
func (c *client) FindChat(ctx context.Context, ID int64) (Chat, error) {
	chats, err := c.GetAllChats(ctx)
	if err != nil {
		return Chat{}, err
	}

	for _, chat := range chats {
		if chat.ID == ID {
			return chat, nil
		}
	}

	return Chat{}, fmt.Errorf("chat %d not found", ID)
}
