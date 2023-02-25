package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
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

func (c *client) getChat(ctx context.Context, ID int64) (peers.Chat, error) {
	chat, err := c.peerMgr.GetChat(ctx, ID)
	if err != nil {
		return peers.Chat{}, err
	}

	return chat, nil
}

func (c *client) getChannel(ctx context.Context, ID int64) (peers.Channel, error) {
	channel, err := c.peerMgr.GetChannel(ctx, &tg.InputChannel{
		ChannelID: ID,
	})

	if err != nil {
		return peers.Channel{}, err
	}

	return channel, nil
}

func (c *client) getInputPeer(ctx context.Context, ID int64) (tg.InputPeerClass, error) {
	info, err := c.FindChat(ctx, ID)
	if err != nil {
		return nil, err
	}

	var inputPeer tg.InputPeerClass
	switch info.Type {
	case ChatTypeChat:
		chat, err := c.getChat(ctx, info.ID)
		if err != nil {
			return nil, err
		}

		inputPeer = chat.InputPeer()

	case ChatTypeChannel:
		channel, err := c.getChannel(ctx, info.ID)
		if err != nil {
			return nil, err
		}

		inputPeer = channel.InputPeer()

	default:
		return nil, fmt.Errorf("unknown chat type %d", info.Type)
	}

	return inputPeer, nil
}
