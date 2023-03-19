package telegram

import "errors"

var (
	errNoFilesInMessage = errors.New("no files in message")

	errPeerStoreNotSet = errors.New("peer store is not set")

	ErrorLimitReached = errors.New("limit reached")
)
