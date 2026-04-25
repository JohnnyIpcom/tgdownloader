package telegram

import (
	"reflect"
	"testing"
)

func TestExtractYandexDiskLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    []string
	}{
		{
			name:    "NoLinks",
			message: "just text without links",
			want:    nil,
		},
		{
			name:    "SingleYandexLink",
			message: "download here https://disk.yandex.ru/d/abc123",
			want:    []string{"https://disk.yandex.ru/d/abc123"},
		},
		{
			name:    "ShortYandexLinkWithPunctuation",
			message: "mirror: https://yadi.sk/d/qwe987).",
			want:    []string{"https://yadi.sk/d/qwe987"},
		},
		{
			name:    "MixedLinksAndDuplicates",
			message: "https://disk.yandex.ru/i/one https://example.com/test https://disk.yandex.ru/i/one https://disk.yandex.com/d/two",
			want:    []string{"https://disk.yandex.ru/i/one", "https://disk.yandex.com/d/two"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := extractYandexDiskLinks(tt.message)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("extractYandexDiskLinks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
