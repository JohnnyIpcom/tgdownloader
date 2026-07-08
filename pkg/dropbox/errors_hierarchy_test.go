package dropbox

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

func TestFsOpenFileUnsupportedFlagsAreTyped(t *testing.T) {
	t.Parallel()

	fs := &Fs{}

	_, err := fs.OpenFile("x", os.O_APPEND, 0)
	if err == nil {
		t.Fatal("expected unsupported append error")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got %v", err)
	}

	_, err = fs.OpenFile("x", os.O_RDWR, 0)
	if err == nil {
		t.Fatal("expected unsupported read/write error")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got %v", err)
	}
}

func TestFsUnsupportedOpsAreTyped(t *testing.T) {
	t.Parallel()

	fs := &Fs{}

	err := fs.Chmod("x", 0)
	if !errors.Is(err, ErrNotSupported) || !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("unexpected chmod error: %v", err)
	}

	err = fs.Chown("x", 0, 0)
	if !errors.Is(err, ErrNotSupported) || !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("unexpected chown error: %v", err)
	}

	err = fs.Chtimes("x", time.Now(), time.Now())
	if !errors.Is(err, ErrNotSupported) || !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("unexpected chtimes error: %v", err)
	}
}

func TestFileUnsupportedOpsAreTyped(t *testing.T) {
	t.Parallel()

	f := &File{streamWrite: nopWriteCloser{}}

	_, err := f.Seek(0, io.SeekStart)
	if err == nil {
		t.Fatal("expected seek unsupported error")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got %v", err)
	}

	err = f.Truncate(10)
	if err == nil {
		t.Fatal("expected truncate unsupported error")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got %v", err)
	}
}
