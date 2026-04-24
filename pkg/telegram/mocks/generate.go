package mocks

//go:generate go run go.uber.org/mock/mockgen -source=../peer.go -destination=mock_peerservice.go -package=mocks PeerService
//go:generate go run go.uber.org/mock/mockgen -source=../client.go -destination=mock_linkedchatpeer.go -package=mocks linkedChatPeer
