package cmd

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

type tuiAuthCodeProvider struct {
	lifetime context.Context
	requests chan *tuiAuthCodeRequest
}

type tuiAuthCodeRequest struct {
	SentCode *tg.AuthSentCode

	once  sync.Once
	reply chan tuiAuthCodeResponse
}

type tuiAuthCodeResponse struct {
	code string
	err  error
}

type promptAuthCodeRequestMsg struct {
	Request *tuiAuthCodeRequest
}

type promptAuthCodeRequestsClosedMsg struct{}

func newTUIAuthCodeProvider(lifetime context.Context) *tuiAuthCodeProvider {
	if lifetime == nil {
		lifetime = context.Background()
	}

	return &tuiAuthCodeProvider{
		lifetime: lifetime,
		requests: make(chan *tuiAuthCodeRequest),
	}
}

func (p *tuiAuthCodeProvider) Requests() <-chan *tuiAuthCodeRequest {
	return p.requests
}

func (p *tuiAuthCodeProvider) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	request := &tuiAuthCodeRequest{
		SentCode: sentCode,
		reply:    make(chan tuiAuthCodeResponse, 1),
	}
	select {
	case p.requests <- request:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.lifetime.Done():
		return "", p.lifetime.Err()
	}

	select {
	case response := <-request.reply:
		return response.code, response.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.lifetime.Done():
		return "", p.lifetime.Err()
	}
}

func (r *tuiAuthCodeRequest) Respond(code string, err error) bool {
	accepted := false

	r.once.Do(func() {
		accepted = true
		r.reply <- tuiAuthCodeResponse{code: code, err: err}
	})

	return accepted
}

func waitForAuthCodeRequest(lifetime context.Context, requests <-chan *tuiAuthCodeRequest) tea.Cmd {
	if requests == nil {
		return nil
	}

	if lifetime == nil {
		lifetime = context.Background()
	}

	return func() tea.Msg {
		select {
		case request := <-requests:
			return promptAuthCodeRequestMsg{Request: request}
		case <-lifetime.Done():
			return promptAuthCodeRequestsClosedMsg{}
		}
	}
}
