package oauth2server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"golang.org/x/oauth2"
)

func TestValidateOAuthCallbackStateSuccess(t *testing.T) {
	t.Parallel()

	cookie := &http.Cookie{Name: "oauthstate", Value: "abc"}
	values, err := validateOAuthCallbackState("state=abc&code=42", cookie)
	if err != nil {
		t.Fatalf("validateOAuthCallbackState() error = %v", err)
	}

	if got := values.Get("code"); got != "42" {
		t.Fatalf("code = %q, want 42", got)
	}
}

func TestValidateOAuthCallbackStateMissingCookie(t *testing.T) {
	t.Parallel()

	_, err := validateOAuthCallbackState("state=abc", nil)
	if err == nil {
		t.Fatal("expected missing cookie error")
	}

	if !errors.Is(err, errInvalidOAuthState) {
		t.Fatalf("expected errInvalidOAuthState, got: %v", err)
	}

	if !apperr.IsKind(err, apperr.KindAuth) {
		t.Fatalf("expected KindAuth, got: %v", err)
	}
}

func TestValidateOAuthCallbackStateMismatch(t *testing.T) {
	t.Parallel()

	cookie := &http.Cookie{Name: "oauthstate", Value: "expected"}
	_, err := validateOAuthCallbackState("state=actual", cookie)
	if err == nil {
		t.Fatal("expected mismatch error")
	}

	if !errors.Is(err, errInvalidOAuthState) {
		t.Fatalf("expected errInvalidOAuthState, got: %v", err)
	}

	if !apperr.IsKind(err, apperr.KindAuth) {
		t.Fatalf("expected KindAuth, got: %v", err)
	}
}

func TestValidateOAuthCallbackStateInvalidQueryKindConfig(t *testing.T) {
	t.Parallel()

	cookie := &http.Cookie{Name: "oauthstate", Value: "abc"}
	_, err := validateOAuthCallbackState("%zz", cookie)
	if err == nil {
		t.Fatal("expected invalid query error")
	}

	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got: %v", err)
	}
}

func TestRunOAuth2ServerUsesCallerContextAndEventWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan renderer.Event, 1)
	done := make(chan error, 1)
	go func() {
		_, err := RunOAuth2Server(ctx, renderer.NewEventWriter(renderer.NewChannelEventSink(events)), 0, oauth2.Config{})
		done <- err
	}()

	select {
	case event := <-events:
		if event.Kind != renderer.EventLine || event.Text == "" {
			t.Fatalf("authorization event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("authorization instruction was not routed through the event writer")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunOAuth2Server error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOAuth2Server did not stop after caller cancellation")
	}
}
