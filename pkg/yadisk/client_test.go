package yadisk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestResolvePublicDownloadURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "public_key=") {
			t.Fatalf("expected public_key query")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"href":"https://download.example/file.bin"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	directURL, err := client.ResolvePublicDownloadURL(context.Background(), "https://disk.yandex.ru/d/abc")
	if err != nil {
		t.Fatalf("ResolvePublicDownloadURL() error = %v", err)
	}

	if directURL != "https://download.example/file.bin" {
		t.Fatalf("unexpected direct URL: %q", directURL)
	}
}

func TestResolvePublicDownloadURLErrors(t *testing.T) {
	t.Parallel()

	t.Run("Non200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()

		client := NewClient(server.Client())
		client.apiBaseURL = server.URL

		if _, err := client.ResolvePublicDownloadURL(context.Background(), "x"); err == nil {
			t.Fatal("expected error for non-200 status")
		}
	})

	t.Run("EmptyHref", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/disk/public/resources/download":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"href":""}`))
			case "/v1/disk/public/resources":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"type":"file","name":"x.bin","file":""}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := NewClient(server.Client())
		client.apiBaseURL = server.URL

		if _, err := client.ResolvePublicDownloadURL(context.Background(), "x"); err == nil {
			t.Fatal("expected error for empty href")
		}
	})

	t.Run("EmptyHrefFallbackToMetaFile", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/disk/public/resources/download":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"href":""}`))
			case "/v1/disk/public/resources":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"type":"file","name":"x.bin","file":"https://download.example/fallback.bin"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := NewClient(server.Client())
		client.apiBaseURL = server.URL

		href, err := client.ResolvePublicDownloadURL(context.Background(), "x")
		if err != nil {
			t.Fatalf("expected fallback to metadata file, got error: %v", err)
		}
		if href != "https://download.example/fallback.bin" {
			t.Fatalf("unexpected href from metadata fallback: %q", href)
		}
	})

	t.Run("DirectoryResourceError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/disk/public/resources/download":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"href":""}`))
			case "/v1/disk/public/resources":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"type":"dir","name":"Shared Folder"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := NewClient(server.Client())
		client.apiBaseURL = server.URL

		_, err := client.ResolvePublicDownloadURL(context.Background(), "x")
		if err == nil {
			t.Fatal("expected directory error")
		}
		if !strings.Contains(err.Error(), "directory") {
			t.Fatalf("expected directory hint in error, got: %v", err)
		}
	})
}

func TestOpenPublicFile(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/disk/public/resources":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"test.bin","path":"/disk/test.bin"}`))
		case "/v1/disk/public/resources/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":"` + server.URL + `/file/test.bin"}`))
		case "/file/test.bin":
			w.Header().Set("Content-Disposition", `attachment; filename="report.zip"`)
			_, _ = w.Write([]byte("payload"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	file, err := client.OpenPublicFile(context.Background(), "https://disk.yandex.ru/d/abc")
	if err != nil {
		t.Fatalf("OpenPublicFile() error = %v", err)
	}
	defer file.Body.Close()

	if file.Name != "report.zip" {
		t.Fatalf("expected filename report.zip, got %q", file.Name)
	}

	body, err := io.ReadAll(file.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("unexpected payload: %q", string(body))
	}
}

func TestOpenDirectFileRange(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.bin" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "bytes=5-" {
			w.Header().Set("Content-Disposition", `attachment; filename="file.bin"`)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("fghij"))
			return
		}

		w.Header().Set("Content-Disposition", `attachment; filename="file.bin"`)
		_, _ = w.Write([]byte("abcdefghij"))
	}))
	defer server.Close()

	client := NewClient(server.Client())

	file, err := client.OpenDirectFileRange(context.Background(), PublicDownload{
		Name:      "file.bin",
		Size:      10,
		DirectURL: server.URL + "/file.bin",
	}, 5)
	if err != nil {
		t.Fatalf("OpenDirectFileRange() error = %v", err)
	}
	defer file.Body.Close()

	body, err := io.ReadAll(file.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "fghij" {
		t.Fatalf("unexpected ranged payload: %q", string(body))
	}
}

func TestOpenDirectFileRangeFallsBackToFullBodyWhenRangeIgnored(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.bin" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Header.Get("Range") == "bytes=5-" {
			// Simulate backend that ignores Range and returns full content.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("abcdefghij"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("abcdefghij"))
	}))
	defer server.Close()

	client := NewClient(server.Client())

	file, err := client.OpenDirectFileRange(context.Background(), PublicDownload{
		Name:      "file.bin",
		Size:      10,
		DirectURL: server.URL + "/file.bin",
	}, 5)
	if err != nil {
		t.Fatalf("OpenDirectFileRange() error = %v", err)
	}
	defer file.Body.Close()

	if file.Offset != 0 {
		t.Fatalf("expected offset 0 when range is ignored, got %d", file.Offset)
	}

	body, err := io.ReadAll(file.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "abcdefghij" {
		t.Fatalf("unexpected payload when range ignored: %q", string(body))
	}
}

func TestResolvePublicResourceDownloadsDirectory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/disk/public/resources/download" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
			return
		}

		if r.URL.Path != "/v1/disk/public/resources" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pathParam := r.URL.Query().Get("path")
		switch pathParam {
		case "":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","name":"Root Folder","path":"/disk/root"}`))
		case "/disk/root":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","path":"/disk/root","_embedded":{"items":[{"type":"file","name":"a.jpg","path":"/disk/root/a.jpg","file":"https://download.example/a.jpg"},{"type":"dir","name":"nested","path":"/disk/root/nested"}],"total":2}}`))
		case "/disk/root/nested":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","path":"/disk/root/nested","_embedded":{"items":[{"type":"file","name":"b.jpg","path":"/disk/root/nested/b.jpg","file":"https://download.example/b.jpg"}],"total":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	resource, err := client.ResolvePublicResourceDownloads(context.Background(), "https://disk.yandex.ru/d/abc")
	if err != nil {
		t.Fatalf("ResolvePublicResourceDownloads() error = %v", err)
	}

	if resource.Type != "dir" {
		t.Fatalf("expected dir resource type, got %q", resource.Type)
	}
	if len(resource.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resource.Files))
	}

	if resource.Files[0].Name != "a.jpg" || resource.Files[0].RelativeDir != "" {
		t.Fatalf("unexpected first file: %+v", resource.Files[0])
	}
	if resource.Files[0].DirectURL != "" {
		t.Fatalf("expected empty direct url for lazy resolution, got: %q", resource.Files[0].DirectURL)
	}
	if resource.Files[1].Name != "b.jpg" || resource.Files[1].RelativeDir != "nested" {
		t.Fatalf("unexpected second file: %+v", resource.Files[1])
	}
	if resource.Files[1].DirectURL != "" {
		t.Fatalf("expected empty direct url for lazy resolution, got: %q", resource.Files[1].DirectURL)
	}
}

