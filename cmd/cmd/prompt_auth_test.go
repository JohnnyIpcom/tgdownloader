package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestTUIAuthCodeProviderDeliversModelResponse(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()

	provider := newTUIAuthCodeProvider(lifetime)
	result := make(chan struct {
		code string
		err  error
	}, 1)
	ctx := context.Background()
	sentCode := &tg.AuthSentCode{}

	go func() {
		code, err := provider.Code(ctx, sentCode)
		result <- struct {
			code string
			err  error
		}{code: code, err: err}
	}()

	msg := waitForAuthCodeRequest(lifetime, provider.Requests())()
	requestMsg, ok := msg.(promptAuthCodeRequestMsg)
	if !ok || requestMsg.Request.SentCode != sentCode {
		t.Fatalf("request message = %#v", msg)
	}

	if !requestMsg.Request.Respond("12345", nil) {
		t.Fatal("first response was rejected")
	}
	if requestMsg.Request.Respond("other", nil) {
		t.Fatal("second response was accepted")
	}

	select {
	case got := <-result:
		if got.code != "12345" || got.err != nil {
			t.Fatalf("Code() = %q, %v", got.code, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Code() did not receive model response")
	}
}

func TestTUIAuthCodeProviderStopsOnRequestContextCancellation(t *testing.T) {
	provider := newTUIAuthCodeProvider(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		_, err := provider.Code(ctx, &tg.AuthSentCode{})
		result <- err
	}()

	requestMsg := waitForAuthCodeRequest(context.Background(), provider.Requests())().(promptAuthCodeRequestMsg)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Code() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Code() remained blocked after context cancellation")
	}

	if requestMsg.Request.Respond("late", nil) == false {
		t.Fatal("buffered late response should not block")
	}
}

func TestTUIAuthCodeProviderStopsOnLifetimeCancellation(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	provider := newTUIAuthCodeProvider(lifetime)
	result := make(chan error, 1)

	go func() {
		_, err := provider.Code(context.Background(), &tg.AuthSentCode{})
		result <- err
	}()

	_ = waitForAuthCodeRequest(lifetime, provider.Requests())()
	cancelLifetime()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Code() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Code() remained blocked after lifetime cancellation")
	}
}
