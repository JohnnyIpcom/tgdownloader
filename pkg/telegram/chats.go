package telegram

import (
	"context"
	"fmt"
)

type ChatType int

const (
	ChatTypeChat ChatType = iota
	ChatTypeChannel
)

type ChatInfo struct {
	Type  ChatType
	ID    int64
	Title string
}

type ChatClient interface {
	GetAllChats(ctx context.Context) ([]ChatInfo, error)
	FindChat(ctx context.Context, ID int64) (ChatInfo, error)
}

var _ ChatClient = (*client)(nil)

// GetAllChats returns all chats.
func (c *client) GetAllChats(ctx context.Context) ([]ChatInfo, error) {
	chats, err := c.peerMgr.GetAllChats(ctx)
	if err != nil {
		return nil, err
	}

	var result []ChatInfo
	for _, chat := range chats.Chats {
		result = append(result, ChatInfo{
			Type:  ChatTypeChat,
			ID:    chat.ID(),
			Title: chat.VisibleName(),
		})
	}

	for _, channel := range chats.Channels {
		result = append(result, ChatInfo{
			Type:  ChatTypeChannel,
			ID:    channel.ID(),
			Title: channel.VisibleName(),
		})
	}

	return result, nil
}

// FindChat returns chat by ID.
func (c *client) FindChat(ctx context.Context, ID int64) (ChatInfo, error) {
	chats, err := c.GetAllChats(ctx)
	if err != nil {
		return ChatInfo{}, err
	}

	for _, chat := range chats {
		if chat.ID == ID {
			return chat, nil
		}
	}

	return ChatInfo{}, fmt.Errorf("chat %d not found", ID)
}