func TestResolvePublicResourceDownloadsDirectoryFileMetaFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/disk/public/resources/download" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
			return
		}

		if r.URL.Path != "/v1/disk/public/resources" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pathParam := r.URL.Query().Get("path")
		switch pathParam {
		case "":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","name":"Root","path":"/","_embedded":{"items":[{"type":"file","name":"x.jpg","path":"/x.jpg"}],"total":1}}`))
		case "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","name":"Root","path":"/","_embedded":{"items":[{"type":"file","name":"x.jpg","path":"/x.jpg"}],"total":1}}`))
		case "/x.jpg":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"x.jpg","path":"/x.jpg","file":"https://download.example/x.jpg"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	resource, err := client.ResolvePublicResourceDownloads(context.Background(), "https://disk.yandex.ru/d/test")
	if err != nil {
		t.Fatalf("ResolvePublicResourceDownloads() error = %v", err)
	}

	if len(resource.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resource.Files))
	}
	if resource.Files[0].DirectURL != "" {
		t.Fatalf("expected empty direct url before lazy resolution, got: %q", resource.Files[0].DirectURL)
	}

	directURL, err := client.ResolvePublicFileDownloadURL(context.Background(), "https://disk.yandex.ru/d/test", resource.Files[0])
	if err != nil {
		t.Fatalf("ResolvePublicFileDownloadURL() error = %v", err)
	}
	if directURL != "https://download.example/x.jpg" {
		t.Fatalf("unexpected direct url after lazy resolution: %q", directURL)
	}
}

func TestResolvePublicResourceDownloadsDirectorySizesFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/disk/public/resources/download" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
			return
		}

		if r.URL.Path != "/v1/disk/public/resources" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pathParam := r.URL.Query().Get("path")
		switch pathParam {
		case "", "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","name":"Root","path":"/","_embedded":{"items":[{"type":"file","name":"x.jpg","path":"/x.jpg"}],"total":1}}`))
		case "/x.jpg":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"x.jpg","path":"/x.jpg","sizes":[{"name":"m","url":"https://download.example/m.jpg"},{"name":"orig","url":"https://download.example/orig.jpg"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	resource, err := client.ResolvePublicResourceDownloads(context.Background(), "https://disk.yandex.ru/d/test")
	if err != nil {
		t.Fatalf("ResolvePublicResourceDownloads() error = %v", err)
	}

	if len(resource.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resource.Files))
	}
	if resource.Files[0].DirectURL != "" {
		t.Fatalf("expected empty direct url before lazy resolution, got: %q", resource.Files[0].DirectURL)
	}

	directURL, err := client.ResolvePublicFileDownloadURL(context.Background(), "https://disk.yandex.ru/d/test", resource.Files[0])
	if err != nil {
		t.Fatalf("ResolvePublicFileDownloadURL() error = %v", err)
	}
	if directURL != "https://download.example/orig.jpg" {
		t.Fatalf("unexpected direct url after lazy resolution: %q", directURL)
	}
}

