package telegram

import "errors"

var (
	errNoFilesInMessage = errors.New("no files in message")
	errPaidMediaLocked  = errors.New("paid media is locked (not purchased)")
	errLimitReached     = errors.New("limit reached")
)
