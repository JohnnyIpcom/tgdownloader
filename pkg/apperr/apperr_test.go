package apperr

import (
	"errors"
	"testing"
)

func TestNewSetsKindAndWrapsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("broken")
	err := New("downloader.download", KindNetwork, cause)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	if !IsKind(err, KindNetwork) {
		t.Fatalf("expected KindNetwork, got %v", err)
	}

	if !errors.Is(err, cause) {
		t.Fatal("expected wrapped cause to be discoverable via errors.Is")
	}
}

func TestWrapPreservesExistingKind(t *testing.T) {
	t.Parallel()

	inner := New("telegram.network.resolver", KindConfig, errors.New("invalid resolver"))
	wrapped := Wrap("cmd.download", inner)

	var appErr *Error
	if !errors.As(wrapped, &appErr) {
		t.Fatal("expected wrapped error to be *Error")
	}

	if appErr.Op != "cmd.download" {
		t.Fatalf("unexpected operation: got %q", appErr.Op)
	}

	if appErr.Kind != KindConfig {
		t.Fatalf("unexpected kind: got %q want %q", appErr.Kind, KindConfig)
	}
}

func TestWrapAssignsUnknownKindForPlainErrors(t *testing.T) {
	t.Parallel()

	wrapped := Wrap("root.execute", errors.New("unexpected"))

	var appErr *Error
	if !errors.As(wrapped, &appErr) {
		t.Fatal("expected wrapped error to be *Error")
	}

	if appErr.Kind != KindUnknown {
		t.Fatalf("unexpected kind: got %q want %q", appErr.Kind, KindUnknown)
	}
}
