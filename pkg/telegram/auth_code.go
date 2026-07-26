package telegram

import (
	"context"
	"errors"

	"github.com/gotd/td/tg"
)

var ErrCodeProviderUnavailable = errors.New("telegram authentication code provider is unavailable")

type CodeProvider interface {
	Code(context.Context, *tg.AuthSentCode) (string, error)
}

type ClientOption func(*clientOptions)

type clientOptions struct {
	codeProvider CodeProvider
}

func WithCodeProvider(provider CodeProvider) ClientOption {
	return func(options *clientOptions) {
		if provider != nil {
			options.codeProvider = provider
		}
	}
}

type unavailableCodeProvider struct{}

func (unavailableCodeProvider) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return "", ErrCodeProviderUnavailable
}
