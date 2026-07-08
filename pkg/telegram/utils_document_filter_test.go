package telegram

import "testing"

func TestIsSkippableTelegramDocumentName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "ThumbsDB", in: "Thumbs.db", want: true},
		{name: "ThumbsDBCaseInsensitive", in: "THUMBS.DB", want: true},
		{name: "DSStore", in: ".DS_Store", want: true},
		{name: "DesktopINI", in: "desktop.ini", want: true},
		{name: "NestedPathSlash", in: "folder/sub/Thumbs.db", want: true},
		{name: "NestedPathBackslash", in: `folder\\sub\\Thumbs.db`, want: true},
		{name: "Empty", in: "", want: false},
		{name: "RegularFile", in: "photo.jpg", want: false},
		{name: "Archive", in: "backup.db.zip", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isSkippableTelegramDocumentName(tt.in); got != tt.want {
				t.Fatalf("isSkippableTelegramDocumentName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
