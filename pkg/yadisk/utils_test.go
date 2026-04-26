package yadisk

import (
	"errors"
	"testing"
)

func TestBuildSubdirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]interface{}
		hashtags bool
		wantLen  int
	}{
		{
			name:     "NilMetadata",
			metadata: nil,
			hashtags: true,
			wantLen:  0,
		},
		{
			name: "PeerOnly",
			metadata: map[string]interface{}{
				"peername": "peer",
			},
			hashtags: false,
			wantLen:  1,
		},
		{
			name: "PeerAndHashtags",
			metadata: map[string]interface{}{
				"peername": "peer",
				"hashtags": []string{"one", "two"},
			},
			hashtags: true,
			wantLen:  3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildSubdirectories(tt.metadata, tt.hashtags)
			if len(got) != tt.wantLen {
				t.Fatalf("BuildSubdirectories() length = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestIsSkippableYandexItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileName string
		err      error
		want     bool
	}{
		{
			name:     "ThumbsDbEmptyHref",
			fileName: "Thumbs.db",
			err:      errors.New("yandex disk api returned empty href (type=\"file\")"),
			want:     true,
		},
		{
			name:     "DesktopIniEmptyHref",
			fileName: "desktop.ini",
			err:      errors.New("yandex disk api returned empty href"),
			want:     true,
		},
		{
			name:     "RegularFileEmptyHref",
			fileName: "photo.jpg",
			err:      errors.New("yandex disk api returned empty href"),
			want:     false,
		},
		{
			name:     "ThumbsDbOtherError",
			fileName: "Thumbs.db",
			err:      errors.New("unexpected yandex disk api status: 403"),
			want:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsSkippableYandexItem(tt.fileName, tt.err); got != tt.want {
				t.Fatalf("IsSkippableYandexItem() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldUseHLSFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileName string
		err      error
		want     bool
	}{
		{
			name:     "VideoForbidsDownload",
			fileName: "movie.mp4",
			err:      errors.New("yandex disk public link forbids file download"),
			want:     true,
		},
		{
			name:     "VideoEmptyHref",
			fileName: "movie.mp4",
			err:      errors.New("yandex disk api returned empty href (type=\"file\")"),
			want:     true,
		},
		{
			name:     "ImageEmptyHref",
			fileName: "image.jpg",
			err:      errors.New("yandex disk api returned empty href"),
			want:     false,
		},
		{
			name:     "VideoOtherError",
			fileName: "movie.mp4",
			err:      errors.New("unexpected yandex disk api status: 403"),
			want:     false,
		},
		{
			name:     "NoError",
			fileName: "movie.mp4",
			err:      nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ShouldUseHLSFallback(tt.fileName, tt.err); got != tt.want {
				t.Fatalf("ShouldUseHLSFallback() = %v, want %v", got, tt.want)
			}
		})
	}
}
