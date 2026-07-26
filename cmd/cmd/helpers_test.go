package cmd

import "testing"

func TestSanitizeDownloadDirectoryComponent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{
			name:     "invalid Windows characters and controls",
			input:    " Group<>:\"/\\|?*\x00\n ",
			fallback: "0x1234",
			want:     "Group",
		},
		{
			name:     "trailing spaces and dots",
			input:    "Dialog...   ",
			fallback: "0x1234",
			want:     "Dialog",
		},
		{
			name:     "empty result",
			input:    "<>:*?",
			fallback: "0x1234",
			want:     "0x1234",
		},
		{
			name:     "reserved Windows component",
			input:    "CON.txt",
			fallback: "0x1234",
			want:     "0x1234",
		},
		{
			name:     "emoji and Unicode",
			input:    "  🍒🍒🍒 Фотограф  ",
			fallback: "0x1234",
			want:     "🍒🍒🍒 Фотограф",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDownloadDirectoryComponent(tt.input, tt.fallback); got != tt.want {
				t.Fatalf("sanitizeDownloadDirectoryComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
