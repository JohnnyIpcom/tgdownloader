package telegram

import (
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/pkg/errors"
)

func getInfoFromUser(user peers.User) UserInfo {
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

func getInfoFromPeer(peer peers.Peer) PeerInfo {
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

func getFileInfoFromElem(elem messages.Elem) (FileInfo, error) {
	var from *tg.User
	fromID, ok := elem.Msg.GetFromID()
	if ok {
		switch f := fromID.(type) {
		case *tg.PeerUser:
			from = elem.Entities.Users()[f.UserID]

		default:
			return FileInfo{}, errors.Errorf("unsupported peer type %T", f)
		}
	}

	if from == nil {
		peer := elem.Msg.GetPeerID()
		switch p := peer.(type) {
		case *tg.PeerUser:
			from = elem.Entities.Users()[p.UserID]

		default:
			return FileInfo{}, errors.Errorf("unsupported peer type %T", p)
		}
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
		from: from,
		size: size,
	}, nil
}