func TestResolvePublicResourceDownloadsDirectoryVideoNoPreviewFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/disk/public/resources/download" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
			return
		}

		if r.URL.Path != "/v1/disk/public/resources" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pathParam := r.URL.Query().Get("path")
		switch pathParam {
		case "", "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"dir","name":"Root","path":"/","_embedded":{"items":[{"type":"file","name":"clip.mp4","path":"/clip.mp4"}],"total":1}}`))
		case "/clip.mp4":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"clip.mp4","path":"/clip.mp4","mime_type":"video/mp4","preview":"https://download.example/preview.mp4","sizes":[{"name":"orig","url":"https://download.example/orig-preview.mp4"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	resource, err := client.ResolvePublicResourceDownloads(context.Background(), "https://disk.yandex.ru/d/test")
	if err != nil {
		t.Fatalf("ResolvePublicResourceDownloads() error = %v", err)
	}

	if len(resource.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resource.Files))
	}

	_, err = client.ResolvePublicFileDownloadURL(context.Background(), "https://disk.yandex.ru/d/test", resource.Files[0])
	if err == nil {
		t.Fatal("expected error for video without href/file")
	}
	if !strings.Contains(err.Error(), "empty href") {
		t.Fatalf("expected empty href error, got: %v", err)
	}
}

func TestResolvePublicFileDownloadURLReadWithoutDownloadHint(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/disk/public/resources/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
		case "/v1/disk/public/resources":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"clip.mp4","path":"/clip.mp4","mime_type":"video/mp4"}`))
		case "/d/test":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body><script>{"rights":["read_without_download"]}</script></body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	_, err := client.ResolvePublicFileDownloadURL(context.Background(), server.URL+"/d/test", PublicDownload{Path: "/clip.mp4"})
	if err == nil {
		t.Fatal("expected read_without_download error")
	}
	if !strings.Contains(err.Error(), "forbids file download") {
		t.Fatalf("expected explicit forbid hint, got: %v", err)
	}
}

func TestResolvePublicDownloadURLReadWithoutDownloadHint(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/disk/public/resources/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"href":""}`))
		case "/v1/disk/public/resources":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"file","name":"clip.mp4","mime_type":"video/mp4"}`))
		case "/d/test":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html class="read-only"><body>read_with_password_without_download</body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.apiBaseURL = server.URL

	_, err := client.ResolvePublicDownloadURL(context.Background(), server.URL+"/d/test")
	if err == nil {
		t.Fatal("expected read_without_download error")
	}
	if !strings.Contains(err.Error(), "forbids file download") {
		t.Fatalf("expected explicit forbid hint, got: %v", err)
	}
}

func TestFilenameFromResponse(t *testing.T) {
	t.Parallel()

	withHeader := &http.Response{
		Header:  http.Header{"Content-Disposition": []string{`attachment; filename="a/b\\c.zip"`}},
		Request: &http.Request{URL: mustParseURL("https://host/path/name.bin")},
	}
	if got := FilenameFromResponse(withHeader, "fallback.bin"); got != "a_b_c.zip" {
		t.Fatalf("expected sanitized header filename, got %q", got)
	}

	withPath := &http.Response{
		Header:  http.Header{},
		Request: &http.Request{URL: mustParseURL("https://host/path/name.bin")},
	}
	if got := FilenameFromResponse(withPath, "fallback.bin"); got != "name.bin" {
		t.Fatalf("expected URL filename, got %q", got)
	}

	noMeta := &http.Response{Header: http.Header{}, Request: &http.Request{URL: mustParseURL("https://host/")}}
	if got := FilenameFromResponse(noMeta, "fallback.bin"); got != "fallback.bin" {
		t.Fatalf("expected fallback filename, got %q", got)
	}
}

func TestResolveDownloadSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fileSize      int64
		contentLength int64
		offset        int64
		want          int64
	}{
		{
			name:          "InitialPrefersContentLength",
			fileSize:      5974172,
			contentLength: 57072,
			offset:        0,
			want:          57072,
		},
		{
			name:          "InitialFallsBackToMetadata",
			fileSize:      5974172,
			contentLength: -1,
			offset:        0,
			want:          5974172,
		},
		{
			name:          "RangeKeepsOriginalTotal",
			fileSize:      5974172,
			contentLength: 1024,
			offset:        2048,
			want:          5974172,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveDownloadSize(tt.fileSize, tt.contentLength, tt.offset); got != tt.want {
				t.Fatalf("resolveDownloadSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestChooseResourceSizeURLPrefersOriginal(t *testing.T) {
	t.Parallel()

	meta := publicResourceResponse{
		Sizes: []resourceSize{
			{Name: "XXXL", URL: "https://example.com/preview.jpg"},
			{Name: "ORIGINAL", URL: "https://example.com/original.jpg"},
		},
	}

	if got := chooseResourceSizeURL(meta); got != "https://example.com/original.jpg" {
		t.Fatalf("chooseResourceSizeURL() = %q, want original URL", got)
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
