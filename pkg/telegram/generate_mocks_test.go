package telegram

//go:generate go run go.uber.org/mock/mockgen -source=peer.go -destination=mock_peerservice_test.go -package=telegram PeerService
