package yadisk

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLiveDownloadStartsAndReturnsBytes(t *testing.T) {
	if os.Getenv("RUN_YADISK_LIVE_TEST") != "1" {
		t.Skip("set RUN_YADISK_LIVE_TEST=1 to run live Yandex Disk smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client := NewClient(&http.Client{Timeout: 30 * time.Second})

	resource, err := client.ResolvePublicResourceDownloads(ctx, "https://disk.yandex.ru/d/EfDJobc8IImwMA")
	if err != nil {
		t.Fatalf("ResolvePublicResourceDownloads() error = %v", err)
	}
	if len(resource.Files) == 0 {
		t.Fatal("expected at least one file in resolved resource")
	}

	file, err := client.OpenDirectFile(ctx, resource.Files[0])
	if err != nil {
		t.Fatalf("OpenDirectFile() error = %v", err)
	}
	defer file.Body.Close()

	buf := make([]byte, 256)
	n, err := file.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read first bytes error = %v", err)
	}
	if n <= 0 {
		t.Fatal("expected to read at least one byte from live download")
	}
}
