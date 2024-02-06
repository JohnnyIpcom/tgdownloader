package telegram

import "errors"

var (
	errNoFilesInMessage = errors.New("no files in message")
	errLimitReached     = errors.New("limit reached")
)
