package oauth2server

import (
	"errors"
	"net/http"
	"testing"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
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
