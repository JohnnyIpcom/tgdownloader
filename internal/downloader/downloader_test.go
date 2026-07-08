package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
)

type fakeFileService struct {
	mu         sync.Mutex
	errSeq     []error
	calls      int
	lastWrites int
	content    []byte
}

func (f *fakeFileService) GetAllFiles(ctx context.Context, peer peers.Peer, opts ...telegram.GetAllFilesOption) (<-chan telegram.File, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeFileService) GetAllFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts ...telegram.GetAllFilesOption) (<-chan telegram.File, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeFileService) GetFilesFromMessage(ctx context.Context, peer peers.Peer, msgID int, opts ...telegram.GetFileOption) ([]*telegram.File, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeFileService) GetFilesFromGroupedMessage(ctx context.Context, peer peers.Peer, msg *tg.Message) ([]*telegram.File, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeFileService) Download(ctx context.Context, file telegram.File, out io.Writer) error {
	f.mu.Lock()
	f.calls++
	call := f.calls
	var err error
	if call <= len(f.errSeq) {
		err = f.errSeq[call-1]
	}
	f.mu.Unlock()

	if err != nil {
		return err
	}

	payload := f.content
	if len(payload) == 0 {
		payload = []byte("ok")
	}

	n, writeErr := out.Write(payload)
	if writeErr != nil {
		return writeErr
	}

	f.mu.Lock()
	f.lastWrites += n
	f.mu.Unlock()

	return nil
}

func (f *fakeFileService) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFileService) DownloadFromOffset(ctx context.Context, file telegram.File, out io.Writer, offset int64) (int64, error) {
	payload := f.content
	if len(payload) == 0 {
		payload = []byte("ok")
	}

	if offset < 0 {
		return 0, errors.New("negative offset")
	}

	if offset >= int64(len(payload)) {
		return 0, nil
	}

	n, err := out.Write(payload[offset:])
	return int64(n), err
}

func makeTelegramFile(name string) telegram.File {
	f := telegram.File{}

	setUnexportedField(&f, "name", name)
	setUnexportedField(&f, "size", int64(2))
	setUnexportedField(&f, "metadata", map[string]interface{}{
		"peername": "peer",
	})

	return f
}

func setUnexportedField(target interface{}, field string, value interface{}) {
	rv := reflect.ValueOf(target).Elem().FieldByName(field)
	reflect.NewAt(rv.Type(), unsafe.Pointer(rv.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func TestDownloaderStatsDownloaded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := afero.NewMemMapFs()
	svc := &fakeFileService{}
	d := New(fs, svc, WithNumWorkers(1), WithRetry(1, time.Millisecond))
	d.SetOutputDir("/downloads")

	q := make(chan File)
	d.Start(ctx)
	d.AddDownloadQueue(ctx, q)
	q <- File{File: makeTelegramFile("a.txt")}
	close(q)

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	stats := d.Stats()
	if stats.Downloaded != 1 || stats.Skipped != 0 || stats.Failed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestDownloaderStatsSkippedWhenFileExists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/downloads", 0755)
	_ = afero.WriteFile(fs, "/downloads/a.txt", []byte("existing"), 0644)

	svc := &fakeFileService{}
	d := New(fs, svc, WithNumWorkers(1), WithRetry(1, time.Millisecond))
	d.SetOutputDir("/downloads")

	q := make(chan File)
	d.Start(ctx)
	d.AddDownloadQueue(ctx, q)
	q <- File{File: makeTelegramFile("a.txt")}
	close(q)

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	stats := d.Stats()
	if stats.Downloaded != 0 || stats.Skipped != 1 || stats.Failed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if got := svc.Calls(); got != 0 {
		t.Fatalf("expected no download calls for skipped file, got %d", got)
	}
}

func TestDownloaderRetryEventuallySucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := afero.NewMemMapFs()
	svc := &fakeFileService{errSeq: []error{errors.New("temporary")}}
	d := New(fs, svc, WithNumWorkers(1), WithRetry(2, time.Millisecond))
	d.SetOutputDir("/downloads")

	q := make(chan File)
	d.Start(ctx)
	d.AddDownloadQueue(ctx, q)
	q <- File{File: makeTelegramFile("r.txt")}
	close(q)

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	if got := svc.Calls(); got != 2 {
		t.Fatalf("expected 2 download attempts, got %d", got)
	}

	stats := d.Stats()
	if stats.Downloaded != 1 || stats.Skipped != 0 || stats.Failed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestDownloaderRetryFailsAfterLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := afero.NewMemMapFs()
	svc := &fakeFileService{errSeq: []error{errors.New("fail-1"), errors.New("fail-2")}}
	d := New(fs, svc, WithNumWorkers(1), WithRetry(2, time.Millisecond))
	d.SetOutputDir("/downloads")

	q := make(chan File)
	d.Start(ctx)
	d.AddDownloadQueue(ctx, q)
	q <- File{File: makeTelegramFile("f.txt")}
	close(q)

	err := d.Stop(ctx)
	if err == nil {
		t.Fatal("expected Stop() error on exhausted retries")
	}

	if got := svc.Calls(); got != 2 {
		t.Fatalf("expected 2 download attempts, got %d", got)
	}

	stats := d.Stats()
	if stats.Downloaded != 0 || stats.Skipped != 0 || stats.Failed != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestDownloaderResumesExistingPartialFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := afero.NewMemMapFs()

	payload := []byte("hello-resume-world")
	svc := &fakeFileService{content: payload}

	d := New(fs, svc, WithNumWorkers(1), WithRetry(2, time.Millisecond))
	d.SetOutputDir("/downloads")

	if err := fs.MkdirAll("/downloads", 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	partial := payload[:5]
	if err := afero.WriteFile(fs, "/downloads/resume.bin", partial, 0644); err != nil {
		t.Fatalf("WriteFile() partial error = %v", err)
	}

	tgFile := makeTelegramFile("resume.bin")
	setUnexportedField(&tgFile, "size", int64(len(payload)))

	q := make(chan File)
	d.Start(ctx)
	d.AddDownloadQueue(ctx, q)
	q <- File{File: tgFile}
	close(q)

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	got, err := afero.ReadFile(fs, "/downloads/resume.bin")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected resumed payload: got %q, want %q", string(got), string(payload))
	}

	if calls := svc.Calls(); calls != 0 {
		t.Fatalf("expected 0 full Download() calls when resume path is used, got %d", calls)
	}

	stats := d.Stats()
	if stats.Downloaded != 1 || stats.Skipped != 0 || stats.Failed != 0 {
		t.Fatalf("unexpected stats after resume: %+v", stats)
	}
}
