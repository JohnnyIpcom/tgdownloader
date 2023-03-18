package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/gotd-contrib/storage"
)

func getUserInfoFromUser(user peers.User) UserInfo {
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

func getPeerInfoFromPeer(peer peers.Peer) PeerInfo {
	var peerType PeerType
	switch peer.(type) {
	case peers.User:
		peerType = PeerTypeUser
	case peers.Chat:
		peerType = PeerTypeChat
	case peers.Channel:
		peerType = PeerTypeChannel
	}

	return PeerInfo{
		Type: peerType,
		ID:   peer.ID(),
		Name: peer.VisibleName(),
	}
}

func extractPeer(ctx context.Context, mgr *peers.Manager, ent peer.Entities, peerID tg.PeerClass) (peers.Peer, error) {
	peer, err := ent.ExtractPeer(peerID)
	if err != nil {
		return nil, fmt.Errorf("extract peer: %w", err)
	}

	return mgr.FromInputPeer(ctx, peer)
}

func getFileInfoFromElem(ctx context.Context, mgr *peers.Manager, elem messages.Elem) (FileInfo, error) {
	var peer peers.Peer
	fromID, ok := elem.Msg.GetFromID()
	if ok {
		from, err := extractPeer(ctx, mgr, elem.Entities, fromID)
		if err != nil {
			return FileInfo{}, fmt.Errorf("extract fromID: %w", err)
		}

		peer = from
	}

	if peer == nil {
		p, err := extractPeer(ctx, mgr, elem.Entities, elem.Msg.GetPeerID())
		if err != nil {
			return FileInfo{}, fmt.Errorf("extract peerID: %w", err)
		}

		peer = p
	}

	file, ok := elem.File()
	if !ok {
		return FileInfo{}, errNoFilesInMessage
	}

	var size int64
	if doc, ok := elem.Document(); ok {
		size = doc.Size
	}

	return FileInfo{
		file: file,
		peer: peer,
		size: size,
	}, nil
}

func getPeerCacheInfoFromStoragePeer(p storage.Peer) PeerCacheInfo {
	var peer PeerInfo

	switch p.Key.Kind {
	case dialogs.User:
		peer.ID = p.User.GetID()
		peer.Name = p.User.Username
		peer.Type = PeerTypeUser

	case dialogs.Chat:
		peer.ID = p.Chat.GetID()
		peer.Name = p.Chat.GetTitle()
		peer.Type = PeerTypeChat

	case dialogs.Channel:
		peer.ID = p.Channel.GetID()
		peer.Name = p.Channel.GetTitle()
		peer.Type = PeerTypeChannel
	}

	return PeerCacheInfo{
		ID:         p.Key.ID,
		AccessHash: p.Key.AccessHash,
		Peer:       peer,
		CreatedAt:  time.Unix(p.CreatedAt, 0),
	}
}
